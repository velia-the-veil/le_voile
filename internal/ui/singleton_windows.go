//go:build windows

package ui

import (
	"fmt"

	"golang.org/x/sys/windows"
)

const mutexName = "Global\\LeVoileUI"

var singletonHandle windows.Handle

// AcquireSingleton creates a named Windows mutex to ensure only one UI
// instance runs at a time. Returns an error if another instance is already running.
func AcquireSingleton() error {
	name, err := windows.UTF16PtrFromString(mutexName)
	if err != nil {
		return fmt.Errorf("ui: singleton utf16: %w", err)
	}
	h, err := windows.CreateMutex(nil, false, name)
	if err != nil {
		if h != 0 {
			windows.CloseHandle(h)
		}
		if err == windows.ERROR_ALREADY_EXISTS {
			return fmt.Errorf("ui: another instance is already running")
		}
		return fmt.Errorf("ui: singleton create: %w", err)
	}
	singletonHandle = h
	return nil
}

// ReleaseSingleton releases the named mutex.
func ReleaseSingleton() {
	if singletonHandle != 0 {
		windows.ReleaseMutex(singletonHandle)
		windows.CloseHandle(singletonHandle)
		singletonHandle = 0
	}
}
