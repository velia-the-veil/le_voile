package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"runtime"
	"time"

	"github.com/kardianos/service"

	"github.com/velia-the-veil/le_voile/internal/browser"
	"github.com/velia-the-veil/le_voile/internal/config"
	"github.com/velia-the-veil/le_voile/internal/ipc"
	"github.com/velia-the-veil/le_voile/internal/ipchandler"
	svc "github.com/velia-the-veil/le_voile/internal/service"
)

var version string

const defaultRelayDomain = "levoile.dev"


// resolvedConfig holds the result of config resolution.
type resolvedConfig struct {
	relayDomain       string
	relayPubKey       string
	insecure          bool
	stunDefaultServer string
	updateEnabled     bool
	updateInterval    time.Duration
	updateRateLimit   int64
	updateOwner       string
	updateRepo        string
	updateStagingDir  string
	blocklistEnabled  bool
	blocklistInterval time.Duration

	registryEnabled         bool
	registryURL             string
	registryMasterPubKey    string
	registryRefreshInterval time.Duration

	httpProxyEnabled bool
	httpProxyPort    int

	browserPoliciesEnabled bool

	preferredCountry string
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

	if rc.relayPubKey == "" {
		return resolvedConfig{}, fmt.Errorf("client: relay public key is required (set in config file or via -relay-pubkey flag)")
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
	if cfg.BrowserPolicies.ChromeStoreUpdateURL != "" {
		browser.ChromeStoreUpdateURL = cfg.BrowserPolicies.ChromeStoreUpdateURL
	}

	// Resolve preferred country.
	rc.preferredCountry = cfg.Client.PreferredCountry

	return rc, nil
}

// newServiceConfig returns the kardianos/service configuration.
func newServiceConfig() *service.Config {
	return &service.Config{
		Name:        svc.ServiceName,
		DisplayName: "Le Voile",
		Description: "VPN minimaliste zero-log",
		Option: service.KeyValue{
			// Windows SCM: restart service on failure after 5 seconds.
			"OnFailure":              "restart",
			"OnFailureDelayDuration": "5s",
			"OnFailureResetPeriod":   10,
		},
	}
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
	flag.Parse()

	// Handle service management sub-commands first (no relay config needed).
	args := flag.Args()
	if len(args) > 0 {
		switch args[0] {
		case "install", "uninstall", "start", "stop":
			handleServiceCommand(args[0])
			return
		case "run":
			// Explicit run mode — falls through to full service setup below.
		default:
			fmt.Fprintf(os.Stderr, "client: unknown command: %s\n", args[0])
			fmt.Fprintln(os.Stderr, "Available commands: install, uninstall, start, stop, run")
			os.Exit(1)
		}
	}

	// Resolve config (only needed for run mode).
	cfgPath := config.DiscoverPath(*configFlag)
	rc, err := resolveConfig(cfgPath, *relayDomainFlag, *relayPubKeyFlag, *insecureFlag)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}

	prg := svc.NewProgram(svc.Config{
		RelayDomain:       rc.relayDomain,
		RelayPubKey:       rc.relayPubKey,
		Insecure:          rc.insecure,
		STUNDefaultServer: rc.stunDefaultServer,
		UpdateEnabled:     rc.updateEnabled,
		UpdateInterval:    rc.updateInterval,
		UpdateRateLimit:   rc.updateRateLimit,
		UpdateOwner:       rc.updateOwner,
		UpdateRepo:        rc.updateRepo,
		UpdateStagingDir:  rc.updateStagingDir,
		BlocklistEnabled:        rc.blocklistEnabled,
		BlocklistInterval:       rc.blocklistInterval,
		RegistryEnabled:         rc.registryEnabled,
		RegistryURL:             rc.registryURL,
		RegistryMasterPubKey:    rc.registryMasterPubKey,
		RegistryRefreshInterval: rc.registryRefreshInterval,
		HTTPProxyEnabled:        rc.httpProxyEnabled,
		HTTPProxyPort:           rc.httpProxyPort,
		BrowserPoliciesEnabled: rc.browserPoliciesEnabled,
		PreferredCountry:       rc.preferredCountry,
	})

	// Set up IPC server with handler that bridges to the service.
	opts := ipchandler.Options{
		ConfigPathFn:     func() string { return config.DiscoverPath("") },
		SetStartupTypeFn: setServiceStartupType,
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

	s, err := service.New(prg, newServiceConfig())
	if err != nil {
		fmt.Fprintf(os.Stderr, "client: %v\n", err)
		os.Exit(1)
	}

	// Default: run the service (interactive or service manager context).
	if err := s.Run(); err != nil {
		fmt.Fprintf(os.Stderr, "client: %v\n", err)
		os.Exit(1)
	}
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

