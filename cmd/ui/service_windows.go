//go:build windows

package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/velia-the-veil/le_voile/internal/elevation"
)

// ensureService starts the Windows service when it isn't already running. The
// previous version fire-and-forgot `levoile-service.exe start` and threw the
// error away — so when the UI was launched without elevation (UAC didn't
// trigger) the SCM refused the start and the user got a blank webview that
// eventually fell back to the "Service non démarré" screen. Now we:
//
//  1. Try `sc.exe start LeVoile` first — it's the canonical SCM entry point,
//     already matches the hint shown on the fallback screen, and reports a
//     proper error on access-denied.
//  2. Fall back to kardianos's `levoile-service.exe start` for parity with
//     older installs.
//  3. Retry once after 500 ms so the SCM-is-busy-from-a-prior-stop race
//     (error 1056 "service has been started") resolves without the user
//     noticing.
//  4. If both attempts failed with an access-denied error AND the current
//     process is not elevated, relaunch the UI under UAC. ShellExecute with
//     "runas" shows the standard consent dialog; the elevated instance will
//     own SCM rights and successfully start the service. The non-elevated
//     caller exits so there's no duplicate UI.
//  5. On final failure (already elevated, or UAC declined), drop a one-line
//     hint into %APPDATA%\LeVoile\ui-service-start.log so operators can read
//     it post-mortem. The UI still falls back gracefully to the "service
//     down" screen in parallel, so the log is strictly diagnostic.
func ensureService() {
	if tryStart() {
		return
	}
	time.Sleep(500 * time.Millisecond)
	if tryStart() {
		return
	}
	if isAccessDenied(lastStartErr) && !elevation.IsElevated() {
		if err := elevation.RelaunchElevated(); err == nil {
			os.Exit(0)
		}
		// Fall through on RelaunchElevated failure (UAC declined, ShellExecute
		// error…) — the non-elevated UI still shows the "service down"
		// fallback screen so the user is not left staring at a blank window.
	}
	logStartFailure()
}

// isAccessDenied matches the SCM / kardianos surface area for access-control
// failures. The SCM surfaces English "Access is denied" and localized
// "Accès refusé" / "Accès est refusé"; kardianos wraps the Win32 error so the
// string flavours vary. Matching on the core tokens covers both.
func isAccessDenied(msg string) bool {
	if msg == "" {
		return false
	}
	lower := strings.ToLower(msg)
	return strings.Contains(lower, "access is denied") ||
		strings.Contains(lower, "access denied") ||
		strings.Contains(lower, "accès refusé") ||
		strings.Contains(lower, "acces refuse") ||
		strings.Contains(lower, "refusé") ||
		strings.Contains(lower, "error 5")
}

func tryStart() bool {
	if runSCStart() {
		return true
	}
	return runKardianosStart()
}

func runSCStart() bool {
	cmd := exec.Command("sc.exe", "start", "LeVoile")
	out, err := cmd.CombinedOutput()
	if err == nil {
		lastStartErr = ""
		return true
	}
	// 1056 = "An instance of the service is already running." — that's a success
	// from our perspective: the service is up, nothing more to do.
	if msg := string(out); len(msg) > 0 && (contains(msg, "1056") || contains(msg, "already running")) {
		lastStartErr = ""
		return true
	}
	lastStartErr = fmt.Sprintf("sc start LeVoile: %v: %s", err, string(out))
	return false
}

func runKardianosStart() bool {
	self, err := os.Executable()
	if err != nil {
		return false
	}
	servicePath := filepath.Join(filepath.Dir(self), "levoile-service.exe")
	if _, err := os.Stat(servicePath); err != nil {
		return false
	}
	out, err := exec.Command(servicePath, "start").CombinedOutput()
	if err == nil {
		lastStartErr = ""
		return true
	}
	lastStartErr = fmt.Sprintf("%s start: %v: %s", servicePath, err, string(out))
	return false
}

var lastStartErr string

func logStartFailure() {
	if lastStartErr == "" {
		return
	}
	appdata := os.Getenv("APPDATA")
	if appdata == "" {
		return
	}
	dir := filepath.Join(appdata, "LeVoile")
	_ = os.MkdirAll(dir, 0o755)
	logPath := filepath.Join(dir, "ui-service-start.log")
	f, err := os.OpenFile(logPath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return
	}
	defer f.Close()
	fmt.Fprintf(f, "%s %s\n", time.Now().Format(time.RFC3339), lastStartErr)
}

func contains(s, sub string) bool {
	return len(s) >= len(sub) && (s == sub || indexOf(s, sub) >= 0)
}

func indexOf(s, sub string) int {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return i
		}
	}
	return -1
}
