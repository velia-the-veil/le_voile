//go:build linux

package browser

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"syscall"
)

type linuxLock struct {
	file *os.File
}

func (l *linuxLock) Close() error {
	if l.file == nil {
		return nil
	}
	// Unlock then close.
	syscall.Flock(int(l.file.Fd()), syscall.LOCK_UN)
	err := l.file.Close()
	l.file = nil
	return err
}

// acquireLock acquires an advisory lock using flock.
func acquireLock() (io.Closer, error) {
	dir := policyStatePath()
	if err := os.MkdirAll(dir, 0755); err != nil {
		return nil, fmt.Errorf("browser: lock: mkdir: %w", err)
	}

	lockPath := filepath.Join(dir, "browser-policies.lock")
	f, err := os.OpenFile(lockPath, os.O_CREATE|os.O_RDWR, 0644)
	if err != nil {
		return nil, fmt.Errorf("browser: lock: open: %w", err)
	}

	if err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		f.Close()
		return nil, fmt.Errorf("browser: lock: acquire: %w", err)
	}

	return &linuxLock{file: f}, nil
}
