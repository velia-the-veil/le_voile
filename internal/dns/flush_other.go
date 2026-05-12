//go:build !linux && !windows

package dns

import "context"

// Flush is a no-op on unsupported platforms (darwin, etc.).
func Flush(_ context.Context) error {
	return nil
}
