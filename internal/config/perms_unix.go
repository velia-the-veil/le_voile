//go:build linux || darwin

package config

import (
	"fmt"
	"os"
	"path/filepath"
)

// applyRestrictedPerms ensures file mode 0600 and parent directory mode 0700
// on the given path. Idempotent. Invoked after every write of config.toml,
// config.toml.hmac, and config.integrity.key so a previously-writable file
// (legacy install, backup/restore) gets tightened.
//
// Directory perms are only touched when they differ from the target, so
// scenarios where the parent dir is pre-created by packaging (e.g.
// /etc/levoile/ owned by `levoile:levoile` 0700 via nfpm scripts) don't
// trip EPERM when a non-owner Save path runs through the same helper.
func applyRestrictedPerms(path string) error {
	if err := os.Chmod(path, 0o600); err != nil {
		return fmt.Errorf("config: chmod %s: %w", path, err)
	}
	dir := filepath.Dir(path)
	info, err := os.Stat(dir)
	if err != nil {
		return fmt.Errorf("config: stat dir %s: %w", dir, err)
	}
	if info.Mode().Perm() == 0o700 {
		// Already tight — skip chmod so packaging-owned dirs don't reject
		// our Save calls on the tighten step.
		return nil
	}
	if err := os.Chmod(dir, 0o700); err != nil {
		return fmt.Errorf("config: chmod dir %s: %w", dir, err)
	}
	return nil
}
