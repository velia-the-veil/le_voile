//go:build windows

package elevation

import (
	"fmt"
	"os"
	"strings"

	"golang.org/x/sys/windows"
)

// IsElevated returns true if the current process has admin privileges.
func IsElevated() bool {
	token := windows.GetCurrentProcessToken()
	return token.IsElevated()
}

// RelaunchElevated re-launches the current executable with UAC elevation.
func RelaunchElevated() error {
	exe, err := os.Executable()
	if err != nil {
		return fmt.Errorf("elevation: executable path: %w", err)
	}
	cwd, err := os.Getwd()
	if err != nil {
		return fmt.Errorf("elevation: working directory: %w", err)
	}
	verb, err := windows.UTF16PtrFromString("runas")
	if err != nil {
		return fmt.Errorf("elevation: utf16 verb: %w", err)
	}
	exePtr, err := windows.UTF16PtrFromString(exe)
	if err != nil {
		return fmt.Errorf("elevation: utf16 exe: %w", err)
	}
	args, err := windows.UTF16PtrFromString(strings.Join(os.Args[1:], " "))
	if err != nil {
		return fmt.Errorf("elevation: utf16 args: %w", err)
	}
	cwdPtr, err := windows.UTF16PtrFromString(cwd)
	if err != nil {
		return fmt.Errorf("elevation: utf16 cwd: %w", err)
	}
	return windows.ShellExecute(0, verb, exePtr, args, cwdPtr, windows.SW_SHOWNORMAL)
}
