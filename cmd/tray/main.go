// Command tray runs the Le Voile system tray UI process.
// This is a separate process from the service; they communicate via IPC.
package main

import (
	"github.com/velia-the-veil/le_voile/internal/config"
	"github.com/velia-the-veil/le_voile/internal/tray"
)

var version string

func main() {
	autoStart := true
	blocklistEnabled := false
	if cfgPath, err := config.DefaultPath(); err == nil {
		if cfg, err := config.Load(cfgPath); err == nil {
			autoStart = cfg.Client.AutoStart
			blocklistEnabled = cfg.Blocklist.Enabled
		}
	}

	t := tray.NewWithConfig(autoStart, false, blocklistEnabled)
	t.Run()
}
