//go:build !windows

package ctlauth

import (
	"fmt"
	"os"
)

// writeRestrictedFile writes the token with mode 0600 (owner read/write only).
// The atomic create+write pattern prevents another process from racing in
// between os.Create and the os.Chmod call.
func writeRestrictedFile(path string, data []byte) error {
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o600)
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
	if err := os.Chmod(path, 0o600); err != nil {
		return fmt.Errorf("ctlauth: chmod: %w", err)
	}
	return nil
}
