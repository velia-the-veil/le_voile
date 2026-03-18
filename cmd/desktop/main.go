// Command desktop runs the Le Voile Wails v2 desktop window.
// This is a separate process from the service and tray; it communicates via IPC.
package main

import (
	"os"

	"github.com/wailsapp/wails/v2"
	"github.com/wailsapp/wails/v2/pkg/options"
	"github.com/wailsapp/wails/v2/pkg/options/assetserver"

	"github.com/velia-the-veil/le_voile/frontend"
	"github.com/velia-the-veil/le_voile/internal/config"
	"github.com/velia-the-veil/le_voile/internal/desktop"
	"github.com/velia-the-veil/le_voile/internal/ipc"
)

func main() {
	relayDomain := ""
	skipQuitModal := false
	cfgPath := config.DiscoverPath("")
	if cfg, err := config.Load(cfgPath); err == nil {
		relayDomain = cfg.Relay.Domain
		skipQuitModal = cfg.Client.SkipQuitModal
	}

	app := desktop.NewApp(ipc.NewClient(), relayDomain, cfgPath, skipQuitModal)

	if err := wails.Run(&options.App{
		Title:            "Le Voile",
		Width:            420,
		Height:           540,
		MinWidth:         420,
		MinHeight:        540,
		MaxWidth:         420,
		MaxHeight:        540,
		DisableResize:    true,
		Frameless:        true,
		StartHidden:      false,
		CSSDragProperty:  "--wails-draggable",
		CSSDragValue:     "drag",
		BackgroundColour: &options.RGBA{R: 11, G: 21, B: 38, A: 255},
		AssetServer: &assetserver.Options{
			Assets: frontend.Assets,
		},
		OnStartup:  app.Startup,
		OnShutdown: app.Shutdown,
		// AC2: Closing the window (X button) hides it instead of destroying
		// the process. The tray remains active and the window can be re-opened
		// via left-click on the tray icon. Only the tray's "Quitter" menu
		// triggers a real quit (via App.Quit → runtime.Quit).
		OnBeforeClose: app.OnBeforeClose,
		Bind: []interface{}{
			app,
		},
	}); err != nil {
		os.Exit(1)
	}
}
