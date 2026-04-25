package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"time"

	"github.com/kardianos/service"

	"github.com/velia-the-veil/le_voile/internal/config"
	"github.com/velia-the-veil/le_voile/internal/ctlauth"
	"github.com/velia-the-veil/le_voile/internal/firewall"
	"github.com/velia-the-veil/le_voile/internal/ipc"
	"github.com/velia-the-veil/le_voile/internal/ipchandler"
	svc "github.com/velia-the-veil/le_voile/internal/service"
	"github.com/velia-the-veil/le_voile/internal/tun"
	"github.com/velia-the-veil/le_voile/internal/updater"
)

var version string

func init() {
	// Propagate the ldflags-injected version to the updater package so
	// CurrentVersion() returns the real build version instead of "dev".
	if version != "" {
		updater.Version = version
	}
}

const defaultRelayDomain = "levoile.dev"


// resolvedConfig holds the result of config resolution.
type resolvedConfig struct {
	relayDomain           string
	relayPubKey           string
	insecure              bool
	stunDefaultServer     string
	stunServers           []string
	stunLeakcheckInterval time.Duration
	updateEnabled         bool
	updateInterval    time.Duration
	updateRateLimit   int64
	updateOwner       string
	updateRepo        string
	updateStagingDir  string
	// Story 8.2 — package-manager override + retry abandon cap.
	updateAllowWhenPackaged bool
	updateMaxInstallRetries int
	blocklistEnabled  bool
	blocklistInterval time.Duration

	registryEnabled             bool
	registryURL                 string
	registryMasterPubKey        string
	registryRefreshInterval     time.Duration
	registryBootstrapDoHEnabled bool
	registryDoHUpstreams        []string

	httpProxyEnabled bool
	httpProxyPort    int

	browserPoliciesEnabled bool

	preferredCountry string

	tunEnabled bool
	tunName    string
	tunMTU     int

	firewallEnabled bool
	allowIPv6Leak   bool

	captiveEnabled   bool
	captiveProbeURLs []string
}

// resolveConfig loads config from file and applies CLI flag overrides.
// Flag values (non-empty) take priority over file values.
func resolveConfig(cfgPath, flagDomain, flagPubKey string, flagInsecure bool) (resolvedConfig, error) {
	cfg, err := config.Load(cfgPath)
	if err != nil {
		return resolvedConfig{}, fmt.Errorf("client: config: %w", err)
	}

	rc := resolvedConfig{
		relayDomain:       cfg.Relay.Domain,
		relayPubKey:       cfg.Relay.PublicKeyEd25519,
		insecure:          cfg.Relay.Insecure,
		stunDefaultServer: cfg.STUN.DefaultServer,
		stunServers:       cfg.STUN.Servers,
	}
	if cfg.STUN.LeakcheckInterval != "" {
		d, err := time.ParseDuration(cfg.STUN.LeakcheckInterval)
		if err != nil {
			return resolvedConfig{}, fmt.Errorf("client: config: invalid stun.leakcheck_interval %q: %w", cfg.STUN.LeakcheckInterval, err)
		}
		rc.stunLeakcheckInterval = d
	}

	// CLI flags override file values (backward compatibility).
	if flagDomain != "" {
		rc.relayDomain = flagDomain
	}
	if flagPubKey != "" {
		rc.relayPubKey = flagPubKey
	}
	if flagInsecure {
		rc.insecure = true
	}

	// Default domain if still empty.
	if rc.relayDomain == "" {
		rc.relayDomain = defaultRelayDomain
	}

	if rc.relayPubKey == "" || rc.relayPubKey == "YOUR_ED25519_PUBLIC_KEY_BASE64" {
		return resolvedConfig{}, fmt.Errorf(
			"client: relay public key is required — remplissez relay.public_key_ed25519 dans %s, passez -relay-pubkey, ou utilisez un paquet d'installation qui fournit une config signée",
			cfgPath,
		)
	}

	// Resolve update config.
	rc.updateEnabled = cfg.Update.Enabled
	rc.updateOwner = cfg.Update.GitHubOwner
	rc.updateRepo = cfg.Update.GitHubRepo
	rc.updateRateLimit = int64(cfg.Update.RateLimitKBps) * 1024
	if cfg.Update.CheckInterval != "" {
		d, err := time.ParseDuration(cfg.Update.CheckInterval)
		if err != nil {
			return resolvedConfig{}, fmt.Errorf("client: config: invalid check_interval %q: %w", cfg.Update.CheckInterval, err)
		}
		rc.updateInterval = d
	}
	if rc.updateEnabled {
		stagingDir, err := config.StagingDir()
		if err != nil {
			return resolvedConfig{}, fmt.Errorf("client: config: staging dir: %w", err)
		}
		rc.updateStagingDir = stagingDir
	}
	// Story 8.2 — package-manager override + retry abandon cap.
	rc.updateAllowWhenPackaged = cfg.Update.AllowWhenPackaged
	rc.updateMaxInstallRetries = cfg.Update.MaxInstallRetries

	// Resolve blocklist config.
	rc.blocklistEnabled = cfg.Blocklist.Enabled
	if cfg.Blocklist.UpdateInterval != "" {
		d, err := time.ParseDuration(cfg.Blocklist.UpdateInterval)
		if err != nil {
			return resolvedConfig{}, fmt.Errorf("client: config: invalid blocklist update_interval %q: %w", cfg.Blocklist.UpdateInterval, err)
		}
		rc.blocklistInterval = d
	}

	// Resolve registry config.
	rc.registryEnabled = cfg.Registry.Enabled
	rc.registryURL = cfg.Registry.URL
	rc.registryMasterPubKey = cfg.Registry.MasterPublicKey
	rc.registryBootstrapDoHEnabled = cfg.Registry.BootstrapDoHEnabled
	rc.registryDoHUpstreams = cfg.Registry.DoHUpstreams
	if cfg.Registry.RefreshInterval != "" {
		d, err := time.ParseDuration(cfg.Registry.RefreshInterval)
		if err != nil {
			return resolvedConfig{}, fmt.Errorf("client: config: invalid registry refresh_interval %q: %w", cfg.Registry.RefreshInterval, err)
		}
		rc.registryRefreshInterval = d
	}

	// Resolve HTTP proxy config.
	rc.httpProxyEnabled = cfg.HTTPProxy.Enabled
	rc.httpProxyPort = cfg.HTTPProxy.Port

	// Resolve browser policies config.
	rc.browserPoliciesEnabled = cfg.BrowserPolicies.Enabled

	// Resolve preferred country.
	rc.preferredCountry = cfg.Client.PreferredCountry

	// Resolve TUN config (Epic 2). Activation primaire via TOML ; env
	// LEVOILE_TUN_ENABLED=1 en override pour tests/CI sans toucher au
	// fichier config.
	rc.tunEnabled = cfg.TUN.Enabled
	rc.tunName = cfg.TUN.Name
	rc.tunMTU = cfg.TUN.MTU
	if os.Getenv("LEVOILE_TUN_ENABLED") == "1" {
		rc.tunEnabled = true
	}

	// Resolve firewall config (Story 2.7 + 2.9).
	rc.firewallEnabled = cfg.Firewall.EnableKillSwitch
	rc.allowIPv6Leak = cfg.Firewall.AllowIPv6Leak

	// Resolve captive portal config (Story 2.8).
	rc.captiveEnabled = cfg.Captive.Enabled
	rc.captiveProbeURLs = cfg.Captive.ProbeURLs

	return rc, nil
}

// newServiceConfig returns the kardianos/service configuration.
//
// Option block configures SCM auto-restart on crash (Story 7.1 AC2 + NFR15) —
// kardianos/service translates these into `sc failure LeVoile reset= 10
// actions= restart/5000` at install time. Without this, a crashed service
// stays down until manual restart, defeating the kill-switch guarantee.
func newServiceConfig() *service.Config {
	return &service.Config{
		Name:        svc.ServiceName,
		DisplayName: "Le Voile",
		Description: "VPN minimaliste zero-log",
		Option: service.KeyValue{
			"OnFailure":              "restart",
			"OnFailureDelayDuration": "5s",
			"OnFailureResetPeriod":   10,
		},
	}
}

// cleanupFirewall is a package-level seam so tests can stub out the WFP/nft
// call without going near the real OS APIs (which require elevation).
var cleanupFirewall = func(ctx context.Context) (int, error) {
	return firewall.New(nil, firewall.Options{}).CleanupOrphans(ctx)
}

// cleanupTun is a package-level seam so tests can stub out the TUN/Wintun
// adapter destruction without needing CAP_NET_ADMIN / LocalSystem.
var cleanupTun = func() error {
	return tun.CleanupOrphan(tun.DefaultName)
}

// runCleanup forces removal of orphan firewall filters (WFP provider on
// Windows / nftables ruleset on Linux) and the TUN/Wintun adapter named
// levoile0. Both operations are idempotent — they return success when there
// is nothing to clean. Used by NSIS uninstall (Story 7.1) to recover from
// crashed-service scenarios where shutdown() never ran.
func runCleanup() int {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	exit := 0

	if n, err := cleanupFirewall(ctx); err != nil {
		fmt.Fprintf(os.Stderr, "client: cleanup firewall: %v\n", err)
		exit = 1
	} else {
		fmt.Printf("Firewall cleanup: %d orphan filter(s) removed.\n", n)
	}

	if err := cleanupTun(); err != nil {
		fmt.Fprintf(os.Stderr, "client: cleanup TUN: %v\n", err)
		exit = 1
	} else {
		fmt.Printf("TUN cleanup: interface %q removed if present.\n", tun.DefaultName)
	}

	return exit
}

// handleServiceCommand handles install/uninstall/start/stop sub-commands.
// These only interact with the OS service manager and do not need relay config.
func handleServiceCommand(cmd string) {
	prg := svc.NewProgram(svc.Config{})
	s, err := service.New(prg, newServiceConfig())
	if err != nil {
		fmt.Fprintf(os.Stderr, "client: %v\n", err)
		os.Exit(1)
	}

	switch cmd {
	case "install":
		if err := s.Install(); err != nil {
			fmt.Fprintf(os.Stderr, "client: install: %v\n", err)
			os.Exit(1)
		}
		// kardianos/service registers SCM with SERVICE_AUTO_START by
		// default. When a reinstall preserves a config.toml that had
		// auto_start=false, leaving the default means SCM would start the
		// service at next boot regardless of the user's preference.
		// Immediately re-apply the TOML value to close that window.
		// Fresh installs (auto_start=true) are idempotent no-ops.
		if cfgPath := config.DiscoverPath(""); cfgPath != "" {
			if cfg, err := config.Load(cfgPath); err == nil {
				if err := setServiceStartupType(cfg.Client.AutoStart); err != nil {
					fmt.Fprintf(os.Stderr, "client: install: apply startup type: %v\n", err)
				}
			}
		}
		fmt.Println("Service installed.")
	case "uninstall":
		if err := s.Uninstall(); err != nil {
			fmt.Fprintf(os.Stderr, "client: uninstall: %v\n", err)
			os.Exit(1)
		}
		fmt.Println("Service uninstalled.")
	case "start":
		if err := s.Start(); err != nil {
			fmt.Fprintf(os.Stderr, "client: start: %v\n", err)
			os.Exit(1)
		}
		fmt.Println("Service started.")
	case "stop":
		if err := s.Stop(); err != nil {
			fmt.Fprintf(os.Stderr, "client: stop: %v\n", err)
			os.Exit(1)
		}
		fmt.Println("Service stopped.")
	}
}

func main() {
	configFlag := flag.String("config", "", "config TOML path (optional, auto-detected)")
	relayDomainFlag := flag.String("relay-domain", "", "relay domain name (overrides config file)")
	relayPubKeyFlag := flag.String("relay-pubkey", "", "relay Ed25519 public key (overrides config file)")
	insecureFlag := flag.Bool("insecure", false, "skip TLS certificate verification (development only)")
	versionFlag := flag.Bool("version", false, "print version info and exit")
	flag.Parse()

	if *versionFlag {
		fmt.Printf("main.version=%q updater.Version=%q CurrentVersion()=%q\n", version, updater.Version, updater.CurrentVersion())
		os.Exit(0)
	}

	// Handle service management sub-commands first (no relay config needed).
	args := flag.Args()
	if len(args) > 0 {
		switch args[0] {
		case "install", "uninstall", "start", "stop":
			handleServiceCommand(args[0])
			return
		case "cleanup":
			// Story 7.1 — force-cleanup of WFP filters + Wintun adapter for
			// uninstall robustness. Runs without service config, idempotent.
			os.Exit(runCleanup())
		case "run":
			// Explicit run mode — falls through to full service setup below.
		default:
			fmt.Fprintf(os.Stderr, "client: unknown command: %s\n", args[0])
			fmt.Fprintln(os.Stderr, "Available commands: install, uninstall, start, stop, cleanup, run")
			os.Exit(1)
		}
	}

	// Resolve config (only needed for run mode). Bootstrap a signed skeleton
	// on first run so Load always sees a file — embed-based default from
	// config.example.toml. NFR9j / Story 7.5 AC3.
	cfgPath := config.DiscoverPath(*configFlag)
	if err := config.Bootstrap(cfgPath); err != nil {
		fmt.Fprintf(os.Stderr, "client: config bootstrap: %v\n", err)
		os.Exit(1)
	}

	// Story 7.5 — verify config integrity before touching resolved settings.
	// A mismatch does NOT exit the process: the service starts in a locked
	// mode (Connect refused, kill switch state preserved) so the UI can
	// surface the alert banner. First run / legacy upgrade autoheals by
	// writing a fresh HMAC sidecar.
	integrityFailed := false
	keyPath, keyErr := config.IntegrityKeyPath()
	if keyErr != nil {
		fmt.Fprintf(os.Stderr, "client: integrity key path: %v\n", keyErr)
		os.Exit(1)
	}
	integrityKey, keyErr := config.LoadOrCreateKey(keyPath)
	if keyErr != nil {
		fmt.Fprintf(os.Stderr, "client: integrity key init: %v\n", keyErr)
		os.Exit(1)
	}
	switch err := config.Verify(cfgPath, integrityKey); {
	case err == nil:
		// OK — signed config matches on-disk contents.
	case errors.Is(err, config.ErrHMACAbsent):
		// Legacy install or first run after Bootstrap — write a fresh sidecar.
		if signErr := config.Sign(cfgPath, integrityKey); signErr != nil {
			fmt.Fprintf(os.Stderr, "client: integrity sign (legacy migration): %v\n", signErr)
			os.Exit(1)
		}
	case errors.Is(err, config.ErrIntegrityMismatch):
		// Log WARNING without config contents or path (NFR22a).
		fmt.Fprintln(os.Stderr, "client: WARN config integrity mismatch — tunnel + mutations locked until hors-band recovery")
		integrityFailed = true
	default:
		fmt.Fprintf(os.Stderr, "client: integrity verify: %v\n", err)
		os.Exit(1)
	}

	rc, err := resolveConfig(cfgPath, *relayDomainFlag, *relayPubKeyFlag, *insecureFlag)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}

	prg := svc.NewProgram(svc.Config{
		RelayDomain:           rc.relayDomain,
		RelayPubKey:           rc.relayPubKey,
		Insecure:              rc.insecure,
		STUNDefaultServer:     rc.stunDefaultServer,
		STUNServers:           rc.stunServers,
		STUNLeakcheckInterval: rc.stunLeakcheckInterval,
		UpdateEnabled:         rc.updateEnabled,
		UpdateInterval:    rc.updateInterval,
		UpdateRateLimit:   rc.updateRateLimit,
		UpdateOwner:       rc.updateOwner,
		UpdateRepo:        rc.updateRepo,
		UpdateStagingDir:  rc.updateStagingDir,
		UpdateAllowWhenPackaged: rc.updateAllowWhenPackaged,
		UpdateMaxInstallRetries: rc.updateMaxInstallRetries,
		BlocklistEnabled:        rc.blocklistEnabled,
		BlocklistInterval:       rc.blocklistInterval,
		BlocklistCachePath:      filepath.Join(filepath.Dir(cfgPath), "blocklist-cache.txt"),
		RegistryEnabled:             rc.registryEnabled,
		RegistryURL:                 rc.registryURL,
		RegistryMasterPubKey:        rc.registryMasterPubKey,
		RegistryRefreshInterval:     rc.registryRefreshInterval,
		RegistryBootstrapDoHEnabled: rc.registryBootstrapDoHEnabled,
		RegistryDoHUpstreams:        rc.registryDoHUpstreams,
		HTTPProxyEnabled:        rc.httpProxyEnabled,
		HTTPProxyPort:           rc.httpProxyPort,
		BrowserPoliciesEnabled: rc.browserPoliciesEnabled,
		PreferredCountry:       rc.preferredCountry,

		TUNEnabled: rc.tunEnabled,
		TUNName:    rc.tunName,
		TUNMTU:     rc.tunMTU,

		FirewallEnabled:  rc.firewallEnabled,
		AllowIPv6Leak:    rc.allowIPv6Leak,
		CaptiveEnabled:   rc.captiveEnabled,
		CaptiveProbeURLs: rc.captiveProbeURLs,

		// Story 5.7 — supervise levoile-ui on Windows. Linux delegates to
		// systemd user units, so the watchdog is a no-op there.
		UIWatchdogEnabled: runtime.GOOS == "windows",
	})

	// Set up IPC server with handler that bridges to the service.
	opts := ipchandler.Options{
		ConfigPathFn:     func() string { return config.DiscoverPath("") },
		SetStartupTypeFn: setServiceStartupType,
		IntegrityKey:     integrityKey,
	}
	ipcListener := ipc.NewPlatformListener()
	ipcServer := ipc.NewServer(ipcListener)
	ipcServer.SetHandler(func(req ipc.Request) ipc.Response {
		return ipchandler.Handle(prg, req, opts)
	})
	prg.SetIPCServer(
		func(ctx context.Context) error { return ipcServer.Start(ctx) },
		func() { ipcServer.Stop() },
	)

	// Story 7.5 — propagate the integrity verdict to the service so the IPC
	// handler can gate Connect + mutations while the UI surfaces the banner.
	prg.SetIntegrityFailed(integrityFailed)

	// Story 5.9 — wire kill-switch persistence + ctl token. Persistence is
	// best-effort (nil callback skips it). Token init is best-effort: if it
	// fails (no perms, missing /etc/levoile), the service still runs but
	// levoile-ctl auth rejects all requests.
	prg.SetKillSwitchPersister(func(enabled bool) error {
		return persistFirewallEnabled(config.DiscoverPath(""), enabled, integrityKey)
	})
	if tokenPath := ctlauth.DefaultPath(); tokenPath != "" {
		if tok, err := ctlauth.LoadOrCreate(tokenPath); err != nil {
			fmt.Fprintf(os.Stderr, "client: ctl token init: %v (CLI auth disabled)\n", err)
		} else {
			prg.SetCtlToken(tok)
		}
	}

	s, err := service.New(prg, newServiceConfig())
	if err != nil {
		fmt.Fprintf(os.Stderr, "client: %v\n", err)
		os.Exit(1)
	}

	// NOTE: a "reconcile SCM startup type from config.toml on every service
	// start" pass lived here briefly. It called sc.exe config LeVoile
	// before s.Run() — i.e. while SCM was still waiting for the process
	// to call StartServiceCtrlDispatcher. Under load, that nested SCM
	// round-trip deadlocked or dragged out long enough to blow the UI's
	// webviewServiceReadyTimeout, surfacing as blank/hung "Ne répond pas"
	// WebView2 windows. We now rely on the two places that actually
	// mutate the SCM state: the install sub-command (post-s.Install) and
	// handleSetAutoStart (IPC from UI toggle). Drift from an out-of-band
	// `sc config` is no longer self-healed at startup; the user has to
	// re-toggle the setting in Paramètres to re-apply it.

	// Default: run the service (interactive or service manager context).
	if err := s.Run(); err != nil {
		fmt.Fprintf(os.Stderr, "client: %v\n", err)
		os.Exit(1)
	}
}

// persistFirewallEnabled rewrites firewall.enable_killswitch in the TOML
// config (Story 5.9). Used as the killSwitchPersist callback wired into the
// service. Holds config.Mu for the full load → modify → save sequence so
// concurrent IPC writers (set_blocklist, set_http_proxy, set_allow_ipv6_leak,
// select_country) cannot lose updates (Story 5.9 H2 fix). Best-effort: when
// cfgPath is empty (portable mode without a config file), persistence is
// silently skipped — runtime state still flips.
func persistFirewallEnabled(cfgPath string, enabled bool, integrityKey []byte) error {
	if cfgPath == "" {
		return nil
	}
	config.Mu.Lock()
	defer config.Mu.Unlock()
	cfg, err := config.Load(cfgPath)
	if err != nil {
		return fmt.Errorf("client: load config for killswitch persist: %w", err)
	}
	cfg.Firewall.EnableKillSwitch = enabled
	if err := cfg.SaveAndSign(cfgPath, integrityKey); err != nil {
		return fmt.Errorf("client: save config for killswitch persist: %w", err)
	}
	return nil
}

// setServiceStartupType changes the OS service startup type.
// Variable for test override.
var setServiceStartupType = setServiceStartupTypeOS

func setServiceStartupTypeOS(autoStart bool) error {
	switch runtime.GOOS {
	case "windows":
		startType := "auto"
		if !autoStart {
			startType = "demand"
		}
		cmd := exec.Command("sc", "config", svc.ServiceName, "start=", startType)
		if out, err := cmd.CombinedOutput(); err != nil {
			return fmt.Errorf("config: sc config: %s: %w", string(out), err)
		}
		// We used to also /Change /DISABLE the ONLOGON scheduled task here,
		// but that broke `schtasks /Run` (the path taken by the desktop
		// shortcut via launch-ui.vbs) on stock Windows — disabled tasks
		// cannot be explicitly run. The gate against auto-launch at logon
		// now lives in cmd/ui: the UI exits early when cfg.Client.AutoStart
		// is false and no recent user-launch flag was written by the VBS
		// launcher. Keeps the task enabled (silent elevation via /Run keeps
		// working) while preserving the no-VPN-at-boot promise.
	case "linux":
		action := "enable"
		if !autoStart {
			action = "disable"
		}
		cmd := exec.Command("systemctl", action, "levoile")
		if out, err := cmd.CombinedOutput(); err != nil {
			return fmt.Errorf("config: systemctl: %s: %w", string(out), err)
		}
	}
	return nil
}

