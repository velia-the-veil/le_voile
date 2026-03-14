//go:build !windows

package dns

// StopDnscache is a no-op on non-Windows platforms.
func StopDnscache() error { return nil }

// RestartDnscache is a no-op on non-Windows platforms.
func RestartDnscache() error { return nil }
