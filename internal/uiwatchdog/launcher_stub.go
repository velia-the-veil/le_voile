//go:build !windows

package uiwatchdog

import (
	"context"
	"errors"
)

// errStubLauncher is returned by the stub launcher; tests / callers use
// this to detect that they are running on a platform without a real UI
// supervisor (Linux delegates to systemd user units).
var errStubLauncher = errors.New("uiwatchdog: stub launcher (Linux delegates supervision to systemd --user)")

// StubLauncher is a ProcessLauncher that always reports unavailable. The
// non-Windows service simply never spawns the watchdog (see service.go),
// but the stub keeps the package compilable on every OS and makes the
// behavioural contract obvious to readers.
type StubLauncher struct{}

func (StubLauncher) Launch(context.Context) (<-chan ProcessExit, error) {
	return nil, errStubLauncher
}

func (StubLauncher) Available() bool { return false }

// NewPlatformLauncher returns the platform-appropriate launcher. On
// non-Windows it returns the stub — callers should treat the watchdog as
// disabled in that case.
func NewPlatformLauncher(binaryPath string) ProcessLauncher {
	return StubLauncher{}
}
