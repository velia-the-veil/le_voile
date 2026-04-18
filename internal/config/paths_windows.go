//go:build windows

package config

import (
	"fmt"
	"os"
	"path/filepath"
)

// DefaultPath returns the default configuration file path on Windows.
func DefaultPath() (string, error) {
	dir, err := os.UserConfigDir()
	if err != nil {
		return "", fmt.Errorf("config: %w", err)
	}
	return filepath.Join(dir, "LeVoile", "config.toml"), nil
}

// StagingDir returns the staging directory for auto-updates on Windows.
// Path: %AppData%/LeVoile/updates/
func StagingDir() (string, error) {
	dir, err := os.UserConfigDir()
	if err != nil {
		return "", fmt.Errorf("config: %w", err)
	}
	return filepath.Join(dir, "LeVoile", "updates"), nil
}

// ServicePath mirrors DefaultPath on Windows: the service runs as
// LocalSystem but the config lives in the installing user's %AppData%
// and is updated via IPC. There is no separate system-wide location.
func ServicePath() (string, error) {
	return DefaultPath()
}

// IntegrityKeyPath returns the path to the machine-local HMAC key file,
// colocated with config.toml under %AppData%\LeVoile\.
func IntegrityKeyPath() (string, error) {
	cfg, err := DefaultPath()
	if err != nil {
		return "", err
	}
	return filepath.Join(filepath.Dir(cfg), "config.integrity.key"), nil
}
