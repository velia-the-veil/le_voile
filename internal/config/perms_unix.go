//go:build linux

package config

import (
	"fmt"
	"os"
	"path/filepath"
)

// applyRestrictedPerms ensures file mode 0600 and a parent directory that
// is at minimum non-world-accessible. Idempotent. Invoked after every write
// of config.toml, config.toml.hmac, and config.integrity.key so a
// previously-writable file (legacy install, backup/restore) gets tightened.
//
// Directory perms are only touched when "other" still has access — the goal
// is to lock world out, not to enforce one specific mode. Skipping when
// world is already excluded prevents EPERM in the production deb install :
//   - /etc/levoile/ ships as root:levoile mode 2770 (postinstall step 2),
//     so the service (running as `levoile`, not root) is NOT the owner and
//     a chmod call on the dir would fail with EPERM.
//   - 2770 already has zero "other" perms → security goal met → no chmod
//     needed.
func applyRestrictedPerms(path string) error {
	if err := os.Chmod(path, 0o600); err != nil {
		return fmt.Errorf("config: chmod %s: %w", path, err)
	}
	dir := filepath.Dir(path)
	info, err := os.Stat(dir)
	if err != nil {
		return fmt.Errorf("config: stat dir %s: %w", dir, err)
	}
	if info.Mode().Perm()&0o007 == 0 {
		// "Other" already locked out — security goal met. Don't try to
		// further constrain to 0700 ; would EPERM when the service is not
		// the dir owner (deb install case, root:levoile 2770).
		return nil
	}
	if err := os.Chmod(dir, 0o700); err != nil {
		return fmt.Errorf("config: chmod dir %s: %w", dir, err)
	}
	return nil
}
