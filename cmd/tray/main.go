// Command tray runs the Le Voile system tray UI process.
// This is a separate process from the service; they communicate via IPC.
package main

import (
	"fmt"
	"os"

	"github.com/velia-the-veil/le_voile/internal/config"
	"github.com/velia-the-veil/le_voile/internal/tray"
)

var version string

func main() {
	// AC6: Ensure only one tray instance runs at a time (named mutex on Windows).
	if err := tray.AcquireSingleton(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(0)
	}
	defer tray.ReleaseSingleton()

	autoStart := true
	blocklistEnabled := false
	httpProxyEnabled := false
	relayDomain := ""
	if cfgPath, err := config.DefaultPath(); err == nil {
		if cfg, err := config.Load(cfgPath); err == nil {
			autoStart = cfg.Client.AutoStart
			blocklistEnabled = cfg.Blocklist.Enabled
			httpProxyEnabled = cfg.HTTPProxy.Enabled
			relayDomain = cfg.Relay.Domain
		}
	}

	t := tray.NewWithConfig(autoStart, false, blocklistEnabled, httpProxyEnabled, relayDomain)
	t.Run()
}
