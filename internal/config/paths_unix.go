//go:build linux || darwin

package config

import (
	"fmt"
	"os"
	"path/filepath"
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
// Path: ~/.config/levoile/updates/
func StagingDir() (string, error) {
	dir, err := os.UserConfigDir()
	if err != nil {
		return "", fmt.Errorf("config: %w", err)
	}
	return filepath.Join(dir, "levoile", "updates"), nil
}
