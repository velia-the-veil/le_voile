// Package ipchandler provides shared IPC request handling for both
// the installed client and portable binaries.
package ipchandler

import (
	"context"
	"fmt"
	"net"
	"time"

	"github.com/velia-the-veil/le_voile/internal/config"
	"github.com/velia-the-veil/le_voile/internal/ipc"
	"github.com/velia-the-veil/le_voile/internal/leakcheck"
	svc "github.com/velia-the-veil/le_voile/internal/service"
	"github.com/velia-the-veil/le_voile/internal/tunnel"
)

// Options configures behavior differences between installed and portable modes.
type Options struct {
	// ConfigPathFn returns the config file path.
	// For installed: config.DiscoverPath(""). For portable: config.DiscoverPortablePath().
	ConfigPathFn func() string

	// SetStartupTypeFn changes OS service startup type. Nil in portable mode.
	SetStartupTypeFn func(bool) error
}

// Handle dispatches an IPC request to the appropriate service component.
func Handle(prg *svc.Program, req ipc.Request, opts Options) ipc.Response {
	switch req.Action {
	case ipc.ActionGetStatus:
		return handleGetStatus(prg)
	case ipc.ActionConnect:
		return handleConnect(prg)
	case ipc.ActionDisconnect:
		return handleDisconnect(prg)
	case ipc.ActionSetAutoStart:
		return handleSetAutoStart(prg, req, opts)
	case ipc.ActionQuit:
		return handleQuit(prg)
	case ipc.ActionSTUNStatus:
		return handleSTUNStatus(prg)
	case ipc.ActionLeakCheck:
		return handleLeakCheck(prg)
	case ipc.ActionCheckUpdate:
		return handleCheckUpdate(prg)
	case ipc.ActionUpdateStatus:
		return handleUpdateStatus(prg)
	case ipc.ActionSetBlocklist:
		return handleSetBlocklist(prg, req, opts)
	case ipc.ActionSetHTTPProxy:
		return handleSetHTTPProxy(prg, req, opts)
	default:
		return ipc.Response{Status: ipc.StatusError, Error: "unknown_action"}
	}
}

func handleGetStatus(prg *svc.Program) ipc.Response {
	tc := prg.TunnelClient()
	if tc == nil {
		// Tunnel not yet started, but rollback may have occurred before the first connect.
		resp := ipc.Response{Status: ipc.StatusDisconnected}
		if prg.RollbackOccurred() {
			resp.UpdateStatus = ipc.StatusRollback
			resp.RollbackVersion = prg.RollbackVersion()
			resp.RollbackReason = prg.RollbackReason()
		}
		fillLeakStatus(prg, &resp)
		resp.BlocklistEnabled = prg.BlocklistActive()
		resp.HTTPProxyActive = prg.HTTPProxyActive()
		resp.HTTPProxyAddr = prg.HTTPProxyAddr()
		resp.HTTPProxySeq = prg.HTTPProxySeq()
		resp.BrowserPoliciesApplied = prg.BrowserPolicyApplied()
		resp.BrowserPoliciesFailed = prg.BrowserPolicyFailed()
		return resp
	}
	state := tc.State().Get()
	uptime := FormatUptime(time.Since(prg.StartTime()))
	resp := ipc.Response{
		Status: string(state),
		IP:     "unknown",
		Uptime: uptime,
	}

	// Include rollback info for tray polling (highest priority)
	if prg.RollbackOccurred() {
		resp.UpdateStatus = ipc.StatusRollback
		resp.RollbackVersion = prg.RollbackVersion()
		resp.RollbackReason = prg.RollbackReason()
	} else if pendingVer := prg.PendingUpdateVersion(); pendingVer != "" {
		// Include pending update info for tray polling
		resp.UpdateVersion = pendingVer
		resp.UpdateStatus = ipc.StatusUpdateReady
	}

	// Include leak test state for tray polling (AC7).
	fillLeakStatus(prg, &resp)

	resp.BlocklistEnabled = prg.BlocklistActive()
	resp.HTTPProxyActive = prg.HTTPProxyActive()
	resp.HTTPProxyAddr = prg.HTTPProxyAddr()
	resp.HTTPProxySeq = prg.HTTPProxySeq()
	resp.BrowserPoliciesApplied = prg.BrowserPolicyApplied()
	resp.BrowserPoliciesFailed = prg.BrowserPolicyFailed()
	return resp
}

// fillLeakStatus populates LeakStatus and LeakLastCheck from the leak scheduler.
func fillLeakStatus(prg *svc.Program, resp *ipc.Response) {
	scheduler := prg.LeakScheduler()
	if scheduler == nil {
		return
	}
	result, checkAt := scheduler.LastResult()
	if result == nil {
		resp.LeakStatus = ipc.StatusLeakPending
	} else {
		resp.LeakStatus = result.Status
		resp.LeakLastCheck = checkAt.Format(time.RFC3339)
	}
}

func handleConnect(prg *svc.Program) ipc.Response {
	tc := prg.TunnelClient()
	if tc == nil {
		return ipc.Response{Status: ipc.StatusError, Error: "service_not_ready"}
	}
	if tc.State().Get() == tunnel.StateConnected {
		return ipc.Response{Status: ipc.StatusConnected}
	}
	// Stop reconnector to prevent race during manual connect.
	if r := prg.Reconnector(); r != nil {
		r.Stop()
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := tc.Connect(ctx); err != nil {
		// Restart reconnector so it can retry later.
		if r := prg.Reconnector(); r != nil {
			go r.Start(prg.Context())
		}
		return ipc.Response{Status: ipc.StatusError, Error: err.Error()}
	}
	// Restart reconnector for future automatic reconnections.
	if r := prg.Reconnector(); r != nil {
		go r.Start(prg.Context())
	}
	return ipc.Response{Status: ipc.StatusConnected}
}

func handleDisconnect(prg *svc.Program) ipc.Response {
	tc := prg.TunnelClient()
	if tc == nil {
		return ipc.Response{Status: ipc.StatusDisconnected}
	}
	// Stop reconnector to prevent automatic reconnection after user-initiated disconnect.
	if r := prg.Reconnector(); r != nil {
		r.Stop()
	}
	if err := tc.Disconnect(); err != nil {
		return ipc.Response{Status: ipc.StatusError, Error: err.Error()}
	}
	return ipc.Response{Status: ipc.StatusDisconnected}
}

func handleSetAutoStart(prg *svc.Program, req ipc.Request, opts Options) ipc.Response {
	if req.Value != "true" && req.Value != "false" {
		return ipc.Response{Status: ipc.StatusError, Error: "invalid_value: must be \"true\" or \"false\""}
	}
	autoStart := req.Value == "true"

	cfgPath := opts.ConfigPathFn()
	if cfgPath == "" {
		return ipc.Response{Status: ipc.StatusError, Error: "no_config_file"}
	}

	cfg, err := config.Load(cfgPath)
	if err != nil {
		return ipc.Response{Status: ipc.StatusError, Error: err.Error()}
	}

	cfg.Client.AutoStart = autoStart
	if err := cfg.Save(cfgPath); err != nil {
		return ipc.Response{Status: ipc.StatusError, Error: err.Error()}
	}

	if opts.SetStartupTypeFn != nil {
		if err := opts.SetStartupTypeFn(autoStart); err != nil {
			return ipc.Response{Status: ipc.StatusError, Error: err.Error()}
		}
	}

	return ipc.Response{Status: ipc.StatusOK}
}

// handleQuit stops the reconnector and triggers service stop through SCM.
// p.svc.Stop() sends SERVICE_CONTROL_STOP via SCM so kardianos Execute
// loop exits cleanly → no OnFailure restart. Cancel() alone would leave
// the Execute loop hanging, causing SCM to treat the exit as a crash.
func handleQuit(prg *svc.Program) ipc.Response {
	if r := prg.Reconnector(); r != nil {
		r.Stop()
	}
	go func() {
		time.Sleep(100 * time.Millisecond)
		prg.RequestStop()
	}()
	return ipc.Response{Status: ipc.StatusDisconnected}
}

func handleSTUNStatus(prg *svc.Program) ipc.Response {
	status := ipc.StatusSTUNInactive
	if prg.STUNActive() {
		status = ipc.StatusSTUNActive
	}
	return ipc.Response{Status: status}
}

func handleLeakCheck(prg *svc.Program) ipc.Response {
	tc := prg.TunnelClient()
	if tc == nil {
		return ipc.Response{Status: ipc.StatusError, Error: "service_not_ready"}
	}
	if tc.State().Get() != tunnel.StateConnected {
		return ipc.Response{Status: ipc.StatusError, Error: "tunnel_not_connected"}
	}

	// Get the VPS public IP by relaying a STUN Binding Request through the
	// tunnel. The VPS forwards it to the STUN server, which sees the VPS IP.
	// This is the correct reference — it's the IP that SHOULD appear in all
	// STUN checks if the interception is working properly.
	getPublicIP := func(ctx context.Context) (net.IP, error) {
		req := leakcheck.BuildBindingRequest()
		resp, err := tc.SendSTUNRelay(ctx, req, "stun.l.google.com:19302")
		if err != nil {
			// Retry once for transient QUIC stream errors.
			select {
			case <-ctx.Done():
				return nil, fmt.Errorf("leakcheck: tunnel stun relay: %w", err)
			case <-time.After(500 * time.Millisecond):
			}
			req = leakcheck.BuildBindingRequest()
			resp, err = tc.SendSTUNRelay(ctx, req, "stun.l.google.com:19302")
			if err != nil {
				return nil, fmt.Errorf("leakcheck: tunnel stun relay: %w", err)
			}
		}
		return leakcheck.ParseXORMappedAddress(resp)
	}
	checker := leakcheck.NewWebRTCLeakChecker(getPublicIP)

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()

	report, err := checker.RunFullCheck(ctx)
	if err != nil {
		return ipc.Response{Status: ipc.StatusError, Error: err.Error()}
	}

	return ipc.Response{
		Status: report.Status,
		IP:     report.STUNIP,
	}
}

func handleCheckUpdate(prg *svc.Program) ipc.Response {
	upd := prg.Updater()
	if upd == nil {
		return ipc.Response{Status: ipc.StatusError, Error: "updates_disabled"}
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	staged, err := upd.CheckAndDownload(ctx)
	if err != nil {
		return ipc.Response{Status: ipc.StatusError, Error: err.Error()}
	}
	if staged == nil {
		return ipc.Response{Status: ipc.StatusOK, UpdateStatus: ipc.StatusUpToDate}
	}
	return ipc.Response{Status: ipc.StatusOK, UpdateVersion: staged.Version, UpdateStatus: ipc.StatusUpdateReady}
}

func handleUpdateStatus(prg *svc.Program) ipc.Response {
	// Highest priority: rollback just occurred
	if prg.RollbackOccurred() {
		return ipc.Response{
			Status:          ipc.StatusOK,
			UpdateStatus:    ipc.StatusRollback,
			RollbackVersion: prg.RollbackVersion(),
			RollbackReason:  prg.RollbackReason(),
		}
	}

	// Check for install result from last startup
	if installedVer := prg.InstalledVersion(); installedVer != "" {
		return ipc.Response{
			Status:           ipc.StatusOK,
			UpdateStatus:     ipc.StatusInstalled,
			InstalledVersion: installedVer,
		}
	}

	if installErr := prg.LastInstallError(); installErr != "" {
		return ipc.Response{
			Status:       ipc.StatusOK,
			UpdateStatus: ipc.StatusInstallFailed,
			InstallError: installErr,
		}
	}

	upd := prg.Updater()
	if upd == nil {
		return ipc.Response{Status: ipc.StatusError, Error: "updates_disabled"}
	}

	if upd.IsDownloading() {
		return ipc.Response{Status: ipc.StatusOK, UpdateStatus: ipc.StatusDownloading}
	}

	stagedVer := upd.StagedVersion()
	if stagedVer != "" {
		return ipc.Response{Status: ipc.StatusOK, UpdateVersion: stagedVer, UpdateStatus: ipc.StatusUpdateReady}
	}

	return ipc.Response{Status: ipc.StatusOK, UpdateStatus: ipc.StatusUpToDate}
}

func handleSetBlocklist(prg *svc.Program, req ipc.Request, opts Options) ipc.Response {
	if req.Value != "true" && req.Value != "false" {
		return ipc.Response{Status: ipc.StatusError, Error: "invalid_value"}
	}
	enabled := req.Value == "true"

	cfgPath := opts.ConfigPathFn()
	if cfgPath == "" {
		return ipc.Response{Status: ipc.StatusError, Error: "no_config_file"}
	}

	cfg, err := config.Load(cfgPath)
	if err != nil {
		return ipc.Response{Status: ipc.StatusError, Error: err.Error()}
	}

	cfg.Blocklist.Enabled = enabled
	if err := cfg.Save(cfgPath); err != nil {
		return ipc.Response{Status: ipc.StatusError, Error: err.Error()}
	}

	if enabled {
		prg.EnableBlocklist()
	} else {
		prg.DisableBlocklist()
	}

	return ipc.Response{Status: ipc.StatusOK, BlocklistEnabled: enabled}
}

func handleSetHTTPProxy(prg *svc.Program, req ipc.Request, opts Options) ipc.Response {
	if req.Value != "true" && req.Value != "false" {
		return ipc.Response{Status: ipc.StatusError, Error: "invalid_value"}
	}
	enabled := req.Value == "true"

	cfgPath := opts.ConfigPathFn()
	if cfgPath == "" {
		return ipc.Response{Status: ipc.StatusError, Error: "no_config_file"}
	}

	cfg, err := config.Load(cfgPath)
	if err != nil {
		return ipc.Response{Status: ipc.StatusError, Error: err.Error()}
	}

	cfg.HTTPProxy.Enabled = enabled
	if err := cfg.Save(cfgPath); err != nil {
		return ipc.Response{Status: ipc.StatusError, Error: err.Error()}
	}

	if enabled {
		if err := prg.EnableHTTPProxy(); err != nil {
			return ipc.Response{Status: ipc.StatusError, Error: err.Error()}
		}
	} else {
		prg.DisableHTTPProxy()
	}

	return ipc.Response{
		Status:          ipc.StatusOK,
		HTTPProxyActive: prg.HTTPProxyActive(),
		HTTPProxyAddr:   prg.HTTPProxyAddr(),
		HTTPProxySeq:    prg.HTTPProxySeq(),
	}
}

// FormatUptime formats a duration as "Xh Ym" or "Xm Ys".
func FormatUptime(d time.Duration) string {
	d = d.Truncate(time.Second)
	h := int(d.Hours())
	m := int(d.Minutes()) % 60
	s := int(d.Seconds()) % 60
	if h > 0 {
		return fmt.Sprintf("%dh%02dm", h, m)
	}
	return fmt.Sprintf("%dm%02ds", m, s)
}
