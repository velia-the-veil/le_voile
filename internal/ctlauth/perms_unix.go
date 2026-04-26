//go:build linux

package ctlauth

import (
	"fmt"
	"os"
)

// writeRestrictedFile writes the token with mode 0640 (owner read/write,
// group read). On Linux the service runs as User=levoile and the UI runs as
// the desktop user — they are different users that share group `levoile`,
// so the token must be group-readable for the UI to authenticate. The token
// path lives in /etc/levoile/ which is itself mode 2770 root:levoile, so
// "other" users still cannot reach it.
//
// The atomic create+write pattern prevents another process from racing in
// between os.Create and the os.Chmod call.
func writeRestrictedFile(path string, data []byte) error {
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o640)
	if err != nil {
		return fmt.Errorf("ctlauth: open: %w", err)
	}
	if _, err := f.Write(data); err != nil {
		f.Close()
		_ = os.Remove(path)
		return fmt.Errorf("ctlauth: write: %w", err)
	}
	if err := f.Close(); err != nil {
		return fmt.Errorf("ctlauth: close: %w", err)
	}
	// Re-Chmod in case the umask removed bits the OpenFile mode requested.
	if err := os.Chmod(path, 0o640); err != nil {
		return fmt.Errorf("ctlauth: chmod: %w", err)
	}
	return nil
}
