//go:build windows

package config

import (
	"os"
	"path/filepath"
	"runtime"
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
// Priority:
//  1. explicit flagPath,
//  2. config.toml next to executable (portable/dev layout),
//  3. system-wide ServicePath if it exists (Linux /etc/levoile/config.toml
//     for the systemd daemon; no-op on Windows where ServicePath==DefaultPath),
//  4. user DefaultPath (fallback).
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
	if sp, err := ServicePath(); err == nil && sp != "" {
		if _, err := os.Stat(sp); err == nil {
			return sp
		}
	}
	p, _ := DefaultPath()
	return p
}

// DiscoverPortablePath determines which config file to use for the portable binary.
// Priority: 1) config.toml next to executable, 2) empty string (use internal defaults).
// No AppData fallback — avoids loading the installed version's config accidentally.
// On Unix, rejects world-writable config files to prevent local privilege escalation.
func DiscoverPortablePath() string {
	exeDir := ExeDir()
	if exeDir != "" {
		candidate := filepath.Join(exeDir, "config.toml")
		info, err := os.Stat(candidate)
		if err == nil {
			// Reject world-writable config on Unix to prevent config injection
			// when the portable binary runs from a shared directory.
			if runtime.GOOS != "windows" && info.Mode()&0o002 != 0 {
				return ""
			}
			return candidate
		}
	}
	return ""
}
