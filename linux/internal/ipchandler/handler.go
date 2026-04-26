//go:build linux

// Package ipchandler provides shared IPC request handling for both
// the installed client and portable binaries.
package ipchandler

import (
	"context"
	"errors"
	"fmt"
	"os"
	"sort"
	"time"

	"github.com/velia-the-veil/le_voile/linux/internal/anomaly"
	"github.com/velia-the-veil/le_voile/linux/internal/config"
	"github.com/velia-the-veil/le_voile/linux/internal/ipc"
	"github.com/velia-the-veil/le_voile/internal/registry"
	svc "github.com/velia-the-veil/le_voile/linux/internal/service"
	"github.com/velia-the-veil/le_voile/internal/tunnel"
	"github.com/velia-the-veil/le_voile/linux/internal/uiwatchdog"
)

// mutatingActions lists every IPC action that writes state — config, service
// lifecycle, or firewall policy. Used by the strict-auth gate (fix C3 audit
// sécurité) so operators can flip one environment variable to require a
// verified token on every mutation and reject the legacy empty-Auth path
// that previously let any same-user process issue commands.
var mutatingActions = map[string]struct{}{
	ipc.ActionConnect:           {},
	ipc.ActionDisconnect:        {},
	ipc.ActionQuit:              {},
	ipc.ActionUIDisconnect:      {},
	ipc.ActionSetAutoStart:      {},
	ipc.ActionSetBlocklist:      {},
	ipc.ActionSetHTTPProxy:      {},
	ipc.ActionSelectCountry:     {},
	ipc.ActionRetryCaptive:      {},
	ipc.ActionSetAllowIPv6Leak:  {},
	ipc.ActionSetKillSwitchMode: {},
	ipc.ActionTriggerRecovery:   {},
}

// isMutatingAction reports whether the given action touches persistent or
// runtime-authoritative state. Used exclusively by the strict-auth gate.
func isMutatingAction(a string) bool {
	_, ok := mutatingActions[a]
	return ok
}

// legacyEmptyAuthAllowed returns true when the operator explicitly opted
// back into the pre-2026-04 contract that accepted mutating IPC requests
// with an empty req.Auth. Set LEVOILE_IPC_LEGACY_AUTH=1 to re-enable that
// path — only useful on a host where a stale UI build has not yet been
// upgraded to the token-aware one and the mismatch is producing
// "auth_required" errors on every click.
//
// Default (env unset or any other value) is strict: empty-Auth mutations
// are rejected. The flip from opt-in-strict (pre-2026-04) to opt-out-legacy
// is deliberate — the window where any same-user process can drive the
// service is closed by default now that cmd/ui loads the ctlauth token on
// startup (see cmd/ui/main.go).
//
// The older LEVOILE_IPC_STRICT_AUTH variable is now a no-op and is kept
// only so a leftover =1 from previous rollouts does not surprise anyone.
func legacyEmptyAuthAllowed() bool {
	return os.Getenv("LEVOILE_IPC_LEGACY_AUTH") == "1"
}

// uiSupervisionFromSnapshot maps the in-process uiwatchdog snapshot to
// the wire-format struct used by GetStatus / GetUISupervision. RFC 3339
// is the format the rest of the IPC payload uses for timestamps
// (LeakLastCheck), so we stay consistent.
func uiSupervisionFromSnapshot(s *uiwatchdog.Snapshot) *ipc.UISupervisionState {
	if s == nil {
		return nil
	}
	out := &ipc.UISupervisionState{
		Enabled:            s.Enabled,
		RestartCountWindow: s.RestartCountWindow,
	}
	if !s.LastRestartAt.IsZero() {
		out.LastRestartAt = s.LastRestartAt.UTC().Format(time.RFC3339)
	}
	if !s.BackoffUntil.IsZero() {
		out.BackoffUntil = s.BackoffUntil.UTC().Format(time.RFC3339)
	}
	return out
}

// configMu is an alias for config.Mu (Story 5.9 H2 fix). Local symbol kept for
// minimal diff in handler bodies — every config writer in the project (IPC
// handlers + cmd/client kill-switch persister) shares the same mutex now.
var configMu = &config.Mu

// persistPreferredCountry writes the user's country choice back to the TOML
// config under client.preferred_country. Serializes with configMu so it
// never races other handlers that also edit config.
//
// Best-effort on load: if the file is missing or corrupt, the call is a
// silent no-op (returns nil). Save errors are surfaced so SelectCountry can
// report them to the UI — losing a preference on disk is a real user-facing
// failure, losing a Load on a partial setup is not.
func persistPreferredCountry(cfgPath, countryCode string, key []byte) error {
	configMu.Lock()
	defer configMu.Unlock()
	cfg, err := config.Load(cfgPath)
	if err != nil {
		return nil
	}
	cfg.Client.PreferredCountry = countryCode
	return cfg.SaveAndSign(cfgPath, key)
}

// Options configures behavior differences between installed and portable modes.
type Options struct {
	// ConfigPathFn returns the config file path.
	// For installed: config.DiscoverPath(""). For portable: config.DiscoverPortablePath().
	ConfigPathFn func() string

	// SetStartupTypeFn changes OS service startup type. Nil in portable mode.
	SetStartupTypeFn func(bool) error

	// IntegrityKey is the 32-byte machine-local HMAC key used to re-sign
	// config.toml after every mutation (Story 7.5 / NFR9j). Nil disables the
	// re-signing step (tests, legacy bootstraps). Production call-sites wire
	// this from cmd/client.main after config.LoadOrCreateKey.
	IntegrityKey []byte
}

// Handle dispatches an IPC request to the appropriate service component.
func Handle(prg *svc.Program, req ipc.Request, opts Options) ipc.Response {
	// Story 7.5 / NFR9j — when startup integrity verification failed, refuse
	// any mutating action. GetStatus stays open so the UI can fetch the
	// integrity_failed flag and render the recovery banner. Read-only probes
	// (leakcheck, update status, registry) also stay open: they don't persist
	// anything and the UI still needs them to show accurate state.
	//
	// This gate runs BEFORE the auth gate so an installation whose config was
	// tampered with returns the actionable integrity_failed signal (which the
	// UI maps to a recovery banner) rather than a generic auth_required that
	// would mask the real failure mode.
	if prg.IntegrityFailed() {
		switch req.Action {
		case ipc.ActionConnect,
			ipc.ActionSetAutoStart,
			ipc.ActionSetBlocklist,
			ipc.ActionSetHTTPProxy,
			ipc.ActionSelectCountry,
			ipc.ActionSetAllowIPv6Leak,
			ipc.ActionSetKillSwitchMode:
			return ipc.Response{
				Status:          ipc.StatusError,
				Error:           "integrity_failed",
				IntegrityFailed: true,
			}
		}
	}

	// Fix C3 (audit sécurité) — strict-by-default authentication gate. A
	// mutating action with an empty req.Auth is rejected unless the operator
	// has explicitly opted back into the pre-2026-04 legacy path via
	// LEVOILE_IPC_LEGACY_AUTH=1 (documented in SECURITY.md §C3). The audit
	// line still fires in both postures so operators can spot missing tokens
	// even when legacy mode is keeping the install functional.
	if isMutatingAction(req.Action) && req.Auth == "" {
		legacy := legacyEmptyAuthAllowed()
		fmt.Fprintf(os.Stderr, "SECURITY AUDIT: mutating IPC without req.Auth action=%s legacy=%v\n",
			req.Action, legacy)
		if !legacy {
			return ipc.Response{Status: ipc.StatusError, Error: "auth_required"}
		}
	}
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
	case ipc.ActionUIDisconnect:
		return handleUIDisconnect(prg)
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
	case ipc.ActionGetRegistry:
		return handleGetRegistry(prg)
	case ipc.ActionSelectCountry:
		return handleSelectCountry(prg, req, opts)
	case ipc.ActionRetryCaptive:
		return handleRetryCaptive(prg)
	case ipc.ActionGetAllowIPv6Leak:
		return handleGetAllowIPv6Leak(prg)
	case ipc.ActionSetAllowIPv6Leak:
		return handleSetAllowIPv6Leak(prg, req, opts)
	case ipc.ActionGetUISupervision:
		return handleGetUISupervision(prg)
	case ipc.ActionGetKillSwitchMode:
		return handleGetKillSwitchMode(prg)
	case ipc.ActionSetKillSwitchMode:
		return handleSetKillSwitchMode(prg, req, opts)
	case ipc.ActionTriggerRecovery:
		return handleTriggerRecovery(prg, req)
	default:
		return ipc.Response{Status: ipc.StatusError, Error: "unknown_action"}
	}
}

// handleGetUISupervision returns the levoile-ui watchdog state (Story 5.7).
// Returns Status=ok with UISupervision=nil when supervision is disabled
// (Linux delegates to systemd) so the UI can distinguish "no data" from
// "watchdog active but idle".
func handleGetUISupervision(prg *svc.Program) ipc.Response {
	snap := prg.UIWatchdogSnapshot()
	resp := ipc.Response{Status: ipc.StatusOK}
	if snap != nil {
		resp.UISupervision = uiSupervisionFromSnapshot(snap)
	}
	return resp
}

func handleGetStatus(prg *svc.Program) ipc.Response {
	// Story 2.3 : si le scan preflight au démarrage a détecté un VPN
	// concurrent, court-circuite tout le reste et retourne un statut explicite.
	// La propriété tunnel/IP/uptime n'a pas de sens tant que le tunnel n'a
	// pas été monté.
	if e := prg.ConcurrentVPNError(); e != nil {
		resp := ipc.Response{
			Status:        ipc.StatusError,
			Error:         e.Error(),
			ConcurrentVPN: true,
			RealIP:        prg.RealIP(),
		}
		// Inclure rollback/update/blocklist même en mode ConcurrentVPN pour
		// que l'UI ne perde pas ces informations (fix M2).
		if prg.RollbackOccurred() {
			resp.UpdateStatus = ipc.StatusRollback
			resp.RollbackVersion = prg.RollbackVersion()
			resp.RollbackReason = prg.RollbackReason()
		}
		resp.BlocklistEnabled = prg.BlocklistActive()
		resp.HTTPProxyActive = prg.HTTPProxyActive()
		resp.HTTPProxyAddr = prg.HTTPProxyAddr()
		resp.HTTPProxySeq = prg.HTTPProxySeq()
		resp.AllowIPv6Leak = prg.AllowIPv6Leak()
		// Story 5.9 — surface kill-switch mode so the UI banner + tray-rouge
		// override stay accurate even when the preflight rejected the tunnel.
		resp.KillSwitchMode = prg.KillSwitchMode()
		// Story 4.4 — keep the failover banner + active country visible even
		// when a concurrent VPN blocks the rest of the status payload, so the
		// user sees the last meaningful state before the preflight rejection.
		resp.FailoverAlert = prg.FailoverAlert()
		resp.CurrentCountryCode = prg.CurrentCountryCode()
		// Story 5.7 — watchdog state must surface on every GetStatus path so
		// the UI diagnostics panel can observe supervision even when a
		// concurrent VPN blocks the tunnel.
		resp.UISupervision = uiSupervisionFromSnapshot(prg.UIWatchdogSnapshot())
		// Story 6.3 — anomaly recovery flags are independent of the tunnel
		// state: the watchdog may fire even when preflight rejected the tunnel.
		resp.AnomalyActive = prg.AnomalyActive()
		resp.AnomalyReason = prg.AnomalyReason()
		// Story 7.5 — propagate the config-integrity flag on every branch so
		// the UI banner stays visible even during concurrent-VPN lockdown.
		resp.IntegrityFailed = prg.IntegrityFailed()
		return resp
	}
	tc := prg.TunnelClient()
	if tc == nil {
		// Tunnel not yet started, but rollback may have occurred before the first connect.
		resp := ipc.Response{Status: ipc.StatusDisconnected, RealIP: prg.RealIP()}
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
		resp.CircuitBreakerTripped = prg.CircuitBreakerTripped()
		resp.CircuitBreakerMessage = prg.CircuitBreakerMessage()
		resp.FailoverAlert = prg.FailoverAlert()
		resp.CurrentCountryCode = prg.CurrentCountryCode()
		resp.AllowIPv6Leak = prg.AllowIPv6Leak()
		resp.KillSwitchMode = prg.KillSwitchMode()
		resp.CaptivePortal = prg.CaptivePortal()
		resp.CaptiveProbeURL = prg.CaptiveProbeURL()
		resp.UISupervision = uiSupervisionFromSnapshot(prg.UIWatchdogSnapshot())
		resp.AnomalyActive = prg.AnomalyActive()
		resp.AnomalyReason = prg.AnomalyReason()
		resp.IntegrityFailed = prg.IntegrityFailed()
		return resp
	}
	state := tc.State().Get()
	uptime := FormatUptime(time.Since(prg.StartTime()))
	visibleIP := prg.VisibleIP()
	relayDomain := tc.RelayDomain()
	resp := ipc.Response{
		Status:      string(state),
		IP:          visibleIP,
		RealIP:      prg.RealIP(),
		Uptime:      uptime,
		RelayDomain: relayDomain,
	}

	// Populate relay metadata from discoverer (best-effort). We also
	// self-heal two pieces of drift-prone state on every status read:
	//
	//  - prg.CurrentCountryCode can be stale when handleSelectCountry
	//    updated tc.RelayDomain but tc.Connect failed, and the reconnector
	//    later succeeded on the new relay without any code path calling
	//    SetCurrentCountry again. The frontend then shows a green
	//    "Connecter" button even though the tunnel IS on the requested
	//    country. Deriving the code from tc.RelayDomain here and pushing
	//    it back into the program keeps both aligned at the cost of one
	//    map lookup + one atomic store per poll (~1 µs).
	//
	//  - VisibleIP lags the same way: if SetVisibleIP("") + DetectVisibleIP
	//    never fired (Connect error path), the stale IP from the previous
	//    relay keeps showing. Trigger a re-detect here when the tunnel is
	//    connected but VisibleIP is empty or still matches the pre-switch
	//    relay domain; DetectVisibleIP is cheap and idempotent.
	derivedCode := ""
	if disc := prg.Discoverer(); disc != nil {
		for _, r := range disc.Relays() {
			if r.Domain == relayDomain {
				resp.RelayID = r.ID
				if lat := disc.LatencyFor(r.ID); lat > 0 {
					resp.RelayLatency = fmt.Sprintf("%dms", lat.Milliseconds())
				}
				derivedCode = registry.ExtractCountryCode(r.ID, r.Domain)
				if meta, ok := registry.CountryMetaMap[derivedCode]; ok {
					resp.Country = meta.Name
					resp.CountryFlag = meta.Flag
				}
				break
			}
		}
	}
	if derivedCode != "" && prg.CurrentCountryCode() != derivedCode {
		prg.SetCurrentCountry(derivedCode)
	}
	if state == tunnel.StateConnected && visibleIP == "" {
		go prg.DetectVisibleIP(prg.Context())
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
	resp.CircuitBreakerTripped = prg.CircuitBreakerTripped()
	resp.CircuitBreakerMessage = prg.CircuitBreakerMessage()
	resp.FailoverAlert = prg.FailoverAlert()
	resp.CurrentCountryCode = prg.CurrentCountryCode()
	resp.FirewallAltered = prg.FirewallAltered()
	resp.AllowIPv6Leak = prg.AllowIPv6Leak()
	resp.KillSwitchMode = prg.KillSwitchMode()
	resp.CaptivePortal = prg.CaptivePortal()
	resp.CaptiveProbeURL = prg.CaptiveProbeURL()
	resp.UISupervision = uiSupervisionFromSnapshot(prg.UIWatchdogSnapshot())
	// Story 6.3 — surface anomaly recovery state to the UI so the webview
	// can show the orange banner and the tray can switch to the alert icon
	// while a RecoverFromAnomaly sequence is in flight.
	resp.AnomalyActive = prg.AnomalyActive()
	resp.AnomalyReason = prg.AnomalyReason()
	// Story 7.5 — config integrity flag is surfaced on every get_status so
	// the UI can render the recovery banner without a separate endpoint.
	resp.IntegrityFailed = prg.IntegrityFailed()
	return resp
}

// fillLeakStatus populates LeakStatus, LeakLastCheck, LeakExpectedIP and
// LeakReason from the leak scheduler's last result. Story 6.2: the handler
// is now pass-through — the leak check module produces "ok"/"leak_detected"
// directly (no translation needed).
func fillLeakStatus(prg *svc.Program, resp *ipc.Response) {
	scheduler := prg.LeakScheduler()
	if scheduler == nil {
		return
	}
	result, checkAt := scheduler.LastResult()
	if result == nil {
		resp.LeakStatus = ipc.StatusLeakPending
		return
	}
	resp.LeakStatus = result.Status
	resp.LeakLastCheck = checkAt.Format(time.RFC3339)
	resp.LeakExpectedIP = result.ExpectedIP
	resp.LeakReason = result.LeakReason
}

func handleConnect(prg *svc.Program) ipc.Response {
	// Story 2.3 : avant tout (y compris service_not_ready), re-scanner. Si
	// l'utilisateur a démarré un VPN tiers après le lancement du service,
	// on doit refuser le Connect IPC sans toucher au tunnel.
	if e := prg.DetectConcurrentVPN(); e != nil {
		return ipc.Response{
			Status:        ipc.StatusError,
			Error:         e.Error(),
			ConcurrentVPN: true,
		}
	}
	tc := prg.TunnelClient()
	if tc == nil {
		return ipc.Response{Status: ipc.StatusError, Error: "service_not_ready"}
	}
	if tc.State().Get() == tunnel.StateConnected {
		return ipc.Response{Status: ipc.StatusConnected}
	}
	// Clear circuit-breaker tripped state so the Reconnector resumes normal
	// behavior and the UI banner disappears once the user triggers a retry.
	prg.ResetCircuitBreaker()
	// Stop reconnector to prevent race during manual connect.
	if r := prg.Reconnector(); r != nil {
		r.Stop()
	}
	// Refresh the stored real IP in the background. Relay /ip is allowed
	// through the kill switch so this works any time, and catches ISP
	// address rotations that happened since the last boot-time detection
	// at service.go:0e. Best-effort — a stale value is better than empty.
	go prg.DetectRealIP(prg.Context())
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := tc.Connect(ctx); err != nil {
		// Restart reconnector so it can retry later.
		if r := prg.Reconnector(); r != nil {
			go r.Start(prg.Context())
		}
		return ipc.Response{Status: ipc.StatusError, Error: err.Error()}
	}
	// Story 5.9 — manual Connect also triggers degraded-mode auto-restore.
	prg.MaybeRestoreKillSwitch(ctx, "ipc-connect")
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

// handleSetAutoStart persists cfg.Client.AutoStart and applies the OS-level
// auto-start policy (SCM startup type + scheduled task on Windows). Not
// atomic across the three pieces of state (review finding F3): if the TOML
// save succeeds but SetStartupTypeFn fails, config.toml claims one value
// while SCM/task still reflect the previous one. The inconsistency is
// self-healed on the next service start by reconcileAutoStartOS in
// cmd/client/main.go, which re-applies the TOML value to the OS surfaces.
func handleSetAutoStart(prg *svc.Program, req ipc.Request, opts Options) ipc.Response {
	if req.Value != "true" && req.Value != "false" {
		return ipc.Response{Status: ipc.StatusError, Error: "invalid_value: must be \"true\" or \"false\""}
	}
	autoStart := req.Value == "true"

	cfgPath := opts.ConfigPathFn()
	if cfgPath == "" {
		return ipc.Response{Status: ipc.StatusError, Error: "no_config_file"}
	}

	configMu.Lock()
	cfg, err := config.Load(cfgPath)
	if err != nil {
		configMu.Unlock()
		return ipc.Response{Status: ipc.StatusError, Error: err.Error()}
	}
	cfg.Client.AutoStart = autoStart
	if err := cfg.SaveAndSign(cfgPath, opts.IntegrityKey); err != nil {
		configMu.Unlock()
		return ipc.Response{Status: ipc.StatusError, Error: err.Error()}
	}
	configMu.Unlock()

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

// handleUIDisconnect acknowledges an UI process quit without touching the
// service lifecycle. The UI sends this via ActionUIDisconnect during its
// shutdown sequence (Story 5.8) so the tunnel, kill switch, routing and TUN
// stay up under systemd/SCM control even after the tray process exits.
// Intentionally does NOT call Reconnector.Stop or RequestStop — those are
// reserved for ActionQuit (full stop via levoile-ctl / SCM callback).
//
// The Program parameter is accepted (not _) so future hooks (session
// counting, structured logging with PID) can be wired in without a
// signature churn.
func handleUIDisconnect(prg *svc.Program) ipc.Response {
	_ = prg // reserved for future observability hooks (see function doc)
	return ipc.Response{Status: ipc.StatusOK}
}

func handleLeakCheck(prg *svc.Program) ipc.Response {
	tc := prg.TunnelClient()
	if tc == nil {
		return ipc.Response{Status: ipc.StatusError, Error: "service_not_ready"}
	}
	if tc.State().Get() != tunnel.StateConnected {
		return ipc.Response{Status: ipc.StatusError, Error: "tunnel_not_connected"}
	}

	// Story 6.2: the scheduler owns the correctly-configured checker (with a
	// RelayIPResolver backed by DoH). Triggering it here runs one check
	// synchronously and refreshes LastResult, which fillLeakStatus reads.
	scheduler := prg.LeakScheduler()
	if scheduler == nil {
		return ipc.Response{Status: ipc.StatusError, Error: "leak_scheduler_not_running"}
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	scheduler.TriggerCheck(ctx)

	result, _ := scheduler.LastResult()
	if result == nil {
		return ipc.Response{Status: ipc.StatusError, Error: "leak_check_no_result"}
	}

	resp := ipc.Response{
		Status:         result.Status,
		IP:             result.STUNIP,
		LeakStatus:     result.Status,
		LeakExpectedIP: result.ExpectedIP,
		LeakReason:     result.LeakReason,
	}
	return resp
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

	configMu.Lock()
	cfg, err := config.Load(cfgPath)
	if err != nil {
		configMu.Unlock()
		return ipc.Response{Status: ipc.StatusError, Error: err.Error()}
	}
	cfg.Blocklist.Enabled = enabled
	if err := cfg.SaveAndSign(cfgPath, opts.IntegrityKey); err != nil {
		configMu.Unlock()
		return ipc.Response{Status: ipc.StatusError, Error: err.Error()}
	}
	configMu.Unlock()

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

	configMu.Lock()
	cfg, err := config.Load(cfgPath)
	if err != nil {
		configMu.Unlock()
		return ipc.Response{Status: ipc.StatusError, Error: err.Error()}
	}
	cfg.HTTPProxy.Enabled = enabled
	if err := cfg.SaveAndSign(cfgPath, opts.IntegrityKey); err != nil {
		configMu.Unlock()
		return ipc.Response{Status: ipc.StatusError, Error: err.Error()}
	}
	configMu.Unlock()

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

func handleGetRegistry(prg *svc.Program) ipc.Response {
	disc := prg.Discoverer()
	if disc == nil {
		return ipc.Response{Status: ipc.StatusError, Error: "registry_disabled"}
	}

	byCountry := disc.RelaysByCountry()

	// Determine the active country from the current relay domain.
	activeCode := ""
	tc := prg.TunnelClient()
	if tc != nil {
		domain := tc.RelayDomain()
		relays := disc.Relays()
		for _, r := range relays {
			if r.Domain == domain {
				activeCode = registry.ExtractCountryCode(r.ID, r.Domain)
				break
			}
		}
	}

	var countries []ipc.RegistryCountry
	for code, relays := range byCountry {
		name, flag := code, ""
		if meta, ok := registry.CountryMetaMap[code]; ok {
			name = meta.Name
			flag = meta.Flag
		}
		countries = append(countries, ipc.RegistryCountry{
			Code:       code,
			Name:       name,
			Flag:       flag,
			RelayCount: len(relays),
			Active:     code == activeCode,
		})
	}

	// Deterministic order: sort by country name for stable sidebar rendering.
	sort.Slice(countries, func(i, j int) bool {
		return countries[i].Name < countries[j].Name
	})

	return ipc.Response{
		Status:            ipc.StatusOK,
		RegistryCountries: countries,
	}
}

func handleSelectCountry(prg *svc.Program, req ipc.Request, opts Options) ipc.Response {
	countryCode := req.Value
	if countryCode == "" {
		return ipc.Response{Status: ipc.StatusError, Error: "missing_country_code"}
	}
	if _, ok := registry.CountryMetaMap[countryCode]; !ok {
		return ipc.Response{Status: ipc.StatusError, Error: "unknown_country_code"}
	}

	disc := prg.Discoverer()
	if disc == nil {
		return ipc.Response{Status: ipc.StatusError, Error: "registry_disabled"}
	}

	tc := prg.TunnelClient()
	if tc == nil {
		return ipc.Response{Status: ipc.StatusError, Error: "service_not_ready"}
	}

	// Strict round-robin across the country's relay pool (AC Story 4.3).
	// The cursor is kept in RAM and resets when the pool composition changes
	// (e.g. after a latency re-sort).
	relay, err := disc.SelectRelay(countryCode)
	if err != nil {
		switch {
		case errors.Is(err, registry.ErrUnknownCountry):
			return ipc.Response{Status: ipc.StatusError, Error: "unknown_country_code"}
		case errors.Is(err, registry.ErrNoRelaysForCountry):
			return ipc.Response{Status: ipc.StatusError, Error: "no_relays_for_country"}
		default:
			return ipc.Response{Status: ipc.StatusError, Error: fmt.Sprintf("ipchandler: select relay: %v", err)}
		}
	}

	// Fire a background latency re-sort as soon as the user's country intent
	// is registered — independent of whether the downstream tunnel swap
	// succeeds (AC Story 4.3 — re-tri à chaque changement de pays). The
	// discoverer deduplicates concurrent triggers so rapid clicks stay cheap.
	disc.TriggerBackgroundDiscover(prg.Context())

	// Update the tunnel to use the new relay (resolves and stores the new
	// relay IP via DNS, swaps the public key for /verify).
	if err := tc.UpdateRelay(relay.Domain, relay.PublicKey); err != nil {
		return ipc.Response{Status: ipc.StatusError, Error: fmt.Sprintf("ipchandler: update relay: %v", err)}
	}

	// Re-apply routing /32 + WFP allow-rule for the new relay IP. Without
	// this the kill-switch keeps allowing only the OLD relay IP and every
	// Connect attempt below times out at 5s, the reconnector burns its 5
	// retries (~12s of backoff), the circuit breaker trips, and the user
	// is stranded with the kill-switch active and no working tunnel. Pre-fix
	// behaviour was identical because the reconfigure step was simply absent.
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	newRelayIP := tc.RelayIP()
	if newRelayIP != nil {
		if err := prg.ReconfigureForRelay(ctx, newRelayIP); err != nil {
			return ipc.Response{Status: ipc.StatusError, Error: fmt.Sprintf("ipchandler: reconfigure: %v", err)}
		}
	}

	// Stop reconnector and drop current tunnel before switching.
	if r := prg.Reconnector(); r != nil {
		r.Stop()
	}
	_ = tc.Disconnect()

	// Connect through the new relay.
	if err := tc.Connect(ctx); err != nil {
		if r := prg.Reconnector(); r != nil {
			go r.Start(prg.Context())
		}
		return ipc.Response{Status: ipc.StatusError, Error: fmt.Sprintf("ipchandler: reconnect: %v", err)}
	}

	// Story 5.9 — country switch is also a "fresh successful connect" that
	// must lift degraded mode if it was active.
	prg.MaybeRestoreKillSwitch(ctx, "ipc-select-country")

	// Restart reconnector for future automatic reconnections.
	if r := prg.Reconnector(); r != nil {
		go r.Start(prg.Context())
	}

	if cfgPath := opts.ConfigPathFn(); cfgPath != "" {
		if err := persistPreferredCountry(cfgPath, countryCode, opts.IntegrityKey); err != nil {
			return ipc.Response{Status: ipc.StatusError, Error: fmt.Sprintf("ipchandler: save config: %v", err)}
		}
	}

	// Story 4.4 — the user just took explicit control of the country, so
	// drop any stale inter-country failover banner and sync the preferred
	// country into the FailoverManager for the next automatic failover.
	prg.ClearFailoverAlert()
	if fm := prg.FailoverManager(); fm != nil {
		fm.SetPreferredCountry(countryCode)
		fm.SetCurrentRelay(relay.ID)
	}
	prg.SetCurrentCountry(countryCode)

	// Clear stale IP before async detection.
	prg.SetVisibleIP("")
	go prg.DetectVisibleIP(prg.Context())

	return ipc.Response{Status: ipc.StatusConnected}
}

func handleRetryCaptive(prg *svc.Program) ipc.Response {
	prg.RetryCaptiveCheck()
	return ipc.Response{Status: ipc.StatusOK}
}

func handleGetAllowIPv6Leak(prg *svc.Program) ipc.Response {
	return ipc.Response{
		Status:        ipc.StatusOK,
		AllowIPv6Leak: prg.AllowIPv6Leak(),
	}
}

func handleSetAllowIPv6Leak(prg *svc.Program, req ipc.Request, opts Options) ipc.Response {
	if req.Value != "true" && req.Value != "false" {
		return ipc.Response{Status: ipc.StatusError, Error: "invalid_value: must be \"true\" or \"false\""}
	}
	allow := req.Value == "true"

	// Persist to TOML first (inside configMu), then update firewall.
	// If config fails, no firewall change. If firewall fails, rollback
	// config inside the same configMu scope to prevent concurrent divergence.
	cfgPath := opts.ConfigPathFn()
	if cfgPath != "" {
		configMu.Lock()
		cfg, err := config.Load(cfgPath)
		if err != nil {
			configMu.Unlock()
			return ipc.Response{Status: ipc.StatusError, Error: err.Error()}
		}
		oldAllow := cfg.Firewall.AllowIPv6Leak
		cfg.Firewall.AllowIPv6Leak = allow
		if err := cfg.SaveAndSign(cfgPath, opts.IntegrityKey); err != nil {
			configMu.Unlock()
			return ipc.Response{Status: ipc.StatusError, Error: err.Error()}
		}
		// Config saved — now update firewall. On failure, rollback config.
		if err := prg.SetAllowIPv6Leak(allow); err != nil {
			cfg.Firewall.AllowIPv6Leak = oldAllow
			_ = cfg.SaveAndSign(cfgPath, opts.IntegrityKey)
			configMu.Unlock()
			return ipc.Response{Status: ipc.StatusError, Error: err.Error()}
		}
		configMu.Unlock()
	} else {
		// No config file — just update firewall.
		if err := prg.SetAllowIPv6Leak(allow); err != nil {
			return ipc.Response{Status: ipc.StatusError, Error: err.Error()}
		}
	}

	return ipc.Response{Status: ipc.StatusOK, AllowIPv6Leak: allow}
}

// handleGetKillSwitchMode returns the current kill-switch mode (Story 5.9).
// Always succeeds — defaults to "normal" when the in-memory config flag
// reads as enabled.
func handleGetKillSwitchMode(prg *svc.Program) ipc.Response {
	return ipc.Response{
		Status:         ipc.StatusOK,
		KillSwitchMode: prg.KillSwitchMode(),
	}
}

// handleSetKillSwitchMode toggles the OS-level firewall (Story 5.9).
//
// Authentication policy:
//   - req.Auth == ""   → source = "ui", no token check (UI is local-loopback only)
//   - req.Auth != ""   → source = "ctl", token verified in constant time
//     against the machine-local file token. Empty configured token rejects all.
//
// Persistence is owned by the service (SetKillSwitchPersister callback wired
// in cmd/client). Atomicity (firewall ↔ config rollback) is handled internally.
func handleSetKillSwitchMode(prg *svc.Program, req ipc.Request, _ Options) ipc.Response {
	if req.Value != ipc.KillSwitchModeNormal && req.Value != ipc.KillSwitchModeDegraded {
		return ipc.Response{Status: ipc.StatusError, Error: "invalid_value: must be \"normal\" or \"degraded\""}
	}

	source := "ui"
	if req.Auth != "" {
		if !prg.VerifyCtlToken(req.Auth) {
			return ipc.Response{Status: ipc.StatusError, Error: svc.ErrCtlAuthFailed.Error()}
		}
		source = "ctl"
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := prg.SetKillSwitchMode(ctx, req.Value, source); err != nil {
		// Surface the bare sentinel string for known cases so the UI can
		// switch on it (captive_portal_active, tunnel_not_connected, auth_failed).
		return ipc.Response{Status: ipc.StatusError, Error: err.Error()}
	}
	return ipc.Response{Status: ipc.StatusOK, KillSwitchMode: req.Value}
}

// handleTriggerRecovery runs a manual RecoverFromAnomaly sequence
// (Story 6.3 AC9). Intended for operator debugging: an authenticated
// levoile-ctl request forces a full kill-switch-preserving reconnect
// without waiting for the leakcheck or TUN watchdog to fire.
//
// Authentication: always required (req.Auth must match the machine-local
// ctl token). Unlike handleSetKillSwitchMode which accepts UI traffic on
// empty Auth, trigger_recovery is intentionally ctl-only — the UI has
// no reason to expose this, and bypassing the token would let any
// loopback client force reconnect loops.
//
// Concurrency: if a recovery is already running, we short-circuit with
// AnomalyActive=true so the operator knows their trigger piggybacked on
// the in-flight sequence (H2/M2 review fix — was silently reporting OK).
//
// Lifecycle: the background goroutine derives its context from the
// service lifecycle ctx (prg.Context()), not context.Background(). If
// the service shuts down mid-recovery, the ctx cancels and recoverTUN
// aborts cleanly instead of racing with shutdown().
func handleTriggerRecovery(prg *svc.Program, req ipc.Request) ipc.Response {
	if req.Auth == "" || !prg.VerifyCtlToken(req.Auth) {
		return ipc.Response{Status: ipc.StatusError, Error: svc.ErrCtlAuthFailed.Error()}
	}

	tc := prg.TunnelClient()
	if tc == nil || tc.State().Get() != tunnel.StateConnected {
		return ipc.Response{Status: ipc.StatusError, Error: "tunnel_not_connected"}
	}

	// M2 fix: surface "already running" as a separate response so the
	// operator doesn't get a false "déclenchée" confirmation when their
	// trigger was effectively a no-op. The race with the goroutine below
	// is acceptable: in the rare case another trigger acquires the mutex
	// between this check and the TryLock, we'll emit StatusOK with
	// AnomalyActive=false — indistinguishable from a successful launch,
	// which is the user-visible truth anyway.
	if prg.AnomalyActive() {
		return ipc.Response{Status: ipc.StatusOK, AnomalyActive: true, AnomalyReason: prg.AnomalyReason()}
	}

	// Fire-and-forget so the IPC reply doesn't block for the ~10-30s of
	// real recovery work. The parent context is the service lifecycle so
	// shutdown cleanly cancels the recovery (H1 fix).
	parent := prg.Context()
	if parent == nil {
		parent = context.Background()
	}
	go func() {
		ctx, cancel := context.WithTimeout(parent, 60*time.Second)
		defer cancel()
		_ = prg.RecoverFromAnomaly(ctx, anomaly.ReasonManual)
	}()

	return ipc.Response{Status: ipc.StatusOK}
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
