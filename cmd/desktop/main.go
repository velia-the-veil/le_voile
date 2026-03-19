// Command desktop runs the Le Voile Wails v2 desktop window.
// This is a separate process from the service and tray; it communicates via IPC.
// When launched from the desktop shortcut after a quit, it ensures the service
// and tray are running before opening the GUI.
package main

import (
	"os"
	"os/exec"
	"path/filepath"

	"github.com/wailsapp/wails/v2"
	"github.com/wailsapp/wails/v2/pkg/options"
	"github.com/wailsapp/wails/v2/pkg/options/assetserver"

	"github.com/velia-the-veil/le_voile/frontend"
	"github.com/velia-the-veil/le_voile/internal/config"
	"github.com/velia-the-veil/le_voile/internal/desktop"
	"github.com/velia-the-veil/le_voile/internal/ipc"
)

func main() {
	// Ensure the service and tray are running (covers desktop shortcut after quit).
	ensureServiceAndTray()

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

// ensureServiceAndTray starts the Windows service and tray if not already running.
func ensureServiceAndTray() {
	self, err := os.Executable()
	if err != nil {
		return
	}
	dir := filepath.Dir(self)
	servicePath := filepath.Join(dir, "levoile-service.exe")
	trayPath := filepath.Join(dir, "levoile-tray.exe")

	// Start service (no-op if already running — SCM returns "already running" error).
	exec.Command(servicePath, "start").Run()

	// Always try to start the tray — the singleton mutex in AcquireSingleton()
	// prevents duplicates. If already running, the new instance exits immediately.
	exec.Command(trayPath).Start()
}
