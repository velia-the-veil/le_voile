//go:build windows

package ui

import (
	"fmt"
	"os"
	"os/exec"
	"strconv"
)

// Relaunch chain bookkeeping. Used as a last-resort recovery from a frozen
// WebView2 runtime where the boot watchdog fired but openWebview never
// returned (Terminate cannot unblock a thread stuck in SetTitle/Bind/Init
// before Run() — the message pump that processes the quit signal is not
// running yet).
const (
	relaunchEnvVar = "LEVOILE_RELAUNCH_COUNT"
	maxRelaunches  = 3
)

// RelaunchUI spawns a fresh levoile-ui.exe with an incremented relaunch
// counter, releases the singleton mutex so the new process can acquire it,
// and exits the current process. Returns false (without exiting) once the
// chain has hit maxRelaunches — the caller then leaves the still-broken UI
// in place rather than looping forever.
func RelaunchUI() bool {
	count := relaunchCount()
	if count >= maxRelaunches {
		return false
	}
	self, err := os.Executable()
	if err != nil {
		return false
	}
	cmd := exec.Command(self)
	cmd.Env = append(os.Environ(), fmt.Sprintf("%s=%d", relaunchEnvVar, count+1))
	if err := cmd.Start(); err != nil {
		return false
	}
	ReleaseSingleton()
	os.Exit(0)
	return true
}

func relaunchCount() int {
	s := os.Getenv(relaunchEnvVar)
	if s == "" {
		return 0
	}
	n, err := strconv.Atoi(s)
	if err != nil || n < 0 {
		return 0
	}
	return n
}
