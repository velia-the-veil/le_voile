//go:build linux

package elevation

import (
	"fmt"
	"os"
)

// IsElevated returns true if running as root (UID 0).
func IsElevated() bool {
	return os.Getuid() == 0
}

// RelaunchElevated is a no-op on Unix. Returns an error instructing to run with sudo.
func RelaunchElevated() error {
	return fmt.Errorf("elevation: run with sudo")
}
