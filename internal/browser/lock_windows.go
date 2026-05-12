//go:build windows

package browser

import (
	"fmt"
	"io"
	"os"
	"path/filepath"

	"golang.org/x/sys/windows"
)

const lockFileName = "browser-policies.lock"

type windowsLock struct {
	file *os.File
}

func (l *windowsLock) Close() error {
	if l.file == nil {
		return nil
	}
	err := l.file.Close()
	l.file = nil
	return err
}

// acquireLock acquires an advisory lock using LockFileEx.
func acquireLock() (io.Closer, error) {
	dir := policyStatePath()
	if err := os.MkdirAll(dir, 0755); err != nil {
		return nil, fmt.Errorf("browser: lock: mkdir: %w", err)
	}

	lockPath := filepath.Join(dir, lockFileName)
	f, err := os.OpenFile(lockPath, os.O_CREATE|os.O_RDWR, 0644)
	if err != nil {
		return nil, fmt.Errorf("browser: lock: open: %w", err)
	}

	// LOCKFILE_EXCLUSIVE_LOCK | LOCKFILE_FAIL_IMMEDIATELY
	const (
		lockfileExclusiveLock   = 0x00000002
		lockfileFailImmediately = 0x00000001
	)

	ol := new(windows.Overlapped)
	err = windows.LockFileEx(
		windows.Handle(f.Fd()),
		lockfileExclusiveLock|lockfileFailImmediately,
		0, // reserved
		1, // nNumberOfBytesToLockLow
		0, // nNumberOfBytesToLockHigh
		ol,
	)
	if err != nil {
		f.Close()
		return nil, fmt.Errorf("browser: lock: acquire: %w", err)
	}

	return &windowsLock{file: f}, nil
}
