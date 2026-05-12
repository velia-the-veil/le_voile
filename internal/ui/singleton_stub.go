//go:build !windows && !linux

package ui

// AcquireSingleton is a no-op on platforms where singleton enforcement is not
// yet implemented (darwin, freebsd, etc.).
func AcquireSingleton() error { return nil }

// ReleaseSingleton is a no-op on platforms where singleton enforcement is not
// yet implemented.
func ReleaseSingleton() {}
