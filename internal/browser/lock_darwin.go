//go:build darwin

package browser

import "io"

type noopLock struct{}

func (noopLock) Close() error { return nil }

// acquireLock returns a no-op lock on macOS.
func acquireLock() (io.Closer, error) {
	return noopLock{}, nil
}
