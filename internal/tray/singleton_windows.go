//go:build windows

package tray

import (
	"fmt"

	"golang.org/x/sys/windows"
)

const mutexName = "Global\\LeVoileTray"

// singletonHandle holds the Windows mutex handle so it can be released on exit.
var singletonHandle windows.Handle

// AcquireSingleton creates a named Windows mutex to ensure only one tray
// instance runs at a time. Returns an error if another instance is already
// running. Call ReleaseSingleton in onExit.
func AcquireSingleton() error {
	name, err := windows.UTF16PtrFromString(mutexName)
	if err != nil {
		return fmt.Errorf("tray: singleton utf16: %w", err)
	}
	h, err := windows.CreateMutex(nil, false, name)
	if err != nil {
		// CreateMutex returns a valid handle AND ERROR_ALREADY_EXISTS when
		// the mutex already exists. Close the handle to avoid leaking it.
		if h != 0 {
			windows.CloseHandle(h)
		}
		if err == windows.ERROR_ALREADY_EXISTS {
			return fmt.Errorf("tray: another instance is already running")
		}
		return fmt.Errorf("tray: singleton create: %w", err)
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
