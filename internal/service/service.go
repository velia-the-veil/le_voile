// Package service manages the OS-level system service lifecycle.
package service

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"net"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"time"

	"github.com/kardianos/service"

	"github.com/velia-the-veil/le_voile/internal/blocklist"
	"github.com/velia-the-veil/le_voile/internal/dns"
	"github.com/velia-the-veil/le_voile/internal/httpproxy"
	"github.com/velia-the-veil/le_voile/internal/leakcheck"
	"github.com/velia-the-veil/le_voile/internal/registry"
	"github.com/velia-the-veil/le_voile/internal/stun"
	"github.com/velia-the-veil/le_voile/internal/tunnel"
	"github.com/velia-the-veil/le_voile/internal/updater"
	"github.com/velia-the-veil/le_voile/internal/watchdog"
)

// serviceStderr is the writer for error output. Defaults to os.Stderr.
// Overridable in tests.
var serviceStderr io.Writer = os.Stderr

// ServiceName is the OS service name used for registration.
const ServiceName = "LeVoile"

// rollbackTimeout is the maximum time to wait for a tunnel connection after
// a fresh install. If the tunnel doesn't connect within this time, a rollback
// is triggered. Only applies when a new binary was just installed.
const rollbackTimeout = 30 * time.Second

// Config holds the parameters needed to construct a Program.
type Config struct {
	RelayDomain       string
	RelayPubKey       string
	Insecure          bool   // skip TLS verification (development only)
	STUNDefaultServer string // default STUN server for relay (e.g. "stun.l.google.com:19302")
	UpdateEnabled     bool
	UpdateInterval    time.Duration
	UpdateRateLimit   int64  // bytes per second
	UpdateOwner       string // GitHub owner
	UpdateRepo        string // GitHub repo
	UpdateStagingDir  string // staging directory path
	UpdatePubKey      string // Ed25519 public key for update signature verification (falls back to RelayPubKey if empty)
	BlocklistEnabled  bool
	BlocklistInterval time.Duration

	RegistryEnabled         bool
	RegistryURL             string
	RegistryMasterPubKey    string
	RegistryRefreshInterval time.Duration

	HTTPProxyEnabled bool
	HTTPProxyPort    int
}

// Program implements kardianos/service.Interface for lifecycle management.
type Program struct {
	config Config

	tunnelClient    *tunnel.Client
	dnsManager      dns.DNSManager
	killSwitch      *dns.KillSwitch
	reconnector     *tunnel.Reconnector
	watchdog        *watchdog.Watchdog
	stunInterceptor *stun.Interceptor
	stunRelayer     *stun.Relayer
	updater         *updater.Updater
	installer       *updater.Installer
	leakScheduler    *leakcheck.PeriodicScheduler
	discoverer       *registry.Discoverer
	failoverMgr      *registry.FailoverManager
	blocklistManager *blocklist.Manager
	blMu             sync.Mutex  // protects blocklistManager field
	toggleMu         sync.Mutex  // serializes Enable/DisableBlocklist end-to-end
	blocklistActive  atomic.Bool // runtime toggle, thread-safe
	startTime        time.Time
	svc              service.Service // kardianos/service instance; used to restart after rollback

	// ipcHandler is set externally via SetIPCServer to avoid circular imports.
	ipcStart func(ctx context.Context) error
	ipcStop  func()

	ctx    context.Context
	cancel context.CancelFunc
	done   chan struct{}
	mu     sync.Mutex

	// updateMu protects update-related state accessed by IPC handlers.
	updateMu             sync.Mutex
	pendingUpdateVersion string // set by onUpdateReady callback
	installedVersion     string // set when Install succeeds at startup
	lastInstallError     string // set when Install fails at startup
	rollbackOccurred     bool   // set when a rollback was performed
	rollbackVersion      string // version that failed and was rolled back
	rollbackReason       string // reason for the rollback

	// proxyMu protects proxy lifecycle (stop/restart for kill switch).
	proxyMu       sync.Mutex
	proxyCancel   context.CancelFunc
	proxyErrCh    chan error
	proxyV6ErrCh  chan error
	proxy         *dns.Proxy // IPv4 proxy ref
	proxyV6       *dns.Proxy // IPv6 proxy ref

	// stunMu protects STUN interceptor lifecycle.
	stunMu     sync.Mutex
	stunCancel context.CancelFunc
	stunErrCh  chan error

	// httpProxyMu protects HTTP proxy lifecycle.
	httpProxyMu     sync.Mutex
	httpProxyCancel context.CancelFunc
	httpProxyErrCh  chan error
	httpProxy       *httpproxy.Server
	httpProxyActive atomic.Bool
	httpProxySeq    atomic.Uint64
	httpProxyAddr   atomic.Value // string
}

// NewProgram creates a Program with the given configuration.
func NewProgram(cfg Config) *Program {
	return &Program{
		config: cfg,
	}
}

// SetIPCServer registers IPC start/stop callbacks to be called during lifecycle.
func (p *Program) SetIPCServer(start func(ctx context.Context) error, stop func()) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.ipcStart = start
	p.ipcStop = stop
}

// Start implements service.Interface. It MUST NOT block.
func (p *Program) Start(s service.Service) error {
	p.svc = s
	p.ctx, p.cancel = context.WithCancel(context.Background())
	p.done = make(chan struct{})
	go p.run()
	return nil
}

// shutdownTimeout is the maximum time Stop waits for graceful shutdown
// before returning to let the OS terminate the process.
const shutdownTimeout = 10 * time.Second

// Stop implements service.Interface. It MUST block until shutdown is complete.
// If shutdown takes longer than shutdownTimeout, Stop returns anyway so the
// OS service manager doesn't kill the process before DNS is restored.
func (p *Program) Stop(s service.Service) error {
	slog.Info("[diag] Stop: initiating shutdown")
	p.cancel()
	select {
	case <-p.done:
		slog.Info("[diag] Stop: graceful shutdown complete")
	case <-time.After(shutdownTimeout):
		slog.Error("[diag] Stop: shutdown timed out, forcing exit")
	}
	return nil
}

// TunnelClient returns the tunnel client (used by IPC handler).
func (p *Program) TunnelClient() *tunnel.Client {
	return p.tunnelClient
}

// DNSManager returns the DNS manager (used by IPC handler).
func (p *Program) DNSManager() dns.DNSManager {
	return p.dnsManager
}

// Reconnector returns the reconnector (used by IPC handler to pause/resume).
func (p *Program) Reconnector() *tunnel.Reconnector {
	return p.reconnector
}

// Context returns the service lifecycle context.
func (p *Program) Context() context.Context {
	return p.ctx
}

// StartTime returns the service start time.
func (p *Program) StartTime() time.Time {
	return p.startTime
}

// Cancel triggers the service shutdown by cancelling the lifecycle context.
func (p *Program) Cancel() {
	if p.cancel != nil {
		p.cancel()
	}
}

// Updater returns the auto-updater (used by IPC handler). May be nil if updates are disabled.
func (p *Program) Updater() *updater.Updater {
	return p.updater
}

// Installer returns the update installer (used by IPC handler). May be nil if updates are disabled.
func (p *Program) Installer() *updater.Installer {
	return p.installer
}

// PendingUpdateVersion returns the version of a pending update (if any).
func (p *Program) PendingUpdateVersion() string {
	p.updateMu.Lock()
	defer p.updateMu.Unlock()
	return p.pendingUpdateVersion
}

// InstalledVersion returns the version installed at last startup (if any).
func (p *Program) InstalledVersion() string {
	p.updateMu.Lock()
	defer p.updateMu.Unlock()
	return p.installedVersion
}

// LastInstallError returns the error from the last install attempt (if any).
func (p *Program) LastInstallError() string {
	p.updateMu.Lock()
	defer p.updateMu.Unlock()
	return p.lastInstallError
}

// RollbackOccurred returns true if a rollback was performed in this session.
func (p *Program) RollbackOccurred() bool {
	p.updateMu.Lock()
	defer p.updateMu.Unlock()
	return p.rollbackOccurred
}

// RollbackVersion returns the version that failed and was rolled back.
func (p *Program) RollbackVersion() string {
	p.updateMu.Lock()
	defer p.updateMu.Unlock()
	return p.rollbackVersion
}

// RollbackReason returns the reason for the rollback.
func (p *Program) RollbackReason() string {
	p.updateMu.Lock()
	defer p.updateMu.Unlock()
	return p.rollbackReason
}

// STUNActive reports whether the STUN interceptor is currently running.
func (p *Program) STUNActive() bool {
	p.stunMu.Lock()
	interceptor := p.stunInterceptor
	p.stunMu.Unlock()
	if interceptor == nil {
		return false
	}
	return interceptor.Active()
}

// LeakScheduler returns the periodic leak scheduler (used by IPC handler). May be nil.
func (p *Program) LeakScheduler() *leakcheck.PeriodicScheduler {
	return p.leakScheduler
}

// BlocklistManager returns the blocklist manager (used by IPC handler and DNS proxy). May be nil.
func (p *Program) BlocklistManager() *blocklist.Manager {
	return p.blocklistManager
}

// BlocklistActive reports whether blocklist filtering is currently active.
func (p *Program) BlocklistActive() bool {
	return p.blocklistActive.Load()
}

// HTTPProxyActive reports whether the HTTP proxy is currently running.
func (p *Program) HTTPProxyActive() bool {
	return p.httpProxyActive.Load()
}

// HTTPProxyAddr returns the address the HTTP proxy is listening on.
func (p *Program) HTTPProxyAddr() string {
	v := p.httpProxyAddr.Load()
	if v == nil {
		return ""
	}
	return v.(string)
}

// HTTPProxySeq returns the monotone sequence number for proxy state changes.
func (p *Program) HTTPProxySeq() uint64 {
	return p.httpProxySeq.Load()
}

// EnableHTTPProxy starts the HTTP proxy at runtime.
func (p *Program) EnableHTTPProxy() error {
	return p.startHTTPProxy(p.ctx)
}

// DisableHTTPProxy stops the HTTP proxy at runtime.
func (p *Program) DisableHTTPProxy() {
	p.stopHTTPProxy()
}

// EnableBlocklist activates DNS blocklist filtering at runtime.
// Creates and starts the Manager if it was never started, then injects it into
// both proxies. Safe to call while the service is running.
// toggleMu serializes concurrent Enable/Disable calls end-to-end, preventing
// interleaved Store+SetBlocklist sequences from producing inconsistent state.
func (p *Program) EnableBlocklist() {
	p.toggleMu.Lock()
	defer p.toggleMu.Unlock()

	p.blMu.Lock()
	if p.blocklistManager == nil {
		interval := p.config.BlocklistInterval
		if interval == 0 {
			interval = 24 * time.Hour
		}
		blMgr := blocklist.NewManager(interval)
		p.blocklistManager = blMgr
		go blMgr.Start(p.ctx)
	}
	blMgr := p.blocklistManager
	p.blMu.Unlock()

	p.blocklistActive.Store(true)

	p.proxyMu.Lock()
	proxy, proxyV6 := p.proxy, p.proxyV6
	p.proxyMu.Unlock()

	if proxy != nil {
		proxy.SetBlocklist(blMgr)
	}
	if proxyV6 != nil {
		proxyV6.SetBlocklist(blMgr)
	}
}

// DisableBlocklist deactivates DNS blocklist filtering at runtime.
// The Manager is kept running so re-activation is instant (no re-download).
func (p *Program) DisableBlocklist() {
	p.toggleMu.Lock()
	defer p.toggleMu.Unlock()

	p.blocklistActive.Store(false)

	p.proxyMu.Lock()
	proxy, proxyV6 := p.proxy, p.proxyV6
	p.proxyMu.Unlock()

	if proxy != nil {
		proxy.SetBlocklist(nil)
	}
	if proxyV6 != nil {
		proxyV6.SetBlocklist(nil)
	}
}

// run executes the full lifecycle: IPC start -> tunnel connect -> proxy start ->
// DNS set -> watchdog start -> reconnector start. It blocks until context is
// cancelled, then performs shutdown in reverse order.
func (p *Program) run() {
	defer close(p.done)

	ctx := p.ctx
	p.startTime = time.Now()

	// --- 0. Start IPC server early so the tray can always connect ---
	// This must happen before tunnel connect: if the tunnel fails, the tray
	// should still be able to show "Disconnected" rather than "IPC not connected".
	p.mu.Lock()
	ipcStart := p.ipcStart
	p.mu.Unlock()
	if ipcStart != nil {
		go func() {
			if err := ipcStart(ctx); err != nil {
				fmt.Fprintf(serviceStderr, "service: ipc start: %v\n", err)
			}
		}()
	}

	// --- 0a. Check for staged update and install before anything else ---
	if p.config.UpdateEnabled && p.config.UpdateStagingDir != "" {
		p.tryInstallStagedUpdate(ctx)
	}

	// --- 0b. Dynamic relay discovery (if registry enabled) ---
	relayDomain := p.config.RelayDomain
	relayPubKey := p.config.RelayPubKey

	if p.config.RegistryEnabled {
		regClient, regErr := registry.NewClient(
			p.config.RegistryURL,
			p.config.RegistryMasterPubKey,
			registry.WithRefreshInterval(p.config.RegistryRefreshInterval),
		)
		if regErr == nil {
			homeDir, _ := os.UserConfigDir()
			cachePath := filepath.Join(homeDir, "LeVoile", "relay-cache.toml")
			cache := registry.NewCache(cachePath)
			defaultRelay := registry.RelayEntry{
				ID:        "default",
				Domain:    relayDomain,
				PublicKey: relayPubKey,
			}

			// Create latency checker for relay selection by latency (Story 9.2).
			latencyChecker := registry.NewLatencyChecker()
			p.discoverer = registry.NewDiscoverer(regClient, cache, defaultRelay,
				registry.WithLatencyChecker(latencyChecker))

			relays, discoverErr := p.discoverer.Discover(ctx)
			if discoverErr == nil && len(relays) > 0 {
				relayDomain = relays[0].Domain
				relayPubKey = relays[0].PublicKey
			}
			// If discover fails: keep static relayDomain/relayPubKey — no fatal error.
		}
	}

	// --- 1. Tunnel connect ---
	client, err := tunnel.NewClient(relayDomain, relayPubKey, tunnel.WithInsecure(p.config.Insecure))
	if err != nil {
		fmt.Fprintf(serviceStderr, "service: %v\n", err)
		return
	}
	p.tunnelClient = client

	// Use a timeout for tunnel connect after fresh install to detect bad binaries
	connectCtx := ctx
	var connectCancel context.CancelFunc
	p.updateMu.Lock()
	justInstalled := p.installedVersion != ""
	p.updateMu.Unlock()
	if justInstalled {
		connectCtx, connectCancel = context.WithTimeout(ctx, rollbackTimeout)
	}

	if err := client.Connect(connectCtx); err != nil {
		if connectCancel != nil {
			connectCancel()
		}
		fmt.Fprintf(serviceStderr, "service: connect: %v\n", err)
		// Attempt rollback if this failure happened after a fresh install.
		// On success, schedule an OS service restart so the restored binary is loaded.
		// Retrying the tunnel in the current process would still execute the new binary's
		// code — a proper restart is required to pick up the restored binary from disk.
		if p.tryRollbackIfNeeded(ctx, err) {
			p.scheduleServiceRestart()
		}
		// Clean up tunnel client that never connected to avoid leaking resources.
		p.tunnelClient = nil
		return
	} else if connectCancel != nil {
		connectCancel()
	}

	// --- 1b. Confirm new version works / Cleanup backup after successful tunnel connect ---
	if p.config.UpdateStagingDir != "" {
		if err := updater.ClearRollbackState(p.config.UpdateStagingDir); err != nil {
			fmt.Fprintf(serviceStderr, "service: clear rollback state: %v\n", err)
		}
	}
	if p.installer != nil {
		if err := p.installer.CleanupBackup(); err != nil {
			fmt.Fprintf(serviceStderr, "service: cleanup backup: %v\n", err)
		}
	}

	// --- 1c. Start discoverer periodic refresh (after successful tunnel connect) ---
	if p.discoverer != nil {
		_ = p.discoverer.Start(ctx)
	}

	// --- 1d. Setup failover manager (Story 9.2) ---
	if p.discoverer != nil && len(p.discoverer.Relays()) > 1 {
		p.failoverMgr = registry.NewFailoverManager(
			p.discoverer,
			p.tunnelClient,
			p.tunnelClient.Connect,
		)
		p.failoverMgr.SetCurrentRelay(p.discoverer.Primary().ID)
	}

	// --- 2. Proxy start ---
	if err := p.startProxy(ctx); err != nil {
		fmt.Fprintf(serviceStderr, "service: %v\n", err)
		if p.discoverer != nil {
			p.discoverer.Stop()
		}
		client.Disconnect()
		return
	}

	// --- 2a. HTTP proxy start (if enabled) ---
	if p.config.HTTPProxyEnabled {
		if err := p.startHTTPProxy(ctx); err != nil {
			fmt.Fprintf(serviceStderr, "service: http proxy start: %v\n", err)
			// Non-fatal: continue without HTTP proxy.
		}
	}

	// --- 2b. STUN interceptor start (after tunnel connected, best-effort) ---
	p.startSTUN(ctx)

	// --- 3. DNS set ---
	dnsMgr := dns.NewManager()
	p.dnsManager = dnsMgr
	if err := dnsMgr.SetResolver(ctx, "127.0.0.1"); err != nil {
		fmt.Fprintf(serviceStderr, "service: dns set resolver: %v\n", err)
		p.stopSTUN()
		p.stopProxy()
		if p.discoverer != nil {
			p.discoverer.Stop()
		}
		client.Disconnect()
		return
	}
	// Safety net: if run() exits for ANY reason after DNS was redirected,
	// restore the original resolver so the user isn't left without internet.
	// The normal path (ctx.Done → shutdown) also restores DNS, but this defer
	// catches panics and unexpected early returns.
	dnsRestored := false
	defer func() {
		if !dnsRestored && p.dnsManager != nil {
			restoreCtx := context.Background()
			if err := p.dnsManager.RestoreResolver(restoreCtx); err != nil {
				fmt.Fprintf(serviceStderr, "service: emergency dns restore: %v\n", err)
			}
		}
		// Always restart Dnscache on exit (normal or crash).
		if err := dns.RestartDnscache(); err != nil {
			fmt.Fprintf(serviceStderr, "service: emergency dnscache restart: %v\n", err)
		}
	}()

	// --- 4. Watchdog start ---
	wd := watchdog.NewWatchdog("127.0.0.1", dns.CheckCurrentResolver, dns.ForceResolver)
	p.watchdog = wd
	go wd.Start(ctx)

	// --- 5. Kill switch + Reconnector start ---
	// Wrap stopProxy/startProxy to also disable/enable STUN components.
	ks := dns.NewKillSwitch(dnsMgr, func() {
		p.setSTUNEnabled(false)
		p.stopHTTPProxy()
		p.stopProxy()
	}, func(reconnCtx context.Context) error {
		err := p.startProxy(reconnCtx)
		if err == nil {
			if p.config.HTTPProxyEnabled {
				if hpErr := p.startHTTPProxy(reconnCtx); hpErr != nil {
					fmt.Fprintf(serviceStderr, "service: http proxy restart: %v\n", hpErr)
				}
			}
			p.setSTUNEnabled(true)
		}
		return err
	})
	ks.SetForceResolver(dns.ForceResolver)
	p.killSwitch = ks

	var reconnOpts []tunnel.ReconnectorOption
	reconnOpts = append(reconnOpts, tunnel.WithDisconnectFn(func() error {
		client.ResetTransport()
		return nil
	}))
	if p.failoverMgr != nil {
		reconnOpts = append(reconnOpts, tunnel.WithFailoverFn(p.failoverMgr.HandleFailover))
	}
	reconnector := tunnel.NewReconnector(client.State().Updates(), client.Connect, ks, reconnOpts...)
	p.reconnector = reconnector
	go reconnector.Start(ctx)

	// --- 5b. Leak scheduler start ---
	getPublicIP := func(lkCtx context.Context) (net.IP, error) {
		stunServer := p.config.STUNDefaultServer
		if stunServer == "" {
			stunServer = "stun.l.google.com:19302"
		}
		req := leakcheck.BuildBindingRequest()
		resp, err := client.SendSTUNRelay(lkCtx, req, stunServer)
		if err != nil {
			// Retry once for transient QUIC stream errors.
			select {
			case <-lkCtx.Done():
				return nil, fmt.Errorf("leakcheck: tunnel stun relay: %w", err)
			case <-time.After(500 * time.Millisecond):
			}
			req = leakcheck.BuildBindingRequest()
			resp, err = client.SendSTUNRelay(lkCtx, req, stunServer)
			if err != nil {
				return nil, fmt.Errorf("leakcheck: tunnel stun relay: %w", err)
			}
		}
		return leakcheck.ParseXORMappedAddress(resp)
	}
	checker := leakcheck.NewWebRTCLeakChecker(getPublicIP)

	var lkScheduler *leakcheck.PeriodicScheduler
	onLeak := func(_ *leakcheck.FullLeakReport) {
		// Force tunnel disconnect → Reconnector picks up StateDisconnected and reconnects (AC4).
		if p.tunnelClient != nil {
			_ = p.tunnelClient.Disconnect()
		}
		// Schedule a re-test 30 seconds after the alert (AC4).
		// Calling Disconnect activates the kill switch while the Reconnector
		// re-establishes the tunnel. runCheck skips when KillSwitch.IsActive() (AC5),
		// so we wait for the kill switch to deactivate before triggering the re-test.
		go func() {
			select {
			case <-ctx.Done():
				return
			case <-time.After(30 * time.Second):
			}
			// Wait for kill switch to deactivate (reconnect complete), up to 2 minutes.
			// If the tunnel never reconnects, the periodic tick will catch future leaks.
			retestDeadline := time.NewTimer(2 * time.Minute)
			defer retestDeadline.Stop()
			for ks.IsActive() {
				select {
				case <-ctx.Done():
					return
				case <-retestDeadline.C:
					return
				case <-time.After(2 * time.Second):
				}
			}
			if lkScheduler != nil {
				lkScheduler.TriggerCheck(ctx)
			}
		}()
	}
	lkScheduler = leakcheck.NewPeriodicScheduler(
		10*time.Minute,
		checker,
		ks,
		client.State(),
		onLeak,
		func() { /* onRecovery: tray sees "pass" on next poll */ },
	)
	p.leakScheduler = lkScheduler
	go func() {
		if err := lkScheduler.Start(ctx); err != nil {
			fmt.Fprintf(serviceStderr, "service: leak scheduler start: %v\n", err)
		}
	}()

	// --- 5c. Blocklist manager start (if enabled) ---
	if p.config.BlocklistEnabled {
		interval := p.config.BlocklistInterval
		if interval == 0 {
			interval = 24 * time.Hour
		}
		blMgr := blocklist.NewManager(interval)
		p.blocklistManager = blMgr
		p.blocklistActive.Store(true)
		go func() {
			if err := blMgr.Start(ctx); err != nil {
				fmt.Fprintf(serviceStderr, "service: blocklist manager start: %v\n", err)
			}
		}()

		// Inject blocklist into already-running proxies (startProxy ran before
		// blocklistActive was set, so the proxies have no blocklist yet).
		p.proxyMu.Lock()
		if p.proxy != nil {
			p.proxy.SetBlocklist(blMgr)
		}
		if p.proxyV6 != nil {
			p.proxyV6.SetBlocklist(blMgr)
		}
		p.proxyMu.Unlock()
	}

	// --- 6. (IPC already started in step 0) ---

	// --- 7. Updater start (if enabled) ---
	if p.config.UpdateEnabled && p.config.UpdateStagingDir != "" {
		upd, err := updater.NewUpdater(updater.UpdaterConfig{
			Owner:                p.config.UpdateOwner,
			Repo:                 p.config.UpdateRepo,
			PubKeyBase64:         p.updatePubKey(),
			StagingDir:           p.config.UpdateStagingDir,
			CheckInterval:        p.config.UpdateInterval,
			RateLimitBytesPerSec: p.config.UpdateRateLimit,
		})
		if err != nil {
			fmt.Fprintf(serviceStderr, "service: updater init: %v\n", err)
		} else {
			upd.SetOnUpdateReady(func(version string) {
				p.updateMu.Lock()
				p.pendingUpdateVersion = version
				p.updateMu.Unlock()
			})
			p.updater = upd
			go upd.Start(ctx)
		}
	}

	// --- Wait for shutdown ---
	<-ctx.Done()

	// --- Shutdown sequence (reverse order) ---
	p.shutdown()
	dnsRestored = true
}

// shutdown performs the reverse-order cleanup.
func (p *Program) shutdown() {
	// 0. Stop IPC server
	p.mu.Lock()
	ipcStop := p.ipcStop
	p.mu.Unlock()
	if ipcStop != nil {
		ipcStop()
	}

	// 1. Stop leak scheduler (before reconnector)
	if p.leakScheduler != nil {
		p.leakScheduler.Stop()
	}

	// 1a. Stop discoverer
	if p.discoverer != nil {
		p.discoverer.Stop()
	}

	// 1b. Stop blocklist manager
	if p.blocklistManager != nil {
		p.blocklistManager.Stop()
	}

	// 1c. Stop reconnector
	if p.reconnector != nil {
		p.reconnector.Stop()
	}

	// 2. Stop watchdog
	if p.watchdog != nil {
		p.watchdog.Stop()
	}

	// 2b. Stop STUN interceptor (before DNS restore)
	p.stopSTUN()

	// 2c. Stop HTTP proxy (before kill switch deactivate)
	p.stopHTTPProxy()

	// 3. Deactivate kill switch if active
	if p.killSwitch != nil && p.killSwitch.IsActive() {
		restoreCtx := context.Background()
		if err := p.killSwitch.Deactivate(restoreCtx); err != nil {
			fmt.Fprintf(serviceStderr, "service: kill switch deactivate: %v\n", err)
		}
	}

	// 4. Restore DNS resolver
	if p.dnsManager != nil {
		restoreCtx := context.Background()
		if err := p.dnsManager.RestoreResolver(restoreCtx); err != nil {
			fmt.Fprintf(serviceStderr, "service: dns restore resolver: %v\n", err)
		}
	}

	// 5. Verify DNS restoration via watchdog
	if p.dnsManager != nil && p.watchdog != nil {
		originalDNS := p.dnsManager.OriginalResolver()
		if originalDNS != "" {
			restoreCtx := context.Background()
			if err := p.watchdog.VerifyAndRestore(restoreCtx, originalDNS); err != nil {
				fmt.Fprintf(serviceStderr, "service: watchdog verify: %v\n", err)
			}
		}
	}

	// 6. Stop DNS proxy
	p.stopProxy()

	// 6a. Restart Windows Dnscache service (was stopped to free port 53).
	if err := dns.RestartDnscache(); err != nil {
		fmt.Fprintf(serviceStderr, "service: %v\n", err)
	}

	// 7. Close state channel and disconnect tunnel
	if p.tunnelClient != nil {
		p.tunnelClient.State().Close()
		if err := p.tunnelClient.Disconnect(); err != nil {
			fmt.Fprintf(serviceStderr, "service: disconnect: %v\n", err)
		}
	}
}

// updatePubKey returns the Ed25519 public key for update verification,
// falling back to RelayPubKey if no dedicated key is configured.
func (p *Program) updatePubKey() string {
	if p.config.UpdatePubKey != "" {
		return p.config.UpdatePubKey
	}
	return p.config.RelayPubKey
}

// tryInstallStagedUpdate checks for and installs a staged update at service startup.
func (p *Program) tryInstallStagedUpdate(ctx context.Context) {
	verifier, err := updater.NewVerifier(p.updatePubKey())
	if err != nil {
		fmt.Fprintf(serviceStderr, "service: installer verifier: %v\n", err)
		return
	}

	inst, err := updater.NewInstaller(p.config.UpdateStagingDir, verifier)
	if err != nil {
		fmt.Fprintf(serviceStderr, "service: installer init: %v\n", err)
		return
	}
	p.installer = inst

	staged, err := inst.HasStagedUpdate()
	if err != nil {
		fmt.Fprintf(serviceStderr, "service: check staged update: %v\n", err)
		return
	}
	if staged == nil {
		return
	}

	if err := inst.Install(ctx, staged); err != nil {
		fmt.Fprintf(serviceStderr, "service: install update: %v\n", err)
		p.updateMu.Lock()
		p.lastInstallError = err.Error()
		p.updateMu.Unlock()
		return
	}

	// Mark that a rollback is possible until tunnel confirms working
	if err := updater.WriteRollbackState(p.config.UpdateStagingDir, &updater.RollbackState{
		JustInstalled:    true,
		InstalledVersion: staged.Version,
	}); err != nil {
		fmt.Fprintf(serviceStderr, "service: write rollback state: %v\n", err)
	}

	fmt.Fprintf(serviceStderr, "updater: installed v%s\n", staged.Version)
	p.updateMu.Lock()
	p.installedVersion = staged.Version
	p.updateMu.Unlock()
}

// tryRollbackIfNeeded checks if a rollback should be performed after a tunnel failure.
// Returns true if rollback was performed successfully, false otherwise.
func (p *Program) tryRollbackIfNeeded(ctx context.Context, tunnelErr error) bool {
	if p.config.UpdateStagingDir == "" {
		return false
	}

	state, err := updater.ReadRollbackState(p.config.UpdateStagingDir)
	if err != nil {
		fmt.Fprintf(serviceStderr, "service: read rollback state: %v\n", err)
		return false
	}
	if state == nil || !state.JustInstalled {
		return false
	}

	if p.installer == nil {
		fmt.Fprintf(serviceStderr, "updater: rollback: no installer available\n")
		return false
	}

	// Verify backup exists
	backupPath := p.installer.ExecutablePath() + ".bak"
	if _, err := os.Stat(backupPath); err != nil {
		fmt.Fprintf(serviceStderr, "updater: rollback: no backup found: %v\n", err)
		return false
	}

	// Perform rollback
	if err := p.installer.Rollback(); err != nil {
		fmt.Fprintf(serviceStderr, "updater: rollback: restore failed: %v\n", err)
		return false
	}

	// Mark version as failed to prevent re-download
	if err := updater.WriteFailedVersion(p.config.UpdateStagingDir, state.InstalledVersion); err != nil {
		fmt.Fprintf(serviceStderr, "updater: rollback: write failed version: %v\n", err)
	}

	// Clear rollback state
	if err := updater.ClearRollbackState(p.config.UpdateStagingDir); err != nil {
		fmt.Fprintf(serviceStderr, "updater: rollback: clear state: %v\n", err)
	}

	// Update rollback status fields and clear stale install state.
	// installedVersion is no longer accurate — the version was rolled back.
	// Clearing it also prevents a spurious 30s tunnel timeout if the service
	// is restarted in-process (justInstalled would otherwise still be true).
	p.updateMu.Lock()
	p.rollbackOccurred = true
	p.rollbackVersion = state.InstalledVersion
	p.rollbackReason = tunnelErr.Error()
	p.installedVersion = ""
	p.updateMu.Unlock()

	fmt.Fprintf(serviceStderr, "updater: rollback: restored previous version (v%s failed: %v)\n", state.InstalledVersion, tunnelErr)
	return true
}

// scheduleServiceRestart arranges for the OS service to restart after run() exits.
// Called after a successful rollback so the restored binary is loaded by the service manager.
// If no service reference is available (tests, portable mode), this is a no-op; the OS
// service manager's configured restart policy (e.g., Windows SCM auto-restart) applies.
func (p *Program) scheduleServiceRestart() {
	if p.svc == nil {
		return
	}
	go func() {
		<-p.done // wait for run() to return and close done
		if err := p.svc.Restart(); err != nil {
			fmt.Fprintf(serviceStderr, "service: restart after rollback: %v\n", err)
		}
	}()
}

// SetRollbackState sets the rollback state fields. Intended for use in tests.
func (p *Program) SetRollbackState(occurred bool, version, reason string) {
	p.updateMu.Lock()
	defer p.updateMu.Unlock()
	p.rollbackOccurred = occurred
	p.rollbackVersion = version
	p.rollbackReason = reason
}

// SetLeakScheduler sets the leak scheduler. Intended for use in tests.
func (p *Program) SetLeakScheduler(s *leakcheck.PeriodicScheduler) {
	p.leakScheduler = s
}

// SetInstalledVersion sets the installed version field. Intended for use in tests.
func (p *Program) SetInstalledVersion(version string) {
	p.updateMu.Lock()
	defer p.updateMu.Unlock()
	p.installedVersion = version
}

// startProxy starts the DNS proxy on IPv4 and IPv6 loopback with retry for port release.
func (p *Program) startProxy(proxyCtx context.Context) error {
	p.proxyMu.Lock()
	defer p.proxyMu.Unlock()

	// Stop Windows Dnscache service to free port 53.
	if err := dns.StopDnscache(); err != nil {
		fmt.Fprintf(serviceStderr, "service: %v\n", err)
		// Best-effort: continue and try to bind anyway.
	}

	var lastErr error
	for attempt := 0; attempt < 2; attempt++ {
		proxy := dns.NewProxy(dns.DefaultListenAddr, p.tunnelClient.SendDoHQuery)
		pCtx, pCancel := context.WithCancel(proxyCtx)
		errCh := make(chan error, 1)

		go func() {
			errCh <- proxy.Start(pCtx)
		}()

		select {
		case <-proxy.Ready():
			p.proxyCancel = pCancel
			p.proxyErrCh = errCh

			// Inject blocklist if active (Story 8.2).
			if p.blocklistActive.Load() {
				p.blMu.Lock()
				blMgr := p.blocklistManager
				p.blMu.Unlock()
				if blMgr != nil {
					proxy.SetBlocklist(blMgr)
				}
			}
			p.proxy = proxy

			// Start IPv6 proxy (best-effort — don't fail if IPv6 unavailable)
			proxyV6 := dns.NewProxy(dns.DefaultListenAddrV6, p.tunnelClient.SendDoHQuery)
			v6ErrCh := make(chan error, 1)
			go func() {
				v6ErrCh <- proxyV6.Start(pCtx)
			}()
			if p.blocklistActive.Load() {
				p.blMu.Lock()
				blMgr := p.blocklistManager
				p.blMu.Unlock()
				if blMgr != nil {
					proxyV6.SetBlocklist(blMgr)
				}
			}
			p.proxyV6ErrCh = v6ErrCh
			p.proxyV6 = proxyV6

			return nil
		case err := <-errCh:
			pCancel()
			lastErr = err
			if attempt == 0 {
				time.Sleep(100 * time.Millisecond)
			}
		}
	}
	return fmt.Errorf("service: dns proxy start: %w", lastErr)
}

// stopProxy stops the DNS proxies (IPv4 and IPv6).
func (p *Program) stopProxy() {
	p.proxyMu.Lock()
	cancel := p.proxyCancel
	errCh := p.proxyErrCh
	v6ErrCh := p.proxyV6ErrCh
	p.proxyCancel = nil
	p.proxyErrCh = nil
	p.proxyV6ErrCh = nil
	p.proxy = nil
	p.proxyV6 = nil
	p.proxyMu.Unlock()

	if cancel != nil {
		cancel()
	}
	if errCh != nil {
		<-errCh
	}
	if v6ErrCh != nil {
		<-v6ErrCh
	}
}

// tunnelStateAdapter adapts tunnel.Client to the stun.TunnelStateChecker interface.
type tunnelStateAdapter struct {
	client *tunnel.Client
}

func (a *tunnelStateAdapter) IsConnected() bool {
	return a.client.State().Get() == tunnel.StateConnected
}

// startSTUN starts the STUN interceptor on standard ports (best-effort, non-fatal).
func (p *Program) startSTUN(ctx context.Context) {
	p.stunMu.Lock()
	defer p.stunMu.Unlock()

	var onIntercept stun.InterceptFunc
	if p.tunnelClient != nil {
		defaultServer := p.config.STUNDefaultServer
		if defaultServer == "" {
			defaultServer = "stun.l.google.com:19302"
		}
		relayer := stun.NewRelayer(
			p.tunnelClient,
			&tunnelStateAdapter{client: p.tunnelClient},
			defaultServer,
		)
		p.stunRelayer = relayer
		onIntercept = relayer.HandleIntercept
	}

	interceptor := stun.NewInterceptor(stun.DefaultPort, stun.DefaultTLSPort, nil, onIntercept)
	p.stunInterceptor = interceptor

	sCtx, sCancel := context.WithCancel(ctx)
	p.stunCancel = sCancel
	errCh := make(chan error, 1)
	p.stunErrCh = errCh

	go func() {
		errCh <- interceptor.Start(sCtx)
	}()

	select {
	case <-interceptor.Ready():
		// STUN interceptor started successfully.
	case err := <-errCh:
		// Best-effort: log but don't fail the service.
		fmt.Fprintf(serviceStderr, "service: stun interceptor: %v\n", err)
		sCancel()
		p.stunInterceptor = nil
		p.stunCancel = nil
		p.stunErrCh = nil
	}
}

// setSTUNEnabled enables or disables the STUN interceptor and relayer.
// Used by the kill switch to block STUN traffic during tunnel disconnection.
func (p *Program) setSTUNEnabled(enabled bool) {
	p.stunMu.Lock()
	interceptor := p.stunInterceptor
	relayer := p.stunRelayer
	p.stunMu.Unlock()

	if interceptor != nil {
		interceptor.SetEnabled(enabled)
	}
	if relayer != nil {
		relayer.SetEnabled(enabled)
	}
}

// startHTTPProxy starts the local HTTP CONNECT proxy (pattern: startProxy).
func (p *Program) startHTTPProxy(proxyCtx context.Context) error {
	p.httpProxyMu.Lock()
	defer p.httpProxyMu.Unlock()

	if p.httpProxy != nil {
		return nil // already running
	}

	port := p.config.HTTPProxyPort
	if port == 0 {
		port = 50113
	}
	listenAddr := fmt.Sprintf("127.0.0.1:%d", port)

	srv := httpproxy.NewServer(listenAddr, p.tunnelClient)
	hpCtx, hpCancel := context.WithCancel(proxyCtx)
	errCh := make(chan error, 1)

	go func() {
		errCh <- srv.Start(hpCtx)
	}()

	select {
	case <-srv.Ready():
		p.httpProxyCancel = hpCancel
		p.httpProxyErrCh = errCh
		p.httpProxy = srv
		p.httpProxyActive.Store(true)
		p.httpProxyAddr.Store(srv.ListenAddr())
		p.httpProxySeq.Add(1)
		return nil
	case err := <-errCh:
		hpCancel()
		return fmt.Errorf("http proxy start: %w", err)
	}
}

// stopHTTPProxy stops the local HTTP CONNECT proxy with 5s draining.
func (p *Program) stopHTTPProxy() {
	p.httpProxyMu.Lock()
	cancel := p.httpProxyCancel
	errCh := p.httpProxyErrCh
	srv := p.httpProxy
	p.httpProxyCancel = nil
	p.httpProxyErrCh = nil
	p.httpProxy = nil
	p.httpProxyMu.Unlock()

	if cancel != nil {
		cancel()
	}
	if errCh != nil {
		<-errCh
	}

	// Wait for active CONNECT connections to drain (max 5s).
	if srv != nil {
		wgDone := make(chan struct{})
		go func() {
			srv.WaitGroup().Wait()
			close(wgDone)
		}()
		select {
		case <-wgDone:
		case <-time.After(5 * time.Second):
		}
	}

	wasActive := p.httpProxyActive.Swap(false)
	if wasActive {
		p.httpProxyAddr.Store("")
		p.httpProxySeq.Add(1)
	}
}

// stopSTUN stops the STUN interceptor.
func (p *Program) stopSTUN() {
	p.stunMu.Lock()
	cancel := p.stunCancel
	errCh := p.stunErrCh
	p.stunCancel = nil
	p.stunErrCh = nil
	p.stunInterceptor = nil
	p.stunRelayer = nil
	p.stunMu.Unlock()

	if cancel != nil {
		cancel()
	}
	if errCh != nil {
		<-errCh
	}
}
