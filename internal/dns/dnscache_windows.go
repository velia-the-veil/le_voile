//go:build windows

package dns

import (
	"fmt"
	"os/exec"
	"strings"
	"sync"
	"time"
)

// port53Services lists Windows services known to bind port 53.
// Each entry tracks whether we stopped it so we can restore on shutdown/crash.
var (
	port53Mu sync.Mutex
	port53Services = []struct {
		name    string
		stopped bool
	}{
		{name: "Dnscache"},
		{name: "SharedAccess"},
	}
)

// StopPort53Services stops Windows services that bind port 53
// (Dnscache, SharedAccess/ICS) to free the port for the local DNS proxy.
func StopDnscache() error {
	port53Mu.Lock()
	defer port53Mu.Unlock()

	var errs []string
	for i := range port53Services {
		svc := &port53Services[i]
		if svc.stopped {
			continue
		}
		out, err := exec.Command("net", "stop", svc.name).CombinedOutput()
		if err != nil {
			// Exit code 2 = service already stopped — that's fine.
			if exitErr, ok := err.(*exec.ExitError); ok && exitErr.ExitCode() == 2 {
				svc.stopped = true
				continue
			}
			// Service may not exist or be uncontrollable — warn but continue.
			errs = append(errs, fmt.Sprintf("%s: %s", svc.name, strings.TrimSpace(string(out))))
			continue
		}
		svc.stopped = true
	}

	// Give Windows time to release the port.
	time.Sleep(300 * time.Millisecond)

	if len(errs) > 0 {
		return fmt.Errorf("dns: stop port-53 services: %s", strings.Join(errs, "; "))
	}
	return nil
}

// RestartPort53Services restarts any port-53 services that were previously
// stopped by StopDnscache. Safe to call multiple times.
func RestartDnscache() error {
	port53Mu.Lock()
	defer port53Mu.Unlock()

	var errs []string
	for i := range port53Services {
		svc := &port53Services[i]
		if !svc.stopped {
			continue
		}
		out, err := exec.Command("net", "start", svc.name).CombinedOutput()
		if err != nil {
			// Exit code 2 = already running — that's fine.
			if exitErr, ok := err.(*exec.ExitError); ok && exitErr.ExitCode() == 2 {
				svc.stopped = false
				continue
			}
			errs = append(errs, fmt.Sprintf("%s: %s", svc.name, strings.TrimSpace(string(out))))
			continue
		}
		svc.stopped = false
	}

	if len(errs) > 0 {
		return fmt.Errorf("dns: restart port-53 services: %s", strings.Join(errs, "; "))
	}
	return nil
}
