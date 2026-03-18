// Command portable runs Le Voile in portable mode: service + tray in a single binary.
// No OS service registration, no auto-start registry key.
package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"sync/atomic"
	"syscall"
	"time"

	"github.com/velia-the-veil/le_voile/internal/config"
	"github.com/velia-the-veil/le_voile/internal/elevation"
	"github.com/velia-the-veil/le_voile/internal/ipc"
	"github.com/velia-the-veil/le_voile/internal/ipchandler"
	svc "github.com/velia-the-veil/le_voile/internal/service"
	"github.com/velia-the-veil/le_voile/internal/tray"
)

var version string

const defaultRelayDomain = "levoile.dev"

// dialIPC attempts to connect to the IPC named pipe to detect a running instance.
// Variable for test override.
var dialIPC = func() error {
	c := ipc.NewClient()
	if err := c.Connect(); err != nil {
		return err
	}
	c.Close()
	return nil
}

func main() {
	// 0. Display version.
	if version != "" {
		fmt.Printf("Le Voile portable %s\n", version)
	}

	// 1. Check admin/root elevation.
	if !elevation.IsElevated() {
		fmt.Println("Le Voile necessite les privileges administrateur pour modifier le DNS systeme.")
		fmt.Println("Demande d'elevation UAC en cours...")
		if err := elevation.RelaunchElevated(); err != nil {
			fmt.Fprintf(os.Stderr, "portable: elevation: %v\n", err)
			os.Exit(1)
		}
		return
	}

	// 2. Detect conflict with installed version.
	if err := dialIPC(); err == nil {
		fmt.Fprintln(os.Stderr, "Le service Le Voile est deja en cours d'execution. Fermez-le avant de lancer la version portable.")
		os.Exit(1)
	}

	// 3. Load config — portable: exe dir only, NOT AppData.
	cfgPath := config.DiscoverPortablePath()
	cfg := &config.Config{
		Client: config.ClientConfig{AutoStart: true},
	}
	if cfgPath != "" {
		loaded, err := config.Load(cfgPath)
		if err != nil {
			fmt.Fprintf(os.Stderr, "portable: config: %v\n", err)
			os.Exit(1)
		}
		cfg = loaded
	}
	if cfg.Relay.Domain == "" {
		cfg.Relay.Domain = defaultRelayDomain
	}
	if cfg.Relay.PublicKeyEd25519 == "" {
		fmt.Fprintln(os.Stderr, "portable: cle publique Ed25519 requise. Placez un fichier config.toml a cote de l'executable.")
		os.Exit(1)
	}

	// 4. Create the Program (inline service).
	svcCfg := svc.Config{
		RelayDomain:          cfg.Relay.Domain,
		RelayPubKey:          cfg.Relay.PublicKeyEd25519,
		Insecure:             cfg.Relay.Insecure,
		STUNDefaultServer:    cfg.STUN.DefaultServer,
		UpdateEnabled:        cfg.Update.Enabled,
		UpdateOwner:          cfg.Update.GitHubOwner,
		UpdateRepo:           cfg.Update.GitHubRepo,
		UpdateRateLimit:      int64(cfg.Update.RateLimitKBps) * 1024,
		BlocklistEnabled:     cfg.Blocklist.Enabled,
		RegistryEnabled:      cfg.Registry.Enabled,
		RegistryURL:          cfg.Registry.URL,
		RegistryMasterPubKey: cfg.Registry.MasterPublicKey,
		HTTPProxyEnabled:     cfg.HTTPProxy.Enabled,
		HTTPProxyPort:        cfg.HTTPProxy.Port,
	}
	if cfg.Update.CheckInterval != "" {
		d, err := time.ParseDuration(cfg.Update.CheckInterval)
		if err != nil {
			fmt.Fprintf(os.Stderr, "portable: config: invalid check_interval %q: %v\n", cfg.Update.CheckInterval, err)
			os.Exit(1)
		}
		svcCfg.UpdateInterval = d
	}
	if cfg.Update.Enabled {
		stagingDir, err := config.StagingDir()
		if err != nil {
			fmt.Fprintf(os.Stderr, "portable: config: staging dir: %v\n", err)
			os.Exit(1)
		}
		svcCfg.UpdateStagingDir = stagingDir
	}
	if cfg.Blocklist.UpdateInterval != "" {
		d, err := time.ParseDuration(cfg.Blocklist.UpdateInterval)
		if err == nil {
			svcCfg.BlocklistInterval = d
		}
	}
	if cfg.Registry.RefreshInterval != "" {
		d, err := time.ParseDuration(cfg.Registry.RefreshInterval)
		if err == nil {
			svcCfg.RegistryRefreshInterval = d
		}
	}
	prg := svc.NewProgram(svcCfg)

	// 5. Set up IPC server with portable handler options.
	opts := ipchandler.Options{
		ConfigPathFn: config.DiscoverPortablePath,
		// SetStartupTypeFn is nil — no SCM service in portable mode.
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

	// 6. Handle OS signals (SIGINT, SIGTERM) for clean shutdown.
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, os.Interrupt, syscall.SIGTERM)
	var signalStopped atomic.Bool
	go func() {
		<-sigCh
		signalStopped.Store(true)
		// Stop blocks until shutdown() completes (including DNS restoration).
		// The tray.Run() loop will eventually exit, and defer prg.Stop handles
		// the normal path. For signal shutdown, we stop the tray to unblock main.
		prg.Stop(nil)
	}()

	// 7. Start the service in a goroutine (non-blocking).
	prg.Start(nil)
	defer func() {
		// Skip if signal handler already called Stop (avoids blocking twice on p.done).
		if !signalStopped.Load() {
			prg.Stop(nil)
		}
	}()

	// 8. Start tray on the main thread (BLOCKING).
	autoStart := cfg.Client.AutoStart
	t := tray.NewWithConfig(autoStart, true, cfg.Blocklist.Enabled, cfg.HTTPProxy.Enabled, cfg.Relay.Domain) // true = portableMode
	t.Run()

	// 9. Clean shutdown — prg.Stop(nil) via defer.
}
