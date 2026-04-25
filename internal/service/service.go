// Package service manages the OS-level system service lifecycle.
package service

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/kardianos/service"

	"github.com/velia-the-veil/le_voile/internal/anomaly"
	"github.com/velia-the-veil/le_voile/internal/blocklist"
	"github.com/velia-the-veil/le_voile/internal/browser"
	"github.com/velia-the-veil/le_voile/internal/captive"
	lecrypto "github.com/velia-the-veil/le_voile/internal/crypto"
	"github.com/velia-the-veil/le_voile/internal/dns"
	"github.com/velia-the-veil/le_voile/internal/firewall"
	"github.com/velia-the-veil/le_voile/internal/httpproxy"
	"github.com/velia-the-veil/le_voile/internal/leakcheck"
	"github.com/velia-the-veil/le_voile/internal/preflight"
	"github.com/velia-the-veil/le_voile/internal/registry"
	"github.com/velia-the-veil/le_voile/internal/routing"
	"github.com/velia-the-veil/le_voile/internal/tun"
	tunwatchdog "github.com/velia-the-veil/le_voile/internal/tun/watchdog"
	"github.com/velia-the-veil/le_voile/internal/tunnel"
	"github.com/velia-the-veil/le_voile/internal/uiwatchdog"
	"github.com/velia-the-veil/le_voile/internal/updater"
	"github.com/velia-the-veil/le_voile/internal/watchdog"
)

// serviceStderr is the writer for error output. Defaults to os.Stderr.
// Overridable in tests.
var serviceStderr io.Writer = os.Stderr

// ServiceName is the OS service name used for registration.
const ServiceName = "LeVoile"

// CircuitBreakerAlertMessage is the French user-facing message surfaced to
// the UI when the tunnel Reconnector has exhausted its retry budget.
// Extracted as a constant so future i18n can substitute locale variants.
const CircuitBreakerAlertMessage = "Connexion impossible après 5 tentatives — vérifiez votre réseau"

// rollbackTimeout is the maximum time to wait for a tunnel connection after
// a fresh install. If the tunnel doesn't connect within this time, a rollback
// is triggered. Only applies when a new binary was just installed.
// rollbackTimeout bounds how long the first Connect after a fresh install
// waits before the post-install failure path kicks in. Sized to tolerate a
// slow ADSL + QUIC handshake so a legitimately slow network does not trigger
// a spurious rollback — the classifier in shouldRollbackOnConnectErr
// distinguishes "binary actually broken" from "network just slow" regardless
// of whether the timeout hits.
const rollbackTimeout = 90 * time.Second

// Config holds the parameters needed to construct a Program.
type Config struct {
	RelayDomain           string
	RelayPubKey           string
	Insecure              bool          // skip TLS verification (development only)
	STUNDefaultServer     string        // optional override of the first STUN server for leakcheck
	STUNServers           []string      // optional override of the full STUN server list
	STUNLeakcheckInterval time.Duration // period between two STUN leak checks; default 10m
	UpdateEnabled         bool
	UpdateInterval    time.Duration
	UpdateRateLimit   int64  // bytes per second
	UpdateOwner       string // GitHub owner
	UpdateRepo        string // GitHub repo
	UpdateStagingDir  string // staging directory path
	UpdatePubKey      string // Ed25519 public key for update signature verification (falls back to RelayPubKey if empty)
	// UpdateAllowWhenPackaged, when true, forces auto-update even when the binary
	// was installed by a system package manager. Default false (package manager
	// is authoritative to prevent version conflicts).
	UpdateAllowWhenPackaged bool
	// UpdateMaxInstallRetries caps retries of install per staged version before
	// abandoning it and re-downloading on the next check. Default 3.
	UpdateMaxInstallRetries int
	BlocklistEnabled  bool
	BlocklistInterval time.Duration
	// BlocklistCachePath is the on-disk location where the Manager persists
	// the last successful StevenBlack/hosts payload. Empty disables caching
	// (the Manager then always downloads on Start, taking 5–30 s over the
	// tunnel). Wired from cmd/client/main.go to sit next to config.toml.
	BlocklistCachePath string

	RegistryEnabled         bool
	RegistryURL             string
	RegistryMasterPubKey    string
	RegistryRefreshInterval time.Duration
	// RegistryBootstrapDoHEnabled, when true, wraps the registry HTTP transport
	// in a DoH resolver (Cloudflare + Quad9 by default) so the first lookup of
	// the registry hostname never hits the system resolver. NFR9i.
	RegistryBootstrapDoHEnabled bool
	// RegistryDoHUpstreams overrides the default DoH endpoints. Empty = defaults.
	RegistryDoHUpstreams []string

	HTTPProxyEnabled bool
	HTTPProxyPort    int

	BrowserPoliciesEnabled bool

	PreferredCountry string // ISO 2-letter code from config, used for initial relay selection

	// TUNEnabled active la création de l'interface TUN/Wintun levoile0 au
	// démarrage (Epic 2 — capture L3). Défaut false tant que les stories
	// routing (2.4) / firewall (2.6-2.7) / pump tunnel (1.1 étendue) ne sont
	// pas livrées : créer une TUN sans router le trafic via elle est inutile.
	TUNEnabled bool
	TUNName    string // défaut "levoile0" si vide
	TUNMTU     int    // défaut 1420 si 0

	// FirewallEnabled active le kill switch kernel-level (WFP Windows,
	// nftables Linux). Si false, Activate est no-op (mode dégradé).
	FirewallEnabled bool
	// AllowIPv6Leak when true skips IPv6 block rules in the kill switch,
	// letting native IPv6 bypass the tunnel (Story 2.9).
	AllowIPv6Leak bool

	// CaptiveEnabled active la détection de portail captif Wi-Fi avant
	// l'établissement du tunnel (Story 2.8). Si false, skip du probe.
	CaptiveEnabled  bool
	CaptiveProbeURLs []string // overrides captive.DefaultProbeURLs if non-empty

	// UIWatchdogEnabled active la supervision du processus levoile-ui
	// (Story 5.7 — FR15b). Activé par défaut sur Windows ; ignoré sur
	// Linux (systemd user gère). UIBinaryPath est le chemin absolu du
	// binaire UI à respawner ; vide = chemin auto-derivé du service.
	UIWatchdogEnabled bool
	UIBinaryPath      string
}

// Program implements kardianos/service.Interface for lifecycle management.
type Program struct {
	config Config

	tunDev          tun.Device         // interface TUN/Wintun (Epic 2), nil si TUNEnabled=false
	tunOutbound     chan []byte        // packets read from tunDev, drained by RunPump (Story 1.1 étendue)
	tunWatchdog     *tunwatchdog.Watchdog // watchdog TUN (story 2.2), nil si TUNEnabled=false
	routeMgr        routing.RouteManager // routage système (Epic 2 — story 2.4), nil si TUNEnabled=false
	firewallMgr     firewall.Firewall    // kill switch kernel-level (Epic 2 — stories 2.6/2.7), nil si FirewallEnabled=false
	tunnelClient    *tunnel.Client
	dnsManager      dns.DNSManager
	killSwitch      *dns.KillSwitch
	reconnector     *tunnel.Reconnector
	watchdog        *watchdog.Watchdog
	updater         *updater.Updater
	installer       *updater.Installer
	leakScheduler    *leakcheck.PeriodicScheduler
	// leakRelayResolver caches the active relay's public IP (via DoH) so
	// the leak checker can compare STUN responses. Persisted on Program so
	// failover events can call Invalidate() and force a fresh DoH lookup
	// against the NEW relay — otherwise the cached IP would cause a 5 min
	// window of false LEAK_DETECTED after an inter-country failover.
	leakRelayResolver *leakcheck.RelayIPResolver
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

	ctx      context.Context
	cancel   context.CancelFunc
	done     chan struct{}
	mu       sync.Mutex
	stopOnce sync.Once // ensures Stop/RequestStop shutdown runs only once (AC6)

	// bgWG tracks long-running background goroutines whose lifetime is tied
	// to p.ctx but that do not expose a blocking Stop() (e.g. the updater
	// loop, or the launcher wrapper goroutines). shutdown() waits on it
	// with a timeout so we know everything is unwound before run() returns
	// and close(p.done) fires.
	bgWG sync.WaitGroup

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

	// browserPolicyMu protects browserPolicyMgr and browserPolicyResult.
	browserPolicyMu     sync.Mutex
	browserPolicyMgr    browser.PolicyManager
	browserPolicyResult *browser.ApplyResult

	// httpProxyMu protects HTTP proxy lifecycle.
	httpProxyMu     sync.Mutex
	httpProxyCancel context.CancelFunc
	httpProxyErrCh  chan error
	httpProxy       *httpproxy.Server
	httpProxyActive atomic.Bool
	httpProxySeq    atomic.Uint64
	httpProxyAddr   atomic.Value // string

	visibleIP atomic.Value // string — detected exit IP of the current relay
	realIP    atomic.Value // string — client's real IP detected before tunnel

	// circuitBreakerTripped and circuitBreakerMessage are set when the tunnel
	// Reconnector gives up after CircuitBreakerThreshold consecutive failures.
	// Surfaced to the UI via IPC (GetStatus) so the webview can display a
	// persistent banner until the user triggers a manual reconnect.
	circuitBreakerTripped atomic.Bool
	circuitBreakerMessage atomic.Value // string

	// failoverAlert holds the French user-facing message shown when the
	// FailoverManager crossed a country boundary (Story 4.4). Cleared on
	// ResetCircuitBreaker and SelectCountry.
	failoverAlert atomic.Value // string
	// currentCountry is the ISO2 code of the country hosting the active relay.
	// It may differ from config.PreferredCountry after an inter-country
	// failover — the UI reads it via IPC to show the accurate active flag.
	currentCountry atomic.Value // string

	// firewallAltered is set when the firewall watchdog (Story 2.7) detects
	// third-party rule tampering. Surfaced via IPC GetStatus.
	firewallAltered atomic.Bool

	// firewallRelayIP stores the relay IPv4 used for firewall Activate, so
	// the watchdog re-activate path can access it without re-resolving DNS.
	firewallRelayIP atomic.Value // net.IP

	// concurrentVPNErr stocke la dernière *preflight.ErrConcurrentVPN (story
	// 2.3) détectée au démarrage ou par un appel Connect IPC. Surfacée via
	// GetStatus (ConcurrentVPN bool + Error string FR). Nil = aucune détection.
	concurrentVPNErr atomic.Value // *preflight.ErrConcurrentVPN

	// captivePortal is true when the service is in captive portal mode
	// (firewall lockdown relaxed to LAN gateway only, waiting for user
	// to authenticate on Wi-Fi portal). Story 2.8.
	captivePortal    atomic.Bool
	captiveProbeURL  atomic.Value // string — URL that triggered captive detection
	captiveCancel    context.CancelFunc // cancels the captiveWatcher goroutine
	captiveMu        sync.Mutex // protects captiveCancel

	// preflightDetector, si non-nil, remplace preflightFactory() (tests).
	preflightDetector preflight.VPNDetector
	// preflightLastScan horodate le dernier scan pour throttler handleConnect
	// (fix M3 — éviter un shell-out PowerShell à chaque clic rapide).
	preflightLastScan time.Time

	// uiWatchdog supervise le processus levoile-ui (Story 5.7). nil si
	// UIWatchdogEnabled est false ou si l'OS délègue (Linux → systemd
	// user). Démarré à la fin de run(), arrêté en tête de shutdown().
	uiWatchdog *uiwatchdog.Watchdog

	// killSwitchPersist persists firewall.enable_killswitch to the TOML.
	// Set externally via SetKillSwitchPersister so that internal/service
	// avoids depending on the cmd/client config-path discovery layer.
	// When nil, runtime kill-switch toggles are not persisted (best-effort,
	// callers should always wire it on production paths). Story 5.9.
	killSwitchPersist func(enabled bool) error

	// ctlToken authenticates levoile-ctl IPC requests. Loaded once at
	// service init from a 0600/ACL-restricted file. Compared with
	// crypto/subtle.ConstantTimeCompare to defend against timing oracles
	// (NFR9c). Empty value = ctl auth disabled (UI/IPC paths still work).
	// Story 5.9.
	ctlToken []byte

	// integrityFailed is true when startup HMAC verification of config.toml
	// failed (Story 7.5 / NFR9j). When set, the IPC handler refuses Connect
	// and any config-mutating RPC. Only recovery is out-of-band (stop +
	// delete config + .hmac + restart → Bootstrap regenerates a signed
	// skeleton). No in-process reset path is exposed by design.
	integrityFailed atomic.Bool

	// Story 6.3 — anomaly auto-recovery state.
	// anomalyActive is true while a RecoverFromAnomaly sequence is running.
	// anomalyReasonPtr stores a *string pointing to the current Reason value
	// so readers can access it lock-free. anomalyRecoveryMu serializes
	// concurrent RecoverFromAnomaly invocations (leakcheck + watchdog may
	// both fire in the same window) — a second call TryLock-drops.
	// anomalyLogger writes operator-facing events to Event Log / journald.
	// anomalyNotifier pushes Started/Succeeded/Failed to the UI so the tray
	// icon + webview banner stay in sync.
	anomalyActive      atomic.Bool
	anomalyReasonPtr   atomic.Pointer[string]
	anomalyRecoveryMu  sync.Mutex
	anomalyLogger      anomaly.Logger
	anomalyNotifier    anomaly.Notifier
}

// NewProgram creates a Program with the given configuration.
// resolveSTUNServers builds the ordered STUN server list for the leakcheck
// scheduler. Priority: [stun].servers (full override) > [stun].default_server
// (first-entry override of the built-in list) > built-in defaults.
func resolveSTUNServers(servers []string, defaultServer string) []string {
	if len(servers) > 0 {
		out := make([]string, len(servers))
		copy(out, servers)
		return out
	}
	out := leakcheck.DefaultSTUNServers()
	if defaultServer != "" {
		out[0] = defaultServer
	}
	return out
}

func NewProgram(cfg Config) *Program {
	return &Program{
		config: cfg,
	}
}

// serviceLogger adapts fmt.Fprintf(serviceStderr, ...) to the firewall.Logger
// interface so the firewall package can log through the service's output.
type serviceLogger struct{}

func (l *serviceLogger) Infof(format string, args ...any)  { fmt.Fprintf(serviceStderr, "service: firewall: "+format+"\n", args...) }
func (l *serviceLogger) Warnf(format string, args ...any)  { fmt.Fprintf(serviceStderr, "service: firewall: WARN "+format+"\n", args...) }
func (l *serviceLogger) Errorf(format string, args ...any) { fmt.Fprintf(serviceStderr, "service: firewall: ERROR "+format+"\n", args...) }
func (l *serviceLogger) Debugf(format string, args ...any) { fmt.Fprintf(serviceStderr, "service: firewall: DEBUG "+format+"\n", args...) }

// uiWatchdogLogger adapts the watchdog's Logger interface to serviceStderr
// so supervision events appear in the same stream as the rest of the
// service. NFR22a — no PII (no PID, path, user name) is added by the
// watchdog itself.
type uiWatchdogLogger struct{}

func (l *uiWatchdogLogger) Infof(format string, args ...any)  { fmt.Fprintf(serviceStderr, "service: ui watchdog: "+format+"\n", args...) }
func (l *uiWatchdogLogger) Warnf(format string, args ...any)  { fmt.Fprintf(serviceStderr, "service: ui watchdog: WARN "+format+"\n", args...) }
func (l *uiWatchdogLogger) Errorf(format string, args ...any) { fmt.Fprintf(serviceStderr, "service: ui watchdog: ERROR "+format+"\n", args...) }

// deriveUIBinaryPath returns the conventional path of the levoile-ui
// binary relative to the running service binary. Returns "" if the
// service binary path cannot be determined.
//
// Resolves symlinks before taking the directory so that Linux layouts
// where /usr/bin/levoile-service is a symlink to /opt/levoile/... still
// land on the correct install directory. No-op on Windows where
// os.Executable already returns the real path.
func deriveUIBinaryPath() string {
	self, err := os.Executable()
	if err != nil {
		return ""
	}
	if resolved, err := filepath.EvalSymlinks(self); err == nil {
		self = resolved
	}
	return filepath.Join(filepath.Dir(self), uiBinaryName)
}

// firewallFactory permet d'injecter un Firewall mocké dans les tests.
// Défaut : firewall.New.
var firewallFactory = func(log firewall.Logger, opts firewall.Options) firewall.Firewall {
	return firewall.New(log, opts)
}

// tunFactory permet d'injecter un Device mocké dans les tests sans
// dépendre de privilèges CAP_NET_ADMIN / LocalSystem. Défaut : appelle
// tun.CleanupOrphan + tun.New.
var tunFactory = func(name string, mtu int) (tun.Device, error) {
	if err := tun.CleanupOrphan(name); err != nil {
		return nil, fmt.Errorf("cleanup orphan: %w", err)
	}
	return tun.New(name, mtu)
}

// ensureTUN nettoie une interface orpheline (crash-recovery NFR17 < 5s)
// puis crée levoile0. Échec retourne une erreur — l'appelant décide de la
// fatalité (actuellement non fatal pendant la transition Epic 2).
func (p *Program) ensureTUN() error {
	name := p.config.TUNName
	if name == "" {
		name = "levoile0"
	}
	mtu := p.config.TUNMTU
	if mtu == 0 {
		mtu = 1420
	}
	dev, err := tunFactory(name, mtu)
	if err != nil {
		return fmt.Errorf("tun %s: %w", name, err)
	}
	p.tunDev = dev
	return nil
}

// routingFactory permet d'injecter un RouteManager mocké dans les tests
// sans dépendre de privilèges OS. Défaut : routing.New().
var routingFactory = func() routing.RouteManager {
	return routing.New()
}

// captureOriginalRouteFunc permet d'injecter un stub dans les tests (recoverTUN
// appelle cette variable au lieu de routing.CaptureOriginalRoute directement).
var captureOriginalRouteFunc = routing.CaptureOriginalRoute

// preflightFactory permet d'injecter un VPNDetector mocké dans les tests
// sans dépendre de l'état réseau réel. Défaut : preflight.New (OS-spécifique
// via build tags).
var preflightFactory = func() preflight.VPNDetector {
	return preflight.New(func(level, msg string) {
		fmt.Fprintf(serviceStderr, "service: [%s] %s\n", level, msg)
	})
}

// SetPreflightDetector injecte un VPNDetector (tests). nil revient au factory
// par défaut. Substitue aussi la détection pour les futurs appels.
func (p *Program) SetPreflightDetector(d preflight.VPNDetector) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.preflightDetector = d
	p.preflightLastScan = time.Time{} // reset throttle cache
}

// preflightThrottle est le délai minimum entre deux scans preflight. Évite
// de spawner un processus PowerShell à chaque clic rapide sur Connect (fix M3).
const preflightThrottle = 3 * time.Second

// detectConcurrentVPN exécute le scan preflight (story 2.3) et stocke le
// résultat. Retourne *preflight.ErrConcurrentVPN si détection positive, nil
// sinon. L'erreur est aussi conservée sur p.concurrentVPNErr pour exposition
// IPC (GetStatus). Une détection négative réinitialise l'état.
// Si appelé dans les preflightThrottle secondes suivant le dernier scan,
// retourne le résultat en cache sans re-scanner.
func (p *Program) detectConcurrentVPN() *preflight.ErrConcurrentVPN {
	p.mu.Lock()
	detector := p.preflightDetector
	if !p.preflightLastScan.IsZero() && time.Since(p.preflightLastScan) < preflightThrottle {
		p.mu.Unlock()
		return p.ConcurrentVPNError()
	}
	p.mu.Unlock()
	if detector == nil {
		detector = preflightFactory()
	}
	if detector == nil {
		p.concurrentVPNErr.Store((*preflight.ErrConcurrentVPN)(nil))
		return nil
	}
	err := detector.DetectConcurrentVPN()
	p.mu.Lock()
	p.preflightLastScan = time.Now()
	p.mu.Unlock()
	if err == nil {
		p.concurrentVPNErr.Store((*preflight.ErrConcurrentVPN)(nil))
		return nil
	}
	var e *preflight.ErrConcurrentVPN
	if asErr, ok := err.(*preflight.ErrConcurrentVPN); ok {
		e = asErr
	}
	p.concurrentVPNErr.Store(e)
	return e
}

// DetectConcurrentVPN expose detectConcurrentVPN pour les handlers IPC qui
// réévaluent l'état avant un Connect manuel.
func (p *Program) DetectConcurrentVPN() *preflight.ErrConcurrentVPN {
	return p.detectConcurrentVPN()
}

// ConcurrentVPNError retourne la dernière erreur ErrConcurrentVPN stockée.
// Nil si aucun VPN concurrent n'est actuellement détecté.
func (p *Program) ConcurrentVPNError() *preflight.ErrConcurrentVPN {
	v := p.concurrentVPNErr.Load()
	if v == nil {
		return nil
	}
	e, _ := v.(*preflight.ErrConcurrentVPN)
	return e
}

// SetIntegrityFailed marks the config-integrity state (Story 7.5 / NFR9j).
// When true, the IPC handler rejects Connect and any config-mutating action
// with an "integrity_failed" error so the only exit path is the documented
// out-of-band recovery (stop service → delete config + .hmac → restart).
// Set once at startup by cmd/client after integrity.Verify.
func (p *Program) SetIntegrityFailed(failed bool) {
	p.integrityFailed.Store(failed)
}

// IntegrityFailed reports whether startup config-integrity verification
// detected tampering. Used by the IPC handler to gate actions and by the
// UI to show a permanent warning banner.
func (p *Program) IntegrityFailed() bool {
	return p.integrityFailed.Load()
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

// shutdownStepTimeout bounds each individual teardown step so a single
// stuck system call (WFP probe, policy registry, DNS resolver) cannot
// swallow the whole 10s shutdownTimeout budget and leave later restore
// steps unrun. Sized so 5 consecutive timeouts stay under shutdownTimeout.
const shutdownStepTimeout = 2 * time.Second

// bgWaitTimeout bounds how long shutdown() blocks waiting for launcher
// goroutines (updater, watchdog launchers, ipcStart wrapper) to unwind
// after p.ctx is cancelled. Short on purpose — these goroutines observe
// ctx.Done() on their next tick, so anything above ~1s means something
// is wedged and we prefer to exit rather than stall the SCM.
const bgWaitTimeout = 1500 * time.Millisecond

// shutdownTimeout is the maximum time Stop waits for graceful shutdown
// before returning to let the OS terminate the process.
const shutdownTimeout = 10 * time.Second

// Stop implements service.Interface. It MUST block until shutdown is complete.
// If shutdown takes longer than shutdownTimeout, Stop returns anyway so the
// OS service manager doesn't kill the process before DNS is restored.
// Idempotent via sync.Once — safe to call concurrently (AC6).
func (p *Program) Stop(s service.Service) error {
	p.stopOnce.Do(func() {
		p.cancel()
	})
	select {
	case <-p.done:
	case <-time.After(shutdownTimeout):
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

// CircuitBreakerTripped reports whether the tunnel reconnector has given up
// after CircuitBreakerThreshold consecutive failures.
func (p *Program) CircuitBreakerTripped() bool {
	return p.circuitBreakerTripped.Load()
}

// CircuitBreakerMessage returns the user-facing French message explaining
// the tripped state. Empty when not tripped.
func (p *Program) CircuitBreakerMessage() string {
	if v := p.circuitBreakerMessage.Load(); v != nil {
		if s, ok := v.(string); ok {
			return s
		}
	}
	return ""
}

// tripCircuitBreaker is called by the Reconnector hook when the circuit
// breaker opens. It captures the user-facing message and transitions the
// tunnel state to StateFailed so IPC polling reflects the new state.
func (p *Program) tripCircuitBreaker(message string) {
	p.circuitBreakerMessage.Store(message)
	p.circuitBreakerTripped.Store(true)
	if tc := p.tunnelClient; tc != nil {
		tc.State().Set(tunnel.StateFailed)
	}
}

// FailoverAlert returns the French user-facing message set by an
// inter-country failover. Empty when no cross-country switch is active.
func (p *Program) FailoverAlert() string {
	if v := p.failoverAlert.Load(); v != nil {
		if s, ok := v.(string); ok {
			return s
		}
	}
	return ""
}

// CurrentCountryCode returns the ISO2 code of the country hosting the relay
// the tunnel is currently targeting. May differ from config.PreferredCountry
// after an inter-country failover.
func (p *Program) CurrentCountryCode() string {
	if v := p.currentCountry.Load(); v != nil {
		if s, ok := v.(string); ok {
			return s
		}
	}
	return ""
}

// ClearFailoverAlert drops the current failover banner message. Called from
// IPC handlers when the user takes action that supersedes the alert (manual
// reconnect, explicit country selection).
func (p *Program) ClearFailoverAlert() {
	p.failoverAlert.Store("")
}

func (p *Program) setFailoverAlert(message string) {
	p.failoverAlert.Store(message)
}

// SetCurrentCountry stores the ISO2 code of the active relay's country. Used
// from IPC handlers when the user explicitly picks a country, at service
// startup to record the initial country, and by the failover wrapper after a
// successful relay switch.
func (p *Program) SetCurrentCountry(code string) {
	p.currentCountry.Store(code)
}

// FailoverManager returns the failover manager, used by IPC handlers to
// update the preferred country after SelectCountry.
func (p *Program) FailoverManager() *registry.FailoverManager {
	return p.failoverMgr
}

// UIWatchdogSnapshot returns a point-in-time snapshot of the levoile-ui
// supervisor state (Story 5.7). nil when supervision is disabled (Linux or
// UIWatchdogEnabled=false) so IPC callers can omit the field entirely.
func (p *Program) UIWatchdogSnapshot() *uiwatchdog.Snapshot {
	if p.uiWatchdog == nil {
		return nil
	}
	s := p.uiWatchdog.Snapshot()
	return &s
}

// ResetCircuitBreaker clears the tripped state and resets the Reconnector
// so subsequent StateDisconnected notifications resume the reconnect loop.
// Called from the IPC Connect handler when the user asks to retry.
func (p *Program) ResetCircuitBreaker() {
	p.circuitBreakerTripped.Store(false)
	p.circuitBreakerMessage.Store("")
	p.failoverAlert.Store("")
	if r := p.reconnector; r != nil {
		r.Reset()
	}
}

// ForTestTripCircuitBreaker exposes tripCircuitBreaker for external test
// packages. Do NOT call from production code.
func (p *Program) ForTestTripCircuitBreaker(message string) {
	p.tripCircuitBreaker(message)
}

// ForTestSetTunnelClient injects a tunnel client for tests without running
// the full service lifecycle. Do NOT call from production code.
func (p *Program) ForTestSetTunnelClient(c *tunnel.Client) {
	p.tunnelClient = c
}

// ForTestSetDiscoverer injects a registry discoverer for tests that exercise
// IPC flows like SelectCountry without running the full service lifecycle.
// Do NOT call from production code.
func (p *Program) ForTestSetDiscoverer(d *registry.Discoverer) {
	p.discoverer = d
}

// ForTestInitContext wires a real cancellable lifecycle context onto the
// program without running Start(). Tests use it to directly observe
// whether a handler triggered Cancel / RequestStop by checking
// p.Context().Err() after the call. Do NOT call from production code.
func (p *Program) ForTestInitContext(parent context.Context) {
	p.ctx, p.cancel = context.WithCancel(parent)
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
// Idempotent via sync.Once — safe to call concurrently (AC6).
func (p *Program) Cancel() {
	p.stopOnce.Do(func() {
		if p.cancel != nil {
			p.cancel()
		}
	})
}

// RequestStop asks the OS service manager to stop this service, routing the
// shutdown through the proper SCM / systemd handshake so kardianos's Execute
// loop can do StopPending → Program.Stop → SERVICE_STOPPED cleanly.
//
// Why not just cancel the ctx internally? kardianos's Windows Execute loop
// blocks on the SCM control channel and never observes an internal cancel,
// so a bare Cancel() + os.Exit(0) made SCM classify the exit as "unexpected
// termination" — which with OnFailure=restart (see newServiceConfig in
// cmd/client) caused SCM to relaunch the service ~5 s later, re-arming WFP
// while the UI was gone. Going through svc.Stop avoids that: SCM sees a
// user-initiated stop, delivers svc.Stop to Execute, run() drains via
// <-ctx.Done, and SERVICE_STOPPED is reported before the process exits.
//
// Fallback to Cancel() covers portable mode, tests, and environments where
// svc.Stop itself errors (e.g. the service is not installed so there's no
// SCM entry to stop) — the ctx still gets cancelled so run() can unwind.
// Idempotent via sync.Once on the Cancel side (AC6); repeated svc.Stop
// calls are harmless (SCM returns "service not running" which we swallow).
func (p *Program) RequestStop() {
	p.mu.Lock()
	s := p.svc
	p.mu.Unlock()
	if s == nil {
		p.Cancel()
		return
	}
	go func() {
		if err := s.Stop(); err != nil {
			fmt.Fprintf(serviceStderr, "service: SCM stop failed, falling back to internal cancel: %v\n", err)
			p.Cancel()
		}
	}()
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

// BrowserPolicyApplied returns the list of browsers with policies applied.
func (p *Program) BrowserPolicyApplied() []string {
	p.browserPolicyMu.Lock()
	defer p.browserPolicyMu.Unlock()
	if p.browserPolicyResult == nil {
		return nil
	}
	return p.browserPolicyResult.Applied
}

// BrowserPolicyFailed returns the list of browsers that failed policy application.
func (p *Program) BrowserPolicyFailed() []string {
	p.browserPolicyMu.Lock()
	defer p.browserPolicyMu.Unlock()
	if p.browserPolicyResult == nil {
		return nil
	}
	names := make([]string, len(p.browserPolicyResult.Failed))
	for i, f := range p.browserPolicyResult.Failed {
		names[i] = f.Name + ": " + f.Reason
	}
	return names
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

// Discoverer returns the relay discoverer (used by IPC handler). May be nil.
func (p *Program) Discoverer() *registry.Discoverer {
	return p.discoverer
}

// VisibleIP returns the detected exit IP of the current relay.
func (p *Program) VisibleIP() string {
	v := p.visibleIP.Load()
	if v == nil {
		return ""
	}
	return v.(string)
}

// SetVisibleIP stores the detected exit IP.
func (p *Program) SetVisibleIP(ip string) {
	p.visibleIP.Store(ip)
}

// RealIP returns the client's real IP detected before tunnel connection.
func (p *Program) RealIP() string {
	v := p.realIP.Load()
	if v == nil {
		return ""
	}
	return v.(string)
}

// SetRealIP stores the client's real IP.
func (p *Program) SetRealIP(ip string) {
	p.realIP.Store(ip)
}

// blocklistHTTPClient returns an http.Client that sends the StevenBlack GET
// through the local HTTP CONNECT proxy (which forwards via the relay /connect
// endpoint — the same path browsers use). Without this, the default transport
// routes through levoile0 + runTUNPump + relay /tunnel + NAT, turning an
// ~800 KB download into a 30 s stall on cold-start. Returns nil when the
// local proxy isn't running (VPN disconnected / feature off), letting the
// Manager fall back to its default direct client.
func (p *Program) blocklistHTTPClient() *http.Client {
	if !p.httpProxyActive.Load() {
		return nil
	}
	addr, _ := p.httpProxyAddr.Load().(string)
	if addr == "" {
		return nil
	}
	proxyURL := &url.URL{Scheme: "http", Host: addr}
	return &http.Client{
		Timeout: 30 * time.Second,
		Transport: &http.Transport{
			Proxy: http.ProxyURL(proxyURL),
		},
	}
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
		blMgr := blocklist.NewManagerWithCacheAndClient(interval, p.config.BlocklistCachePath, p.blocklistHTTPClient())
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
		p.bgWG.Add(1)
		go func() {
			defer p.bgWG.Done()
			if err := ipcStart(ctx); err != nil {
				fmt.Fprintf(serviceStderr, "service: ipc start: %v\n", err)
			}
		}()
	}

	// --- 0a. Recover orphan browser policies from previous crash ---
	// Must run in the service (not tray) because HKLM/etc require SYSTEM/root.
	if err := browser.RecoverOrphanPolicies(ctx); err != nil {
		fmt.Fprintf(serviceStderr, "service: recover orphan browser policies: %v\n", err)
	}

	// --- 0c. Check for staged update and install before anything else ---
	if p.config.UpdateEnabled && p.config.UpdateStagingDir != "" {
		p.tryInstallStagedUpdate(ctx)
	}

	// --- 0d. Dynamic relay discovery (if registry enabled) ---
	relayDomain := p.config.RelayDomain
	relayPubKey := p.config.RelayPubKey

	if p.config.RegistryEnabled {
		regOpts := []registry.ClientOption{
			registry.WithRefreshInterval(p.config.RegistryRefreshInterval),
			registry.WithRejectLogger(func(id, domain, reason string) {
				fmt.Fprintf(serviceStderr, "service: registry: entry rejected id=%s domain=%s reason=%s\n", id, domain, reason)
			}),
		}
		if p.config.RegistryBootstrapDoHEnabled {
			dohOpts := []registry.DoHOption{
				registry.WithDoHLogger(func(format string, args ...any) {
					fmt.Fprintf(serviceStderr, "service: registry doh: "+format+"\n", args...)
				}),
			}
			if len(p.config.RegistryDoHUpstreams) > 0 {
				dohOpts = append(dohOpts, registry.WithDoHUpstreams(p.config.RegistryDoHUpstreams))
			}
			doh, dohErr := registry.NewDoHResolver(dohOpts...)
			if dohErr != nil {
				fmt.Fprintf(serviceStderr, "service: registry doh: invalid configuration, falling back to system resolver: %v\n", dohErr)
			} else {
				regOpts = append(regOpts, registry.WithResolver(doh))
				fmt.Fprintf(serviceStderr, "service: registry bootstrap via DoH enabled (NFR9i)\n")
			}
		}
		regClient, regErr := registry.NewClient(
			p.config.RegistryURL,
			p.config.RegistryMasterPubKey,
			regOpts...,
		)
		if regErr != nil {
			// Client init failed (invalid URL or malformed master key). Fallback
			// to static relay is kept intentional for resilience, but surface
			// the reason so operators can diagnose silent misconfig. No client
			// IP or user content is emitted (NFR20/NFR22a).
			fmt.Fprintf(serviceStderr, "service: registry: client init failed, falling back to static relay: %v\n", regErr)
		} else {
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
			switch {
			case discoverErr != nil:
				// Discover/fetch/verify failed — log once and keep static relay.
				fmt.Fprintf(serviceStderr, "service: registry: discover failed, falling back to static relay: %v\n", discoverErr)
			case len(relays) == 0:
				fmt.Fprintf(serviceStderr, "service: registry: discover returned 0 relays, falling back to static relay\n")
			default:
				chosen := relays[0]
				// If preferred_country is set, pick via the same round-robin
				// path used by SelectCountry (AC Story 4.3). Fall back silently
				// to relays[0] if the preferred country has no relay — don't
				// abort startup.
				if p.config.PreferredCountry != "" {
					if picked, err := p.discoverer.SelectRelay(p.config.PreferredCountry); err == nil {
						chosen = picked
					}
				}
				relayDomain = chosen.Domain
				relayPubKey = chosen.PublicKey
			}
		}
	}

	// --- 0e. Detect real IP before tunnel connect ---
	p.DetectRealIP(ctx)

	// --- 0e-bis. Preflight : détection d'un VPN concurrent (story 2.3) ---
	// Scan purement read-only : aucune interface, règle firewall, ni route
	// n'est modifiée. En cas de détection, on boucle toutes les 5s jusqu'à
	// ce que le VPN tiers soit déconnecté ou que ctx soit annulé. IPC reste
	// actif et GetStatus/handleConnect surfacent l'état ConcurrentVPN à l'UI.
	if e := p.detectConcurrentVPN(); e != nil {
		fmt.Fprintf(serviceStderr, "service: preflight: %v\n", e)
		ticker := time.NewTicker(5 * time.Second)
		cleared := false
		for !cleared {
			select {
			case <-ctx.Done():
				ticker.Stop()
				return
			case <-ticker.C:
				if re := p.detectConcurrentVPN(); re == nil {
					fmt.Fprintf(serviceStderr, "service: preflight: VPN concurrent disparu, reprise de la séquence\n")
					cleared = true
				}
			}
		}
		ticker.Stop()
	}

	// --- 0e-bis. Captive portal detection (Story 2.8) ---
	// Runs BEFORE TUN/firewall setup so the probe uses the normal network
	// stack. If a captive portal is detected, we activate firewall in
	// Captive mode (gateway-only) and wait for user authentication.
	if p.config.CaptiveEnabled && p.config.TUNEnabled && p.config.FirewallEnabled {
		probeDetail := captive.Probe(ctx, p.config.CaptiveProbeURLs)
		switch probeDetail.Result {
		case captive.PortalDetected:
			fmt.Fprintf(serviceStderr, "service: captive portal detected url=%s status=%d\n",
				probeDetail.URL, probeDetail.StatusCode)
			p.captivePortal.Store(true)
			p.captiveProbeURL.Store(probeDetail.URL)

			// Activate firewall in captive mode (gateway-only + loopback).
			origGW, _, gwErr := routing.CaptureOriginalRoute()
			if gwErr != nil {
				fmt.Fprintf(serviceStderr, "service: captive: cannot detect LAN gateway: %v\n", gwErr)
				// Fall through — skip captive mode, proceed to normal connect.
			} else {
				fwLog := &serviceLogger{}
				fw := firewallFactory(fwLog, firewall.Options{AllowIPv6Leak: p.config.AllowIPv6Leak})
				if err := fw.Activate(ctx, firewall.ActivateParams{
					Mode:       firewall.ModeCaptive,
					LanGateway: origGW,
				}); err != nil {
					fmt.Fprintf(serviceStderr, "service: captive firewall activate: %v\n", err)
				} else {
					p.firewallMgr = fw
					// Block here until captive is cleared (user clicks retry
					// which cancels captiveCancel, or periodic re-probe succeeds).
					p.waitForCaptiveClear(ctx, origGW)
					// After captive clear, firewallMgr is deactivated in
					// waitForCaptiveClear. Fall through to normal TUN/routing/firewall.
				}
			}
		case captive.ProbeError:
			fmt.Fprintf(serviceStderr, "service: captive probe error (fail-open): %v\n", probeDetail.Err)
			// Fail-open: continue to normal connect.
		default:
			// NoPortal — continue normally.
		}
	}

	// --- 0f. TUN/Wintun interface (Epic 2 — capture L3) ---
	// Opt-in via TUNEnabled : tant que routing (story 2.4) et firewall
	// (stories 2.6/2.7) ne sont pas livrés, créer l'interface sans router le
	// trafic est inutile. Échec non fatal — log + continue (le tunnel QUIC
	// fonctionne indépendamment). Architecture ordre : elevation → tun.New →
	// routing → firewall → tunnel.Connect.
	//
	// tunCleanup ferme l'interface TUN sur les chemins d'erreur anticipés de
	// run() avant que shutdown() ne prenne le relais au ctx.Done normal. Armé
	// après ensureTUN, désarmé juste avant l'attente ctx.Done (à ce moment,
	// shutdown() fermera proprement tunDev).
	tunCleanup := func() {}
	var resolvedRelayIP net.IP // relay IPv4 resolved for routing/firewall
	if p.config.TUNEnabled {
		if err := p.ensureTUN(); err != nil {
			fmt.Fprintf(serviceStderr, "service: tun setup: %v\n", err)
		} else {
			// Start the long-lived TUN reader before any other consumer of
			// the device. The reader populates p.tunOutbound which
			// runTUNPump (started after Connect) drains.
			p.startTUNReader(ctx)
			tunCleanup = func() {
				// Ordre strict shutdown erreur : firewall → routing → tun.
				if p.firewallMgr != nil {
					if derr := p.firewallMgr.Deactivate(context.Background()); derr != nil {
						fmt.Fprintf(serviceStderr, "service: firewall deactivate (error path): %v\n", derr)
					}
					p.firewallMgr = nil
				}
				if p.routeMgr != nil {
					if terr := p.routeMgr.Teardown(); terr != nil {
						fmt.Fprintf(serviceStderr, "service: routing teardown (error path): %v\n", terr)
					}
					p.routeMgr = nil
				}
				if p.tunDev != nil {
					if cerr := p.tunDev.Close(); cerr != nil {
						fmt.Fprintf(serviceStderr, "service: tun close (error path): %v\n", cerr)
					}
					p.tunDev = nil
				}
			}

			// --- 0g. Routing cleanup (NFR17 < 5s) puis setup ---
			// Ordre : tun.New → routing.Cleanup → routing.Setup → firewall
			rm := routingFactory()
			if err := rm.Cleanup(); err != nil {
				fmt.Fprintf(serviceStderr, "service: routing cleanup: %v\n", err)
			}

			// Résoudre l'IP du relais pour la route /32 anti-loop.
			relayIPs, err := net.LookupIP(relayDomain)
			if err != nil || len(relayIPs) == 0 {
				fmt.Fprintf(serviceStderr, "service: routing: cannot resolve relay %s: %v\n", relayDomain, err)
			} else {
				// Prendre la première IPv4.
				for _, ip := range relayIPs {
					if ip4 := ip.To4(); ip4 != nil {
						resolvedRelayIP = ip4
						break
					}
				}
				if resolvedRelayIP == nil {
					fmt.Fprintf(serviceStderr, "service: routing: no IPv4 for relay %s\n", relayDomain)
				} else {
					// Capturer gateway + interface par défaut en un seul
					// appel atomique (AC3 — pas de TOCTOU).
					origGW, origIface, gwErr := routing.CaptureOriginalRoute()
					if gwErr != nil {
						fmt.Fprintf(serviceStderr, "service: routing: %v\n", gwErr)
					} else {
						if err := rm.Setup(p.tunDev.Name(), resolvedRelayIP, origGW, origIface); err != nil {
							fmt.Fprintf(serviceStderr, "service: routing setup: %v\n", err)
							// Non fatal — continue sans routage L3.
						} else {
							p.routeMgr = rm
						}
					}
				}
			}

			// --- 0h. Firewall kill switch (Stories 2.6/2.7) ---
			// Ordre strict : tun.New → routing.Setup → firewall.Activate → tunnel.Connect
			// Ne pas activer si FirewallEnabled=false (mode dégradé) ou si
			// TUN/routing ont échoué (pas de LUID, pas de relay IP).
			if p.config.FirewallEnabled && resolvedRelayIP != nil && p.tunDev != nil {
				p.firewallRelayIP.Store(resolvedRelayIP)
				fwLog := &serviceLogger{}
				fw := firewallFactory(fwLog, firewall.Options{AllowIPv6Leak: p.config.AllowIPv6Leak})
				// Crash-recovery: nettoyer d'éventuels orphelins d'un crash précédent.
				if n, err := fw.CleanupOrphans(ctx); err != nil {
					fmt.Fprintf(serviceStderr, "service: firewall cleanup orphans: %v\n", err)
				} else if n > 0 {
					fmt.Fprintf(serviceStderr, "service: firewall orphans cleaned: %d\n", n)
				}
				if err := fw.Activate(ctx, firewall.ActivateParams{Mode: firewall.ModeFull, RelayIP: resolvedRelayIP, TunName: p.tunDev.Name()}); err != nil {
					fmt.Fprintf(serviceStderr, "service: firewall activate: %v\n", err)
					// Non fatal — continue sans kill switch réseau.
				} else {
					p.firewallMgr = fw
					// Observer le channel d'altération tiers (watchdog).
					if ch := fw.AlteredCh(); ch != nil {
						go p.watchFirewallAltered(ctx, ch)
					}
				}
			}
		}
	}

	// --- 1. Tunnel connect ---
	client, err := tunnel.NewClient(relayDomain, relayPubKey, tunnel.WithInsecure(p.config.Insecure))
	if err != nil {
		fmt.Fprintf(serviceStderr, "service: %v\n", err)
		tunCleanup()
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

	// Known gap (review finding F1): this Connect runs unconditionally —
	// cfg.Client.AutoStart only gates the OS-level auto-start surfaces
	// (SCM startup type + ONLOGON scheduled task). Once the service has
	// been started by any path (boot, manual sc start, ensureService from
	// a user-launched UI), it will auto-connect the tunnel. Fixing this
	// cleanly requires extracting the post-Connect setup (TUN watchdog,
	// pump, discoverer, reconnector, leak scheduler, blocklist) into a
	// function invocable from handleConnect on first use. Deferred:
	// architectural and outside the scope of the original user report
	// "VPN started automatically at next boot" which is addressed by the
	// OS-level gate.
	if err := client.Connect(connectCtx); err != nil {
		if connectCancel != nil {
			connectCancel()
		}
		fmt.Fprintf(serviceStderr, "service: connect: %v\n", err)
		// Post-install rollback is gated to errors that actually implicate
		// the new binary (bad signature, cert pinning mismatch, verify
		// failure). A plain network timeout, DNS failure, or transient
		// connection refused on a lossy ADSL is classified as "not the
		// binary" and left to the normal reconnect loop — burning a
		// rollback on a slow uplink would ship users back to the previous
		// version and trap them there until the next update cycle.
		if shouldRollbackOnConnectErr(err) {
			if p.tryRollbackIfNeeded(ctx, err) {
				p.scheduleServiceRestart()
			}
		} else if justInstalled {
			fmt.Fprintf(serviceStderr, "service: connect: transient failure after fresh install, skipping rollback: %v\n", err)
		}
		// Clean up tunnel client that never connected to avoid leaking resources.
		p.tunnelClient = nil
		tunCleanup()
		return
	} else if connectCancel != nil {
		connectCancel()
	}

	// --- 1a. TUN watchdog start (Story 2.2 — NFR16 < 5s) ---
	// Lancé après tunnel.Connect réussi. Surveille la disparition/altération de
	// levoile0 et déclenche un reconnect complet si détecté.
	if p.tunDev != nil {
		p.startTUNWatchdog(ctx)
	}

	// --- 1a-bis. TUN ↔ relay pump (Story 1.1 étendue) ---
	// L'interface levoile0 reçoit des paquets IP du kernel via la route /0
	// posée par routing.Setup. Sans pump, ces paquets stagnent dans le ring
	// buffer Wintun jusqu'à overflow → black-hole → DNS/ICMP/TCP coupés
	// pendant que le killswitch WFP bloque tout autre chemin. La goroutine
	// runTUNPump ouvre POST /tunnel vers le relais et fait circuler les
	// paquets dans les deux sens. Auto-restart sur erreur (post-reconnect).
	if p.tunDev != nil && p.tunnelClient != nil {
		go p.runTUNPump(ctx)
	}

	// --- 1b. Detect visible exit IP (best-effort, non-blocking) ---
	go p.DetectVisibleIP(ctx)

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

	// --- 1d. Setup failover manager (Story 4.4 — country-aware) ---
	if p.discoverer != nil && len(p.discoverer.Relays()) > 1 {
		p.failoverMgr = registry.NewFailoverManager(
			p.discoverer,
			p.tunnelClient,
			p.tunnelClient.Connect,
		)
		primary := p.discoverer.Primary()
		p.failoverMgr.SetCurrentRelay(primary.ID)
		p.failoverMgr.SetPreferredCountry(p.config.PreferredCountry)
		// Record the initial country so IPC consumers see the active flag
		// before any failover happens.
		initialCountry := registry.ExtractCountryCode(primary.ID, primary.Domain)
		if initialCountry == "" {
			initialCountry = p.config.PreferredCountry
		}
		p.SetCurrentCountry(initialCountry)
	}

	// --- 2. Proxy start ---
	if err := p.startProxy(ctx); err != nil {
		fmt.Fprintf(serviceStderr, "service: %v\n", err)
		if p.discoverer != nil {
			p.discoverer.Stop()
		}
		client.Disconnect()
		tunCleanup()
		return
	}

	// --- 2a. HTTP proxy start (if enabled) ---
	if p.config.HTTPProxyEnabled {
		if err := p.startHTTPProxy(ctx); err != nil {
			fmt.Fprintf(serviceStderr, "service: http proxy start: %v\n", err)
			// Non-fatal: continue without HTTP proxy.
		}
	}

	// --- 2b. Browser policies (before DNS to close WebRTC race window) ---
	// Story 6.1: STUN interceptor removed — post-Epic-2 the L3 capture
	// handles the STUN pass-through structurally; no applicative relay.
	if p.config.BrowserPoliciesEnabled {
		bpMgr := browser.NewPolicyManager()
		result, bpErr := bpMgr.ApplyPolicies(ctx)
		if bpErr != nil {
			fmt.Fprintf(serviceStderr, "service: browser policies apply: %v\n", bpErr)
			// Non-fatal: continue without browser policies.
		}
		p.browserPolicyMu.Lock()
		p.browserPolicyMgr = bpMgr
		p.browserPolicyResult = result
		p.browserPolicyMu.Unlock()
	}

	// --- 3. DNS set ---
	dnsMgr := dns.NewManager()
	p.dnsManager = dnsMgr
	if err := dnsMgr.SetResolver(ctx, "127.0.0.1"); err != nil {
		fmt.Fprintf(serviceStderr, "service: dns set resolver: %v\n", err)
		p.stopProxy()
		if p.discoverer != nil {
			p.discoverer.Stop()
		}
		client.Disconnect()
		tunCleanup()
		return
	}
	// Safety net: if run() exits for ANY reason after DNS was redirected,
	// restore the original resolver so the user isn't left without internet.
	// The normal path (ctx.Done → shutdown) also restores DNS, but this defer
	// catches panics and unexpected early returns.
	dnsRestored := false
	defer func() {
		// Emergency browser policy restore on unexpected exit.
		p.browserPolicyMu.Lock()
		bpMgr := p.browserPolicyMgr
		p.browserPolicyMu.Unlock()
		if bpMgr != nil {
			restoreCtx := context.Background()
			if err := bpMgr.RestorePolicies(restoreCtx); err != nil {
				fmt.Fprintf(serviceStderr, "service: emergency browser policies restore: %v\n", err)
			}
		}
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
		// Flush DNS cache after Dnscache restart (Story 2.5 — emergency path).
		flushCtx, flushCancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer flushCancel()
		if err := dns.Flush(flushCtx); err != nil {
			fmt.Fprintf(serviceStderr, "service: emergency dns flush: %v\n", err)
		}
	}()

	// --- 4. Watchdog start ---
	wd := watchdog.NewWatchdog("127.0.0.1", dns.CheckCurrentResolver, dns.ForceResolver)
	p.watchdog = wd
	go wd.Start(ctx)

	// --- 5. Kill switch + Reconnector start ---
	// Story 6.1: setSTUNEnabled removed — the kill switch only gates DNS
	// proxy + HTTP proxy. The STUN leakcheck scheduler reads tunnel state
	// directly and skips when disconnected.
	ks := dns.NewKillSwitch(dnsMgr, func() {
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
		reconnOpts = append(reconnOpts, tunnel.WithFailoverFn(func(failoverCtx context.Context) error {
			previousCountry := p.CurrentCountryCode()
			result, err := p.failoverMgr.HandleFailover(failoverCtx)
			if err != nil {
				return err
			}
			// Story 6.2 M3 — drop the cached relay IP so the next leak
			// check resolves against the newly-selected relay's domain.
			// Without this, the STUN comparison would run for up to 5 min
			// against the OLD relay's IP and fire false LEAK_DETECTED.
			if p.leakRelayResolver != nil {
				p.leakRelayResolver.Invalidate()
			}
			// Only update currentCountry when the failover actually resolved a
			// country — the flat-list fallback path may leave it empty if the
			// new relay has no country hint, and overwriting with "" would
			// drop the UI's active-country indicator.
			if result.NewCountry != "" {
				p.SetCurrentCountry(result.NewCountry)
			}
			if result.CrossCountry {
				oldMeta := registry.CountryMetaMap[previousCountry]
				newMeta := registry.CountryMetaMap[result.NewCountry]
				oldName := oldMeta.Name
				if oldName == "" {
					oldName = previousCountry
				}
				newName := newMeta.Name
				if newName == "" {
					newName = result.NewCountry
				}
				p.setFailoverAlert(fmt.Sprintf("Tous les relais %s indisponibles, basculement vers %s", oldName, newName))
				fmt.Fprintf(serviceStderr, "service: failover: %s pool exhausted → %s (inter)\n", previousCountry, result.NewRelayID)
			} else {
				fmt.Fprintf(serviceStderr, "service: failover: → %s (intra)\n", result.NewRelayID)
			}
			return nil
		}))
	}
	reconnOpts = append(reconnOpts, tunnel.WithCircuitBreakerHook(func(_ context.Context) {
		p.tripCircuitBreaker(CircuitBreakerAlertMessage)
	}))
	// Story 5.9 — auto-restore the OS firewall after a successful reconnect
	// when degraded mode was active. No-op when already in normal mode.
	reconnOpts = append(reconnOpts, tunnel.WithReconnectSuccessHook(func(hookCtx context.Context) {
		p.MaybeRestoreKillSwitch(hookCtx, "auto-reconnect")
	}))
	reconnector := tunnel.NewReconnector(client.State().Updates(), client.Connect, ks, reconnOpts...)
	p.reconnector = reconnector
	go reconnector.Start(ctx)

	// --- 5b. Leak scheduler start ---
	// Story 6.1: STUN checks flow via net.DialUDP — the OS routes through
	// levoile0 (default route, Story 2.4) and the tunnel pump carries the
	// packet to the relay NAT. No /stun-relay endpoint, no applicative relay.
	//
	// Story 6.2: the checker compares each STUN response against the active
	// relay's public IP, resolved via DoH (never the system resolver — a
	// poisoned DNS could inject a matching "expected" IP and mask a real
	// leak). If DoH resolution fails the comparison is skipped (no false
	// leak_detected); the scheduler treats the error as transient.
	stunServers := resolveSTUNServers(p.config.STUNServers, p.config.STUNDefaultServer)

	var expectedIPFunc leakcheck.ExpectedIPFunc
	if relayDomain != "" {
		dohForLeakCheck, dohErr := registry.NewDoHResolver(
			registry.WithDoHLogger(func(format string, args ...any) {
				fmt.Fprintf(serviceStderr, "service: leakcheck doh: "+format+"\n", args...)
			}),
		)
		if dohErr != nil {
			fmt.Fprintf(serviceStderr, "service: leakcheck doh: invalid configuration, leak comparison disabled: %v\n", dohErr)
		} else {
			// Read the active relay domain dynamically so country switches
			// and inter-country failovers automatically retarget the DoH
			// lookup — a domain figé here would fire a faux LEAK_DETECTED
			// every tick for 10 min until the next restart (STUN sees the
			// new relay's IP, ExpectedIP stays on the old domain → mismatch
			// → RecoverFromAnomaly fires ≈600ms every cycle).
			relayResolver, resErr := leakcheck.NewRelayIPResolver(client.RelayDomain, dohForLeakCheck)
			if resErr != nil {
				fmt.Fprintf(serviceStderr, "service: leakcheck: relay resolver init failed, leak comparison disabled: %v\n", resErr)
			} else {
				p.leakRelayResolver = relayResolver
				expectedIPFunc = relayResolver.ExpectedIP
			}
		}
	}
	checker := leakcheck.NewWebRTCLeakChecker(expectedIPFunc).WithSTUNServers(stunServers)
	interval := p.config.STUNLeakcheckInterval
	if interval == 0 {
		interval = 10 * time.Minute
	}
	// Story 6.3 — a confirmed leak (FullLeakReport.Status == "leak_detected")
	// triggers the full anomaly recovery sequence. The scheduler itself
	// only invokes onLeak when the comparison is actually positive, never
	// on transient DoH/STUN errors (see leakcheck/scheduler.go:runCheck).
	//
	// M5 review fix: read the service ctx via p.Context() at callback
	// time rather than capturing the closure-scoped `ctx`. If a future
	// refactor makes the scheduler outlive the initial run, this keeps
	// us reading the live lifecycle context instead of a stale one.
	onLeak := func(_ *leakcheck.FullLeakReport) {
		rctx := p.Context()
		if rctx == nil {
			rctx = ctx
		}
		if err := p.RecoverFromAnomaly(rctx, anomaly.ReasonLeakDetected); err != nil {
			fmt.Fprintf(serviceStderr, "service: anomaly recovery (leak): %v\n", err)
		}
	}
	lkScheduler := leakcheck.NewPeriodicScheduler(
		interval,
		checker,
		client.State(),
		onLeak,
		func() { /* onRecovery: tray sees "pass" on next poll */ },
	)
	p.leakScheduler = lkScheduler
	p.bgWG.Add(1)
	go func() {
		defer p.bgWG.Done()
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
		blMgr := blocklist.NewManagerWithCacheAndClient(interval, p.config.BlocklistCachePath, p.blocklistHTTPClient())
		p.blocklistManager = blMgr
		p.blocklistActive.Store(true)
		p.bgWG.Add(1)
		go func() {
			defer p.bgWG.Done()
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
		// Detect package-managed binary so we can short-circuit the loop.
		// The installer was created earlier in tryInstallStagedUpdate().
		packageManaged := false
		if p.installer != nil && !p.config.UpdateAllowWhenPackaged {
			packageManaged = p.installer.IsPackageManaged()
		}
		upd, err := updater.NewUpdater(updater.UpdaterConfig{
			Owner:                p.config.UpdateOwner,
			Repo:                 p.config.UpdateRepo,
			PubKeyBase64:         p.updatePubKey(),
			StagingDir:           p.config.UpdateStagingDir,
			CheckInterval:        p.config.UpdateInterval,
			RateLimitBytesPerSec: p.config.UpdateRateLimit,
			PackageManaged:       packageManaged,
			// Story 8.1 AC11 — route updater events to syslog/Event Log via
			// the same writer kardianos/service binds for the rest of the
			// service. NFR22a: zero PII per emitted line.
			Logger: serviceStderr,
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
			p.bgWG.Add(1)
			go func() {
				defer p.bgWG.Done()
				if err := upd.Start(ctx); err != nil && !errors.Is(err, context.Canceled) && !errors.Is(err, context.DeadlineExceeded) {
					fmt.Fprintf(serviceStderr, "service: updater exit: %v\n", err)
				}
			}()
		}
	}

	// --- 8. UI watchdog (Story 5.7 — supervision levoile-ui) ---
	// Démarré APRÈS l'IPC pour que GetStatus puisse retourner UISupervision
	// dès la première requête. Arrêté EN TÊTE de shutdown() pour ne pas
	// respawner pendant le teardown tunnel/firewall.
	if p.config.UIWatchdogEnabled {
		binaryPath := p.config.UIBinaryPath
		if binaryPath == "" {
			binaryPath = deriveUIBinaryPath()
		}
		if binaryPath != "" {
			if _, statErr := os.Stat(binaryPath); statErr != nil {
				// NFR22a — log filename only, not the full path (which could
				// expose install location or user profile on some layouts).
				fmt.Fprintf(serviceStderr, "service: ui watchdog: binary %s not found, supervision disabled: %v\n", filepath.Base(binaryPath), statErr)
			} else {
				wd, wdErr := uiwatchdog.New(uiwatchdog.Config{
					Launcher: uiwatchdog.NewPlatformLauncher(binaryPath),
					Logger:   &uiWatchdogLogger{},
				})
				if wdErr != nil {
					fmt.Fprintf(serviceStderr, "service: ui watchdog: init failed: %v\n", wdErr)
				} else {
					p.uiWatchdog = wd
					p.bgWG.Add(1)
					go func() {
						defer p.bgWG.Done()
						if err := wd.Start(ctx); err != nil {
							fmt.Fprintf(serviceStderr, "service: ui watchdog: %v\n", err)
						}
					}()
				}
			}
		}
	}

	// --- Wait for shutdown ---
	// À partir d'ici, shutdown() prend la responsabilité de fermer tunDev.
	// Le désarmement de tunCleanup évite un double-close si shutdown se
	// déclenche alors que run() est encore dans la séquence de setup.
	tunCleanup = func() {}
	<-ctx.Done()

	// --- Shutdown sequence (reverse order) ---
	p.shutdown()
	dnsRestored = true

	// Return (do NOT os.Exit). The deferred close(p.done) fires, which is
	// what Program.Stop is blocked on; letting it return lets kardianos's
	// Execute loop report SERVICE_STOPPED to SCM. The previous os.Exit(0)
	// skipped the defer, SCM never saw a proper stop status, and with
	// OnFailure=restart the service was relaunched ~5 s later — re-arming
	// WFP behind a vanished UI. RequestStop now funnels IPC quits through
	// svc.Stop so this path is always driven by an SCM-initiated stop.
}

// shutdown performs the reverse-order cleanup.
//
// CRITICAL (AC4, AC8): IPC server is stopped LAST so the tray can receive
// confirmation that shutdown is complete. Browser policies and DNS are
// restored BEFORE IPC close.
func (p *Program) shutdown() {
	// 0. Stop UI watchdog FIRST (Story 5.7) — must happen before tunnel/
	// firewall teardown so the watchdog does not respawn levoile-ui.exe
	// during the shutdown window.
	if p.uiWatchdog != nil {
		p.uiWatchdog.Stop()
		p.uiWatchdog = nil
	}

	// 0a. Clear captive portal state (Story 2.8).
	p.captivePortal.Store(false)
	p.captiveProbeURL.Store("")
	p.captiveMu.Lock()
	if p.captiveCancel != nil {
		p.captiveCancel()
		p.captiveCancel = nil
	}
	p.captiveMu.Unlock()

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

	// 2b. Stop HTTP proxy (5s drain, before kill switch deactivate)
	p.stopHTTPProxy()

	// 3. Deactivate kill switch if active
	if p.killSwitch != nil && p.killSwitch.IsActive() {
		restoreCtx, restoreCancel := context.WithTimeout(context.Background(), shutdownStepTimeout)
		if err := p.killSwitch.Deactivate(restoreCtx); err != nil {
			fmt.Fprintf(serviceStderr, "service: kill switch deactivate: %v\n", err)
		}
		restoreCancel()
	}

	// 4. Restore browser policies (service owns this — AC5)
	p.browserPolicyMu.Lock()
	bpMgr := p.browserPolicyMgr
	p.browserPolicyMgr = nil
	p.browserPolicyMu.Unlock()
	if bpMgr != nil {
		restoreCtx, restoreCancel := context.WithTimeout(context.Background(), shutdownStepTimeout)
		if err := bpMgr.RestorePolicies(restoreCtx); err != nil {
			fmt.Fprintf(serviceStderr, "service: browser policies restore: %v\n", err)
		}
		restoreCancel()
	}

	// 5. Restore DNS resolver (service owns this — AC5)
	if p.dnsManager != nil {
		restoreCtx, restoreCancel := context.WithTimeout(context.Background(), shutdownStepTimeout)
		if err := p.dnsManager.RestoreResolver(restoreCtx); err != nil {
			fmt.Fprintf(serviceStderr, "service: dns restore resolver: %v\n", err)
		}
		restoreCancel()
	}

	// 6. Verify DNS restoration via watchdog
	if p.dnsManager != nil && p.watchdog != nil {
		originalDNS := p.dnsManager.OriginalResolver()
		if originalDNS != "" {
			restoreCtx, restoreCancel := context.WithTimeout(context.Background(), shutdownStepTimeout)
			if err := p.watchdog.VerifyAndRestore(restoreCtx, originalDNS); err != nil {
				fmt.Fprintf(serviceStderr, "service: watchdog verify: %v\n", err)
			}
			restoreCancel()
		}
	}

	// 7. Stop DNS proxy
	p.stopProxy()

	// 7a. Restart Windows Dnscache service (was stopped to free port 53).
	if err := dns.RestartDnscache(); err != nil {
		fmt.Fprintf(serviceStderr, "service: %v\n", err)
	}

	// 7b. Stop TUN watchdog BEFORE tunnel disconnect (Story 2.2 — évite
	// de triggerer un faux recovery pendant le shutdown propre).
	if p.tunWatchdog != nil {
		p.tunWatchdog.Stop()
		p.tunWatchdog = nil
	}

	// 8. Disconnect tunnel, THEN close the state channel. Order matters:
	// Disconnect internally calls state.Set(StateDisconnected) which sends
	// on the updates channel. Closing the channel first would panic with
	// "send on closed channel" the moment Disconnect fired, aborting the
	// rest of shutdown() and leaving WFP/routing/TUN in place. StateManager
	// is also defensive (Set is a no-op after Close) but the explicit
	// order keeps intent obvious and leaves the final StateDisconnected
	// observable by any consumer still ranging over Updates().
	if p.tunnelClient != nil {
		if err := p.tunnelClient.Disconnect(); err != nil {
			fmt.Fprintf(serviceStderr, "service: disconnect: %v\n", err)
		}
		p.tunnelClient.State().Close()
	}

	// 8a. Deactivate firewall kill switch (après tunnel disconnect, avant routing teardown).
	// Ordre strict : tunnel → firewall → routing → tun.Close.
	if p.firewallMgr != nil {
		fwCtx, fwCancel := context.WithTimeout(context.Background(), shutdownStepTimeout)
		if err := p.firewallMgr.Deactivate(fwCtx); err != nil {
			fmt.Fprintf(serviceStderr, "service: firewall deactivate: %v\n", err)
		}
		fwCancel()
		p.firewallMgr = nil
	}

	// 8b. Teardown routing (après firewall, avant tun.Close).
	if p.routeMgr != nil {
		if err := p.routeMgr.Teardown(); err != nil {
			fmt.Fprintf(serviceStderr, "service: routing teardown: %v\n", err)
		}
		p.routeMgr = nil
	}

	// 8b. Close TUN/Wintun interface LAST (Epic 2 — architecture ordre strict
	// de shutdown : tunnel → firewall → routing → tun.Close).
	if p.tunDev != nil {
		if err := p.tunDev.Close(); err != nil {
			fmt.Fprintf(serviceStderr, "service: tun close: %v\n", err)
		}
		p.tunDev = nil
	}

	// 8c. Flush DNS cache (Story 2.5 — après tun.Close, best-effort).
	// Ordre : RestartDnscache (étape 7a) → Flush cache résiduel.
	{
		flushCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := dns.Flush(flushCtx); err != nil {
			fmt.Fprintf(serviceStderr, "service: dns flush: %v\n", err)
		}
	}

	// 9. Stop IPC server LAST — tray waits for shutdown confirmation (AC8)
	p.mu.Lock()
	ipcStop := p.ipcStop
	p.mu.Unlock()
	if ipcStop != nil {
		ipcStop()
	}

	// 10. Wait for launcher goroutines (updater loop, watchdog launcher
	// wrappers, ipcStart wrapper) to exit. Their ctx has already been
	// cancelled via p.cancel() in Stop(); this is just a best-effort
	// join so run()'s return (and the process exit that follows) does
	// not race them mid-log. Bounded so a truly wedged goroutine cannot
	// hold the SCM shutdown hostage.
	bgDone := make(chan struct{})
	go func() {
		p.bgWG.Wait()
		close(bgDone)
	}()
	select {
	case <-bgDone:
	case <-time.After(bgWaitTimeout):
		fmt.Fprintf(serviceStderr, "service: background goroutines still running after %s — exiting anyway\n", bgWaitTimeout)
	}
}

// updatePubKey returns the Ed25519 public key for update verification,
// falling back to RelayPubKey if no dedicated key is configured.
func (p *Program) updatePubKey() string {
	if p.config.UpdatePubKey != "" {
		return p.config.UpdatePubKey
	}
	// Canonical fallback: the release key baked into the binary at build time
	// (rotated every 24 months per NFR22h). This is the key the release
	// pipeline signs artifacts with — verifying with RelayPubKey (registry
	// master) was incorrect and would reject every legitimate signed release
	// because the two keys are distinct by design (see internal/crypto/
	// release_keys.go and reference_relay_servers memory).
	if k := lecrypto.ReleasePublicKeyCurrentBase64; k != "" {
		return k
	}
	// Last resort: RelayPubKey. Kept for backward-compat with test fixtures
	// that wire the relay key directly; production releases will never reach
	// this branch because release_keys.go is compile-time non-empty.
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

	// Skip install when the binary is package-managed (deb/rpm/pacman) unless
	// explicitly overridden. The system package manager is authoritative for
	// version upgrades — writing a new binary on top would conflict on the
	// next `apt/dnf/pacman upgrade` cycle.
	if inst.IsPackageManaged() && !p.config.UpdateAllowWhenPackaged {
		// NFR22a: log only the binary's parent directory (system path like
		// /usr/bin), never the full resolved path which could expose a
		// non-default install layout.
		fmt.Fprintf(serviceStderr,
			"updater: install: skipped — binary is package-managed (%s), use system package manager to upgrade\n",
			filepath.Dir(inst.ExecutablePath()))
		// Remove the staged payload so we don't repeat this log on every boot.
		_ = os.Remove(staged.BinaryPath)
		_ = os.Remove(staged.ChecksumPath)
		_ = os.Remove(staged.SignaturePath)
		if staged.VersionFile != "" {
			_ = os.Remove(staged.VersionFile)
		}
		return
	}

	// Check install-retry counter: abandon the staged payload if previous
	// attempts have already failed past the configured cap. This prevents an
	// infinite retry loop when the staged binary is structurally broken
	// (e.g. incompatible CPU arch, missing dependency, disk-full at target).
	// Values ≤ 0 fall back to the 3-retry default — no "unlimited" mode
	// by design (matches config.UpdateConfig.MaxInstallRetries contract).
	maxRetries := p.config.UpdateMaxInstallRetries
	if maxRetries <= 0 {
		maxRetries = 3
	}
	retries, retriesErr := updater.ReadInstallRetries(p.config.UpdateStagingDir)
	if retriesErr != nil {
		fmt.Fprintf(serviceStderr, "service: read install retries: %v\n", retriesErr)
	}
	if retries >= maxRetries {
		fmt.Fprintf(serviceStderr,
			"updater: install: abandoned after %d retries, staged payload cleared (will re-download on next check)\n",
			retries)
		_ = os.Remove(staged.BinaryPath)
		_ = os.Remove(staged.ChecksumPath)
		_ = os.Remove(staged.SignaturePath)
		if staged.VersionFile != "" {
			_ = os.Remove(staged.VersionFile)
		}
		_ = updater.ClearInstallRetries(p.config.UpdateStagingDir)
		return
	}

	if err := inst.Install(ctx, staged); err != nil {
		fmt.Fprintf(serviceStderr, "service: install update: %v\n", err)
		p.updateMu.Lock()
		p.lastInstallError = err.Error()
		p.updateMu.Unlock()
		// Bump the retry counter so repeated boots don't loop forever.
		if werr := updater.WriteInstallRetries(p.config.UpdateStagingDir, retries+1); werr != nil {
			fmt.Fprintf(serviceStderr, "service: write install retries: %v\n", werr)
		}
		return
	}

	// Install succeeded — clear any prior retry counter for this staged payload.
	if err := updater.ClearInstallRetries(p.config.UpdateStagingDir); err != nil {
		fmt.Fprintf(serviceStderr, "service: clear install retries: %v\n", err)
	}

	// Mark that a rollback is possible until tunnel confirms working
	if err := updater.WriteRollbackState(p.config.UpdateStagingDir, &updater.RollbackState{
		JustInstalled:    true,
		InstalledVersion: staged.Version,
	}); err != nil {
		fmt.Fprintf(serviceStderr, "service: write rollback state: %v\n", err)
	}

	// Sync the in-memory version with the binary we just put on disk. The
	// running image was loaded from the *previous* binary, so without this
	// assignment CurrentVersion() keeps reporting the pre-swap version for
	// the entire life of this process — which makes the updater cycle see
	// every subsequent CheckLatest of the just-installed release as "newer"
	// and re-download it in a loop until the next full restart. The fix is
	// scoped to this package var only; no persistent state depends on it.
	updater.Version = staged.Version

	fmt.Fprintf(serviceStderr, "updater: installed v%s\n", staged.Version)
	p.updateMu.Lock()
	p.installedVersion = staged.Version
	p.updateMu.Unlock()
}

// tryRollbackIfNeeded checks if a rollback should be performed after a tunnel failure.
// Returns true if rollback was performed successfully, false otherwise.
// shouldRollbackOnConnectErr reports whether a Connect error is a strong
// signal that the *new* binary is at fault (bad crypto, corrupted signature
// path, cert pinning mismatch) rather than a transient network problem. Only
// the former should trigger a rollback — the latter is routinely caused by
// a slow uplink, a DNS flake, or a relay restart during the first tunnel
// handshake on a freshly installed version.
//
// Classification:
//   - ErrVerificationFailed / ErrPinningFailed → rollback (binary vs crypto)
//   - context.DeadlineExceeded / Canceled     → transient (don't rollback)
//   - net.Error (Timeout/Temporary) or DNS    → transient (don't rollback)
//   - anything else                           → rollback (unknown,
//     original behaviour preserved so genuine "new binary segfaults in
//     Connect" paths still escalate)
func shouldRollbackOnConnectErr(err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, tunnel.ErrVerificationFailed) || errors.Is(err, tunnel.ErrPinningFailed) {
		return true
	}
	if errors.Is(err, context.DeadlineExceeded) || errors.Is(err, context.Canceled) {
		return false
	}
	var netErr net.Error
	if errors.As(err, &netErr) {
		if netErr.Timeout() {
			return false
		}
	}
	var dnsErr *net.DNSError
	if errors.As(err, &dnsErr) {
		return false
	}
	return true
}

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
//
// Platform mapping (provided by kardianos/service.Service.Restart):
//   - Linux (systemd): shells out to `systemctl restart <name>.service`
//   - Linux (SysV / OpenRC): shells out to `service <name> restart` / `rc-service <name> restart`
//   - Windows: SCM StopService + StartService via advapi32
//
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

	srv := httpproxy.NewServer(listenAddr, p.tunnelClient, httpproxy.NewVolumeTracker(httpproxy.VolumeThreshold))
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

// startTUNWatchdog crée et démarre le watchdog TUN (Story 2.2 — NFR16).
// Surveille levoile0 toutes les 3s. Sur disparition/altération, exécute
// recoverTUN (sequence : recreate TUN → routing → firewall → tunnel reconnect).
func (p *Program) startTUNWatchdog(ctx context.Context) {
	chk, err := tunwatchdog.NewNetChecker(tunwatchdog.CheckerConfig{
		Name:        p.tunDev.Name(),
		ExpectedMTU: p.tunDev.MTU(),
	})
	if err != nil {
		fmt.Fprintf(serviceStderr, "service: tun watchdog checker: %v\n", err)
		return
	}
	wd, err := tunwatchdog.NewWatchdog(tunwatchdog.Config{
		Checker: chk,
		// Story 6.3 — watchdog calls RecoverFromAnomaly instead of
		// recoverTUN directly so the sequence is wrapped with logging
		// (Event Log / journald) + UI notification + serialization
		// against a concurrent leakcheck trigger. RecoverFromAnomaly
		// delegates to recoverTUN internally — functional behaviour is
		// preserved.
		OnLost: func(ctx context.Context) error {
			return p.RecoverFromAnomaly(ctx, anomaly.ReasonTUNAltered)
		},
		Logger: &serviceLogger{},
	})
	if err != nil {
		fmt.Fprintf(serviceStderr, "service: tun watchdog: %v\n", err)
		return
	}
	p.tunWatchdog = wd
	p.bgWG.Add(1)
	go func() {
		defer p.bgWG.Done()
		if err := wd.Start(ctx); err != nil {
			fmt.Fprintf(serviceStderr, "service: tun watchdog exit: %v\n", err)
		}
	}()
}

// pumpRetryBackoff bounds how often runTUNPump retries opening POST /tunnel
// after a stream error or when the tunnel is not yet (re)connected. Tight
// enough to recover quickly when reconnect succeeds, slow enough to avoid
// spinning on persistent failure (e.g. relay 5xx or session token rejected).
const pumpRetryBackoff = 1 * time.Second

// pumpOutboundQueueSize bounds the channel that buffers packets from the TUN
// reader to the pump. Small enough that backpressure propagates to dev.Read
// (the kernel sees congestion, TCP backs off) before unbounded memory growth
// can occur on a stalled relay.
const pumpOutboundQueueSize = 64

// startTUNReader launches a long-lived goroutine that reads IP packets from
// the TUN device and feeds them into p.tunOutbound. The channel is the
// bridge between the device-bound goroutine (which lives across pump
// reconnects) and the per-stream pump goroutine. Closes the channel on dev
// EOF / error so subsequent RunPump calls return cleanly.
//
// Lifetime: started once per TUN device (after ensureTUN). Recreated by
// recoverTUN when the device is rebuilt. Exits when dev.Read errors —
// typically because dev.Close was called during shutdown or recovery.
func (p *Program) startTUNReader(ctx context.Context) {
	if p.tunDev == nil {
		return
	}
	dev := p.tunDev
	out := make(chan []byte, pumpOutboundQueueSize)
	p.tunOutbound = out
	mtu := dev.MTU()
	if mtu == 0 {
		mtu = 1420
	}
	go func() {
		defer close(out)
		buf := make([]byte, mtu)
		// Light periodic telemetry (60s) — only printed when counters
		// change since last tick, so an idle TUN never generates noise.
		// Goes to serviceStderr (Event Log on Windows when service is run
		// under SCM with ERR redirection; foreground stderr otherwise).
		var nRead, nFwd, nDrop uint64
		statsTick := time.NewTicker(60 * time.Second)
		defer statsTick.Stop()
		go func() {
			var lastRead, lastFwd, lastDrop uint64
			for {
				select {
				case <-ctx.Done():
					return
				case <-statsTick.C:
					if nRead == lastRead && nFwd == lastFwd && nDrop == lastDrop {
						continue
					}
					fmt.Fprintf(serviceStderr, "service: tun reader stats: read=%d forwarded=%d dropped=%d\n", nRead, nFwd, nDrop)
					lastRead, lastFwd, lastDrop = nRead, nFwd, nDrop
				}
			}
		}()
		for {
			n, err := dev.Read(buf)
			if err != nil {
				return
			}
			if n == 0 {
				continue
			}
			nRead++
			// Drop packets the relay's NAT can't process (parseIPv4 +
			// SSRF-block in internal/relay/nat_packet.go +
			// internal/relay/connect_handler.go::isBlockedIP). Forwarding
			// them anyway makes the relay close the stream on the FIRST
			// rejected packet (tunnel_handler.go:217 returns on
			// forwarder error), which kicks off an infinite "open → first
			// junk packet → reject → close" reconnect loop. Background
			// chatter on a freshly opened TUN routinely contains IPv6
			// link-local, mDNS multicast, ARP-like and DHCP renewals that
			// all qualify as junk from the relay's perspective.
			if !forwardablePacket(buf[:n]) {
				nDrop++
				continue
			}
			cp := make([]byte, n)
			copy(cp, buf[:n])
			select {
			case out <- cp:
				nFwd++
			case <-ctx.Done():
				return
			}
		}
	}()
}

// forwardablePacket returns true only for IPv4 TCP/UDP packets bound to a
// public destination IP — the subset the relay's NAT can actually translate.
// Mirrors the relay-side checks (parseIPv4 + isBlockedIP). Dropping at the
// source saves bandwidth and avoids the relay's hard "drop stream on first
// reject" behaviour.
func forwardablePacket(pkt []byte) bool {
	if len(pkt) < 20 {
		return false
	}
	if pkt[0]>>4 != 4 {
		return false // IPv6, ARP-likes, garbage
	}
	proto := pkt[9]
	if proto != 6 && proto != 17 { // TCP, UDP
		return false
	}
	dst := net.IPv4(pkt[16], pkt[17], pkt[18], pkt[19])
	if dst.IsLoopback() || dst.IsPrivate() ||
		dst.IsLinkLocalUnicast() || dst.IsLinkLocalMulticast() ||
		dst.IsMulticast() || dst.IsUnspecified() ||
		dst.Equal(net.IPv4bcast) {
		return false
	}
	return true
}

// runTUNPump is the long-lived L3 pump goroutine. It bridges levoile0 ↔ relay
// /tunnel by opening a POST stream and shuttling IP packets in both
// directions. Each iteration of the loop:
//
//  1. Bail out if ctx is cancelled.
//  2. Wait for tunnel.StateConnected (the reconnector owns Connect calls;
//     we never trigger reconnects from here, just observe state).
//  3. Refresh session token if needed.
//  4. Call tunnel.Client.RunPump which blocks until the stream errors,
//     EOFs, or ctx is cancelled.
//  5. Sleep pumpRetryBackoff and loop.
//
// Without this goroutine, the kernel routes all traffic into levoile0 (per
// routing.Setup's /0 default) but the Wintun ring buffer fills and drops
// packets — every non-proxy app loses connectivity even though the WFP
// killswitch correctly allows the TUN interface.
func (p *Program) runTUNPump(ctx context.Context) {
	if p.tunDev == nil || p.tunnelClient == nil || p.tunOutbound == nil {
		return
	}
	for {
		if ctx.Err() != nil {
			return
		}
		if p.tunnelClient.State().Get() != tunnel.StateConnected {
			select {
			case <-ctx.Done():
				return
			case <-time.After(pumpRetryBackoff):
			}
			continue
		}
		if err := p.tunnelClient.EnsureSessionToken(ctx); err != nil {
			fmt.Fprintf(serviceStderr, "service: tun pump: token refresh: %v\n", err)
			select {
			case <-ctx.Done():
				return
			case <-time.After(pumpRetryBackoff):
			}
			continue
		}
		// Snapshot dev under lock — recoverTUN may swap p.tunDev.
		p.mu.Lock()
		dev := p.tunDev
		p.mu.Unlock()
		if dev == nil {
			select {
			case <-ctx.Done():
				return
			case <-time.After(pumpRetryBackoff):
			}
			continue
		}
		if err := p.tunnelClient.RunPump(ctx, p.tunOutbound, dev.Write); err != nil && ctx.Err() == nil {
			fmt.Fprintf(serviceStderr, "service: tun pump: %v\n", err)
		}
		if ctx.Err() != nil {
			return
		}
		select {
		case <-ctx.Done():
			return
		case <-time.After(pumpRetryBackoff):
		}
	}
}

// recoverTUN est le callback du watchdog TUN (Story 2.2 — AC2/AC3).
// Séquence stricte : recreate TUN → routing.Setup → firewall.Activate →
// tunnel.Connect. Le firewall n'est PAS désactivé durant la procédure (AC3).
func (p *Program) recoverTUN(ctx context.Context) error {
	// Vérifier que le service n'est pas en cours de shutdown (fix H1 — si
	// le service ctx est annulé, ne rien faire).
	if ctx.Err() != nil {
		return ctx.Err()
	}

	fmt.Fprintf(serviceStderr, "service: tun recovery: starting sequence\n")

	// Sérialiser l'accès aux champs partagés avec shutdown() (fix H1).
	p.mu.Lock()

	// 1. Recreate TUN interface.
	tunName := tun.DefaultName
	tunMTU := tun.DefaultMTU
	if p.tunDev != nil {
		tunName = p.tunDev.Name()
		tunMTU = p.tunDev.MTU()
	}
	// Close old device best-effort (may already be gone).
	if p.tunDev != nil {
		_ = p.tunDev.Close()
		p.tunDev = nil
	}
	p.mu.Unlock()

	dev, err := tunFactory(tunName, tunMTU)
	if err != nil {
		return fmt.Errorf("tun recovery: tun.New: %w", err)
	}

	p.mu.Lock()
	// Re-check shutdown après l'appel bloquant tun.New.
	if ctx.Err() != nil {
		p.mu.Unlock()
		_ = dev.Close()
		return ctx.Err()
	}
	p.tunDev = dev
	p.mu.Unlock()
	fmt.Fprintf(serviceStderr, "service: tun recovery: interface %s recreated\n", dev.Name())

	// Restart the long-lived TUN reader on the new device. The previous
	// reader exited when the old dev was closed at line 2374 (dev.Read
	// returns an error after Close). Without this, runTUNPump would block
	// forever waiting on a stale, never-fed channel.
	p.startTUNReader(ctx)

	// 2. Firewall re-activate (SANS Deactivate préalable — AC3).
	// Réutilise l'instance existante si possible (évite fuite goroutine
	// AlteredCh watcher — fix H2). Activate est idempotent (flush+replace
	// atomique côté nftables/WFP).
	//
	// Fix C5 (audit sécurité 2026-04) : ce bloc est exécuté AVANT le routing
	// setup. Raison : si on installe les routes vers le nouveau TUN d'abord,
	// une fenêtre ~ms existe où le kernel route des paquets vers un TUN
	// que le firewall n'a pas encore blessé. En activant le firewall d'abord
	// (flush+replace atomique), les paquets qui commencent à être routés
	// ensuite sont toujours gouvernés par des règles à jour pour le nouveau
	// nom d'interface.
	relayIP := p.resolvedRelayIP()
	if p.config.FirewallEnabled && relayIP != nil {
		p.mu.Lock()
		fw := p.firewallMgr
		p.mu.Unlock()
		if fw == nil {
			fwLog := &serviceLogger{}
			fw = firewallFactory(fwLog, firewall.Options{AllowIPv6Leak: p.config.AllowIPv6Leak})
		}
		if err := fw.Activate(ctx, firewall.ActivateParams{
			Mode:    firewall.ModeFull,
			RelayIP: relayIP,
			TunName: dev.Name(),
		}); err != nil {
			fmt.Fprintf(serviceStderr, "service: tun recovery: firewall activate: %v\n", err)
		} else {
			p.mu.Lock()
			p.firewallMgr = fw
			p.mu.Unlock()
			fmt.Fprintf(serviceStderr, "service: tun recovery: firewall restored\n")
		}
	}

	// 3. Routing re-setup.
	p.mu.Lock()
	if p.routeMgr != nil {
		_ = p.routeMgr.Teardown() // idempotent
		p.routeMgr = nil
	}
	p.mu.Unlock()

	if relayIP != nil {
		rm := routingFactory()
		_ = rm.Cleanup()
		origGW, origIface, err := captureOriginalRouteFunc()
		if err != nil {
			fmt.Fprintf(serviceStderr, "service: tun recovery: routing gateway: %v\n", err)
		} else if err := rm.Setup(dev.Name(), relayIP, origGW, origIface); err != nil {
			fmt.Fprintf(serviceStderr, "service: tun recovery: routing setup: %v\n", err)
		} else {
			p.mu.Lock()
			p.routeMgr = rm
			p.mu.Unlock()
			fmt.Fprintf(serviceStderr, "service: tun recovery: routing restored\n")
		}
	}

	// 4. Tunnel reconnect.
	if p.tunnelClient != nil {
		p.tunnelClient.ResetTransport()
		if err := p.tunnelClient.Connect(ctx); err != nil {
			return fmt.Errorf("tun recovery: tunnel.Connect: %w", err)
		}
		fmt.Fprintf(serviceStderr, "service: tun recovery: tunnel reconnected\n")
	}

	fmt.Fprintf(serviceStderr, "service: tun recovery: sequence complete\n")
	return nil
}

// watchFirewallAltered listens on the firewall's AlteredCh and reacts by
// logging a warning, surfacing an IPC alert, and re-activating the firewall.
// Runs until ctx is cancelled (service shutdown).
func (p *Program) watchFirewallAltered(ctx context.Context, ch <-chan struct{}) {
	for {
		select {
		case <-ctx.Done():
			return
		case <-ch:
			fmt.Fprintf(serviceStderr, "service: firewall rules altered by third party — restoring\n")
			p.firewallAltered.Store(true)
			// Re-activate: Deactivate (cleanup tampered state) → Activate (restore).
			p.mu.Lock()
			fw := p.firewallMgr
			tunName := ""
			if p.tunDev != nil {
				tunName = p.tunDev.Name()
			}
			p.mu.Unlock()
			if fw == nil {
				continue
			}
			if err := fw.Deactivate(ctx); err != nil {
				fmt.Fprintf(serviceStderr, "service: firewall deactivate (restore): %v\n", err)
			}
			relayIP := p.resolvedRelayIP()
			if relayIP != nil && tunName != "" {
				if err := fw.Activate(ctx, firewall.ActivateParams{Mode: firewall.ModeFull, RelayIP: relayIP, TunName: tunName}); err != nil {
					fmt.Fprintf(serviceStderr, "service: firewall re-activate: %v\n", err)
				} else {
					fmt.Fprintf(serviceStderr, "service: firewall rules restored\n")
					p.firewallAltered.Store(false)
				}
			}
		}
	}
}

// FirewallAltered reports whether the firewall watchdog has detected third-party
// rule tampering. Surfaced to the UI via IPC GetStatus.
func (p *Program) FirewallAltered() bool {
	return p.firewallAltered.Load()
}

// AllowIPv6Leak returns the current IPv6 leak policy setting.
func (p *Program) AllowIPv6Leak() bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.config.AllowIPv6Leak
}

// ErrSecurityPolicyLocked is returned by runtime setters for security-relevant
// policies (IPv6 leak, kill-switch mode, …) when the operator has locked
// those policies to config-file-only via LEVOILE_LOCK_SECURITY_POLICY=1 or
// the equivalent config flag. Recovery from a lock is out-of-band: edit
// config.toml and restart the service.
var ErrSecurityPolicyLocked = errors.New("service: security policy locked — edit config.toml and restart")

// securityPolicyLocked reports whether runtime toggles of security-relevant
// policies are forbidden. Read once per call so operators can unlock by
// restarting the service after unsetting the env var.
func securityPolicyLocked() bool {
	return os.Getenv("LEVOILE_LOCK_SECURITY_POLICY") == "1"
}

// SetAllowIPv6Leak updates the IPv6 leak policy at runtime.
// It updates the firewall rules and the in-memory config atomically.
// Returns ErrSecurityPolicyLocked when LEVOILE_LOCK_SECURITY_POLICY=1 is set,
// so operators can pin the policy to config.toml and forbid UI/CTL overrides.
// Every invocation is audit-logged on stderr even when allowed (fix H6).
func (p *Program) SetAllowIPv6Leak(allow bool) error {
	// Audit trail: every IPv6 policy change is stderr-logged so a SIEM or
	// journald consumer can catch unexpected toggles. The log line precedes
	// the lock check on purpose — attempted overrides under a lock are also
	// events worth reviewing.
	fmt.Fprintf(serviceStderr, "SECURITY AUDIT: SetAllowIPv6Leak allow=%v locked=%v\n", allow, securityPolicyLocked())

	if securityPolicyLocked() {
		return ErrSecurityPolicyLocked
	}

	p.mu.Lock()
	fw := p.firewallMgr
	// Hold lock across the entire operation to prevent concurrent reads
	// of p.config.AllowIPv6Leak from seeing inconsistent state.
	defer p.mu.Unlock()
	if fw != nil {
		if err := fw.SetIPv6Policy(context.Background(), allow); err != nil {
			return err
		}
	}
	p.config.AllowIPv6Leak = allow
	return nil
}

// CaptivePortal reports whether the service is in captive portal mode
// (firewall lockdown relaxed, waiting for portal auth). Story 2.8.
func (p *Program) CaptivePortal() bool {
	return p.captivePortal.Load()
}

// CaptiveProbeURL returns the URL that triggered captive detection, or "".
func (p *Program) CaptiveProbeURL() string {
	v := p.captiveProbeURL.Load()
	if v == nil {
		return ""
	}
	s, _ := v.(string)
	return s
}

// RetryCaptiveCheck triggers an immediate captive portal re-probe.
// Called from IPC handler when the user clicks "Activer la protection".
func (p *Program) RetryCaptiveCheck() {
	p.captiveMu.Lock()
	cancelFn := p.captiveCancel
	p.captiveMu.Unlock()
	if cancelFn != nil {
		// Cancelling the captive watcher triggers an immediate re-probe
		// cycle in the orchestrator, which will either clear captive or
		// restart the watcher.
		cancelFn()
	}
}

// resolvedRelayIP returns the relay IPv4 stored during startup for firewall
// re-activation. Returns nil if not set.
func (p *Program) resolvedRelayIP() net.IP {
	v := p.firewallRelayIP.Load()
	if v == nil {
		return nil
	}
	ip, _ := v.(net.IP)
	return ip
}

// ReconfigureForRelay re-applies routing (/32 to relay via original gateway)
// and firewall (kill-switch allow rule for relay IP:443/UDP) for the given
// new relay IP. Used by handleSelectCountry when the user picks a different
// country: without this, WFP keeps allowing only the OLD relay IP and
// every Connect attempt to the new relay times out (5s × 5 retries → 12s
// → circuit breaker → killswitch stays active and locks the user out of
// the internet). Idempotent: routing.Setup and firewall.Activate are both
// flush+replace under the hood.
//
// Order matters — firewall first, then routing — so a routing change never
// admits a packet through a stale-allow filter pointing at the previous
// relay (matches the C5 ordering rationale in recoverTUN).
//
// Safe to call concurrently with the tunnel client; does not touch
// tunDev or the QUIC connection.
func (p *Program) ReconfigureForRelay(ctx context.Context, newRelayIP net.IP) error {
	if newRelayIP == nil {
		return fmt.Errorf("service: reconfigure: nil relay IP")
	}
	ip4 := newRelayIP.To4()
	if ip4 == nil {
		return fmt.Errorf("service: reconfigure: relay IP %s is not IPv4", newRelayIP)
	}

	// Update the cached IP first so any concurrent recoverTUN or watchdog
	// trigger reads the new value.
	p.firewallRelayIP.Store(ip4)

	// 1. Firewall re-activate with new RelayIP (idempotent: flush+replace).
	if p.config.FirewallEnabled {
		p.mu.Lock()
		fw := p.firewallMgr
		tunDev := p.tunDev
		p.mu.Unlock()
		if fw != nil && tunDev != nil {
			params := firewall.ActivateParams{
				Mode:    firewall.ModeFull,
				RelayIP: ip4,
				TunName: tunDev.Name(),
			}
			if err := fw.Activate(ctx, params); err != nil {
				return fmt.Errorf("service: reconfigure: firewall activate: %w", err)
			}
		}
	}

	// 2. Routing teardown of the old /32 + setup with the new one.
	p.mu.Lock()
	oldRouteMgr := p.routeMgr
	p.routeMgr = nil
	p.mu.Unlock()
	if oldRouteMgr != nil {
		if err := oldRouteMgr.Teardown(); err != nil {
			fmt.Fprintf(serviceStderr, "service: reconfigure: old routing teardown: %v\n", err)
			// Non-fatal — Setup below will overwrite.
		}
	}
	p.mu.Lock()
	tunDev := p.tunDev
	p.mu.Unlock()
	if tunDev == nil {
		return nil // routing only meaningful when TUN is up
	}
	rm := routingFactory()
	if err := rm.Cleanup(); err != nil {
		fmt.Fprintf(serviceStderr, "service: reconfigure: routing cleanup: %v\n", err)
	}
	origGW, origIface, err := captureOriginalRouteFunc()
	if err != nil {
		return fmt.Errorf("service: reconfigure: capture original route: %w", err)
	}
	if err := rm.Setup(tunDev.Name(), ip4, origGW, origIface); err != nil {
		return fmt.Errorf("service: reconfigure: routing setup: %w", err)
	}
	p.mu.Lock()
	p.routeMgr = rm
	p.mu.Unlock()

	return nil
}

// waitForCaptiveClear blocks until the captive portal is cleared. It runs a
// periodic re-probe (15s) and also listens for a manual retry (RetryCaptiveCheck
// cancels captiveCancel). When the portal is cleared, it deactivates the captive
// firewall and resets the captive state so the normal Connect flow can proceed.
func (p *Program) waitForCaptiveClear(ctx context.Context, lanGW net.IP) {
	const reprobeInterval = 15 * time.Second

	for {
		// Create a child context that RetryCaptiveCheck can cancel.
		watchCtx, watchCancel := context.WithCancel(ctx)
		p.captiveMu.Lock()
		p.captiveCancel = watchCancel
		p.captiveMu.Unlock()

		// Wait for either timer, manual retry (cancel), or service shutdown.
		select {
		case <-ctx.Done():
			watchCancel()
			return
		case <-time.After(reprobeInterval):
			// periodic re-probe
		case <-watchCtx.Done():
			// manual retry (RetryCaptiveCheck cancelled us) or service shutdown
			if ctx.Err() != nil {
				return
			}
			// manual retry — fall through to immediate re-probe
		}
		watchCancel()

		// Check service shutdown before probing (avoid wasted probe cycle).
		if ctx.Err() != nil {
			return
		}

		// Re-probe using background context so a service shutdown race
		// doesn't silently skip the result.
		probeDetail := captive.Probe(context.Background(), p.config.CaptiveProbeURLs)
		if probeDetail.Result == captive.NoPortal {
			fmt.Fprintf(serviceStderr, "service: captive portal cleared\n")
			// Do NOT Deactivate the captive firewall here. Leave it active
			// so there is zero gap between captive rules and full rules.
			// The normal Connect flow's Activate(ModeFull) atomically
			// replaces the captive ruleset (nftables flush+add in one
			// transaction; WFP begin+commit transaction). This eliminates
			// the traffic leak window that would occur if we Deactivated
			// first and then Activated later.
			p.captivePortal.Store(false)
			p.captiveProbeURL.Store("")
			p.captiveMu.Lock()
			p.captiveCancel = nil
			p.captiveMu.Unlock()
			return
		}
		// Still captive — loop.
		fmt.Fprintf(serviceStderr, "service: captive re-probe: still portal (result=%s)\n", probeDetail.Result)
	}
}

// DetectRealIP fetches the client's real public IP from the relay's /ip
// endpoint (best-effort, 5 s timeout). Using the relay — rather than a
// third-party echo like api.ipify.org — has two properties we need:
//  1. The endpoint is reachable even once the kill switch is armed, since
//     the relay is the one address the firewall keeps allowed. This lets
//     us re-detect the real IP on every manual Connect, catching ISP
//     address rotations that happened after service boot.
//  2. No extra DNS/TLS footprint leaked to a third party — the host was
//     going to resolve and TLS-handshake the relay anyway.
// Exported so ipchandler can refresh the value on each Connect.
func (p *Program) DetectRealIP(ctx context.Context) {
	if ctx == nil {
		ctx = context.Background()
	}
	detectCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	relayDomain := p.config.RelayDomain
	if tc := p.tunnelClient; tc != nil {
		if d := tc.RelayDomain(); d != "" {
			relayDomain = d
		}
	}
	if relayDomain == "" {
		return
	}

	req, err := http.NewRequestWithContext(detectCtx, http.MethodGet, "https://"+relayDomain+"/ip", nil)
	if err != nil {
		return
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, 64))
	if err != nil {
		return
	}
	ip := strings.TrimSpace(string(body))
	if ip != "" {
		p.SetRealIP(ip)
	}
}

// DetectVisibleIP resolves the relay domain to get the exit IP that external
// services see when traffic goes through the tunnel.
// Safe to call from any goroutine.
func (p *Program) DetectVisibleIP(_ context.Context) {
	tc := p.tunnelClient
	if tc == nil {
		return
	}
	addrs, err := net.LookupHost(tc.RelayDomain())
	if err != nil || len(addrs) == 0 {
		return
	}
	p.SetVisibleIP(addrs[0])
}
