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
