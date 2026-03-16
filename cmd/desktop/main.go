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
	cfgPath := config.DiscoverPath("")
	if cfg, err := config.Load(cfgPath); err == nil {
		relayDomain = cfg.Relay.Domain
	}

	app := desktop.NewApp(ipc.NewClient(), relayDomain)

	if err := wails.Run(&options.App{
		Title:            "Le Voile",
		Width:            400,
		Height:           500,
		MinWidth:         400,
		MinHeight:        500,
		MaxWidth:         400,
		MaxHeight:        500,
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
		Bind: []interface{}{
			app,
		},
	}); err != nil {
		os.Exit(1)
	}
}
