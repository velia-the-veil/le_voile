// Command ui runs the Le Voile unified UI process (systray + webview + local HTTP server).
// This is a separate process from the service; they communicate via IPC.
package main

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"time"

	"github.com/velia-the-veil/le_voile/frontend"
	"github.com/velia-the-veil/le_voile/internal/config"
	"github.com/velia-the-veil/le_voile/internal/ctlauth"
	"github.com/velia-the-veil/le_voile/internal/ipc"
	"github.com/velia-the-veil/le_voile/internal/ui"
)

var version string

// userLaunchFlagName is the sentinel written by installer/levoile.nsi's
// launch-ui.vbs to %TEMP% just before it calls `schtasks /Run` on the
// ONLOGON task. Its presence at UI startup distinguishes a user-initiated
// shortcut click from an automatic ONLOGON launch, which is how the "Ne
// pas démarrer automatiquement" preference blocks the tray without
// breaking manual launches.
const userLaunchFlagName = "levoile-user-launch.flag"

// userLaunchFlagTTL bounds how stale the flag can be before we ignore it.
// 15 seconds accommodates a slow cold-start of Task Scheduler + wscript
// without leaving a forgotten flag around long enough to mask a later
// ONLOGON auto-launch as a user-initiated one.
const userLaunchFlagTTL = 15 * time.Second

func main() {
	if shouldSuppressAutoLaunch() {
		os.Exit(0)
	}
	// Ensure the service is running (covers desktop shortcut after quit).
	ensureService()

	if err := ui.AcquireSingleton(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(0)
	}
	defer ui.ReleaseSingleton()

	relayDomain := ""
	if cfgPath := config.DiscoverPath(""); cfgPath != "" {
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

// shouldSuppressAutoLaunch returns true when this process was started by
// the ONLOGON scheduled task and the user has opted out of auto-start.
// Linux returns false unconditionally — auto-start there is gated by
// systemctl enable/disable at the unit level.
//
// Detection: launch-ui.vbs (shipped by installer/levoile.nsi and run by
// the desktop shortcut) writes levoile-user-launch.flag to %TEMP% before
// it invokes `schtasks /Run`. The Task Scheduler-spawned process inherits
// the interactive user's session (thanks to /IT /RL HIGHEST), so its
// os.TempDir() resolves to the same %TEMP% the VBS wrote to. Presence of
// a recent flag → user-initiated launch → proceed and delete the flag.
// Absence → no VBS ran → ONLOGON auto-launch → suppress when the user has
// auto_start = false. Stale flag (> userLaunchFlagTTL) is treated as
// absent so a crashed earlier shortcut click can't let a future ONLOGON
// through.
func shouldSuppressAutoLaunch() bool {
	if runtime.GOOS != "windows" {
		return false
	}
	cfgPath := config.DiscoverPath("")
	if cfgPath == "" {
		return false
	}
	cfg, err := config.Load(cfgPath)
	if err != nil {
		// Fail-open: without a readable config we can't tell what the
		// user wants; better to launch than silently fail to appear.
		return false
	}
	if cfg.Client.AutoStart {
		return false
	}
	flagPath := filepath.Join(os.TempDir(), userLaunchFlagName)
	info, statErr := os.Stat(flagPath)
	if statErr != nil {
		// No flag → caller is the ONLOGON task (or the scheduled-task
		// pump without VBS context) and the user opted out of auto-start.
		return true
	}
	// Always consume the flag — stale or not — so a future auto-launch
	// can't reuse it.
	_ = os.Remove(flagPath)
	return time.Since(info.ModTime()) > userLaunchFlagTTL
}
