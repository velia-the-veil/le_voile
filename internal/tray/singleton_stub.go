//go:build !windows

package tray

// AcquireSingleton is a no-op on non-Windows platforms.
func AcquireSingleton() error { return nil }

// ReleaseSingleton is a no-op on non-Windows platforms.
func ReleaseSingleton() {}
