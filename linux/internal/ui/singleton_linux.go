//go:build linux

package ui

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"syscall"
)

var (
	lockFile *os.File
	// lockPathOverride lets tests redirect the lock location. Empty = default.
	lockPathOverride string
)

// AcquireSingleton takes an exclusive advisory lock on a per-user lock file so
// that only one levoile-ui instance runs for a given Linux user. Returns a
// French user-facing error if another instance already holds the lock.
//
// Linux singleton is per-user (unlike Windows which uses Global\... machine-wide).
// This matches the architecture: Le Voile is a machine-wide VPN, but each user
// may run their own UI instance — they all control the same service.
func AcquireSingleton() error {
	path, err := resolveLockPath()
	if err != nil {
		return fmt.Errorf("ui: singleton path: %w", err)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return fmt.Errorf("ui: singleton mkdir: %w", err)
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return fmt.Errorf("ui: singleton open: %w", err)
	}
	if err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		f.Close()
		if errors.Is(err, syscall.EWOULDBLOCK) {
			return fmt.Errorf("ui: une autre instance Le Voile est déjà active pour cet utilisateur")
		}
		return fmt.Errorf("ui: singleton flock: %w", err)
	}
	lockFile = f
	return nil
}

// ReleaseSingleton releases the per-user lock. Safe to call multiple times.
func ReleaseSingleton() {
	if lockFile == nil {
		return
	}
	syscall.Flock(int(lockFile.Fd()), syscall.LOCK_UN)
	lockFile.Close()
	lockFile = nil
}

// resolveLockPath returns the lock file path, honoring XDG_STATE_HOME when set.
func resolveLockPath() (string, error) {
	if lockPathOverride != "" {
		return lockPathOverride, nil
	}
	if xdg := os.Getenv("XDG_STATE_HOME"); xdg != "" {
		return filepath.Join(xdg, "levoile", "ui.lock"), nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".local", "state", "levoile", "ui.lock"), nil
}
