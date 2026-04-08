// Command ui runs the Le Voile unified UI process (systray + webview + local HTTP server).
// This is a separate process from the service; they communicate via IPC.
package main

import (
	"fmt"
	"os"

	"github.com/velia-the-veil/le_voile/frontend"
	"github.com/velia-the-veil/le_voile/internal/config"
	"github.com/velia-the-veil/le_voile/internal/ipc"
	"github.com/velia-the-veil/le_voile/internal/ui"
)

var version string

func main() {
	if err := ui.AcquireSingleton(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(0)
	}
	defer ui.ReleaseSingleton()

	relayDomain := ""
	if cfgPath, err := config.DefaultPath(); err == nil {
		if cfg, err := config.Load(cfgPath); err == nil {
			relayDomain = cfg.Relay.Domain
		}
	}

	client := ipc.NewClient()
	u := ui.New(client, ui.Config{
		RelayDomain: relayDomain,
		FrontendFS:  frontend.Assets,
	})
	u.Run()
}
