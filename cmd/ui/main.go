// Command ui runs the Le Voile unified UI process (systray + webview + local HTTP server).
// This is a separate process from the service; they communicate via IPC.
package main

import (
	"fmt"
	"os"

	"github.com/velia-the-veil/le_voile/frontend"
	"github.com/velia-the-veil/le_voile/internal/config"
	"github.com/velia-the-veil/le_voile/internal/ctlauth"
	"github.com/velia-the-veil/le_voile/internal/ipc"
	"github.com/velia-the-veil/le_voile/internal/ui"
)

var version string

func main() {
	// Ensure the service is running (covers desktop shortcut after quit).
	ensureService()

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

	// Load the ctlauth token written by the service at bootstrap. Empty on
	// read error — sendIPC then issues pre-strict-auth calls and the service
	// logs a SECURITY AUDIT stderr line per mutation, letting operators
	// notice the missing token without breaking the install.
	var authToken string
	if tokenPath := ctlauth.DefaultPath(); tokenPath != "" {
		if raw, err := ctlauth.Load(tokenPath); err == nil {
			authToken = ctlauth.Hex(raw)
		}
	}

	client := ipc.NewClient()
	u := ui.New(client, ui.Config{
		RelayDomain: relayDomain,
		FrontendFS:  frontend.Assets,
		AuthToken:   authToken,
	})
	u.Run()
}
