//go:build !windows

package ui

// AcquireSingleton is a no-op on non-Windows platforms.
func AcquireSingleton() error { return nil }

// ReleaseSingleton is a no-op on non-Windows platforms.
func ReleaseSingleton() {}
