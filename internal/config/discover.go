package config

import (
	"os"
	"path/filepath"
)

// ExeDir returns the directory of the current executable.
// Variable for test override.
var ExeDir = func() string {
	exe, err := os.Executable()
	if err != nil {
		return ""
	}
	return filepath.Dir(exe)
}

// DiscoverPath determines which config file to use for the installed client.
// Priority: 1) explicit flagPath, 2) config.toml next to executable, 3) user AppData default.
func DiscoverPath(flagPath string) string {
	if flagPath != "" {
		return flagPath
	}
	exeDir := ExeDir()
	if exeDir != "" {
		candidate := filepath.Join(exeDir, "config.toml")
		if _, err := os.Stat(candidate); err == nil {
			return candidate
		}
	}
	p, _ := DefaultPath()
	return p
}

// DiscoverPortablePath determines which config file to use for the portable binary.
// Priority: 1) config.toml next to executable, 2) empty string (use internal defaults).
// No AppData fallback — avoids loading the installed version's config accidentally.
func DiscoverPortablePath() string {
	exeDir := ExeDir()
	if exeDir != "" {
		candidate := filepath.Join(exeDir, "config.toml")
		if _, err := os.Stat(candidate); err == nil {
			return candidate
		}
	}
	return ""
}
