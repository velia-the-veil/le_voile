//go:build linux || darwin

package config

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
)

// DefaultPath returns the default configuration file path on Linux/macOS.
func DefaultPath() (string, error) {
	dir, err := os.UserConfigDir()
	if err != nil {
		return "", fmt.Errorf("config: %w", err)
	}
	return filepath.Join(dir, "levoile", "config.toml"), nil
}

// StagingDir returns the staging directory for auto-updates on Unix.
//
// Resolution order:
//  1. LEVOILE_STAGING_DIR env var (explicit override — tests, advanced users)
//  2. Service mode (euid == 0): /var/lib/levoile/updates
//  3. User mode: {os.UserConfigDir}/levoile/updates (typically ~/.config/levoile/updates)
//
// A system service started via kardianos/systemd runs as root and has no $HOME,
// so os.UserConfigDir would fail or return a nonsensical path. The euid==0 check
// routes the service to a deterministic system path.
func StagingDir() (string, error) {
	if override := os.Getenv("LEVOILE_STAGING_DIR"); override != "" {
		return override, nil
	}
	if isServiceMode() {
		return "/var/lib/levoile/updates", nil
	}
	dir, err := os.UserConfigDir()
	if err != nil {
		return "", fmt.Errorf("config: %w", err)
	}
	return filepath.Join(dir, "levoile", "updates"), nil
}

// isServiceMode reports whether the binary is running as a system service.
// On Unix, the system service runs as root (euid 0).
func isServiceMode() bool {
	return os.Geteuid() == 0
}

// ServicePath returns the system-wide configuration path used when the
// service runs as a systemd daemon (Linux only). macOS falls back to
// DefaultPath since macOS is not yet shipped as a LaunchDaemon.
func ServicePath() (string, error) {
	if runtime.GOOS == "linux" {
		return "/etc/levoile/config.toml", nil
	}
	return DefaultPath()
}

// IntegrityKeyPath returns the path to the machine-local HMAC key file.
// Colocated with the config it protects so a backup/restore keeps the pair
// in sync. Service mode writes under /etc/levoile/; user mode under
// ~/.config/levoile/.
func IntegrityKeyPath() (string, error) {
	if runtime.GOOS == "linux" && isServiceMode() {
		return "/etc/levoile/config.integrity.key", nil
	}
	cfg, err := DefaultPath()
	if err != nil {
		return "", err
	}
	return filepath.Join(filepath.Dir(cfg), "config.integrity.key"), nil
}
