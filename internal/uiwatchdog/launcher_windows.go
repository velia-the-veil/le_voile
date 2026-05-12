//go:build windows

package uiwatchdog

import (
	"context"
	"errors"
	"fmt"
	"syscall"
	"unsafe"

	"golang.org/x/sys/windows"
)

// errNoSession is returned when no interactive (WTSActive) user session
// exists on the host. Common in headless servers, before user login,
// during shutdown. Treated as a normal "wait" condition by the watchdog.
var errNoSession = errors.New("uiwatchdog: no interactive session")

// WindowsLauncher launches levoile-ui.exe in the active user session
// from a service running as LocalSystem. Cross-session spawn requires
// CreateProcessAsUser + a primary token obtained via WTSQueryUserToken.
type WindowsLauncher struct {
	binary string
}

// NewWindowsLauncher returns a launcher that spawns binary in the active
// interactive session.
func NewWindowsLauncher(binary string) *WindowsLauncher {
	return &WindowsLauncher{binary: binary}
}

// Available reports whether at least one WTSActive session exists. The
// watchdog polls this when the launcher is unavailable.
func (l *WindowsLauncher) Available() bool {
	sessionID, err := activeSessionID()
	if err != nil || sessionID == 0xFFFFFFFF {
		return false
	}
	return true
}

// Launch spawns the configured binary in the active interactive session.
// The returned channel receives one ProcessExit when the child exits;
// the channel is closed afterwards.
func (l *WindowsLauncher) Launch(ctx context.Context) (<-chan ProcessExit, error) {
	sessionID, err := activeSessionID()
	if err != nil {
		return nil, err
	}
	if sessionID == 0xFFFFFFFF {
		return nil, errNoSession
	}
	hUserToken, err := wtsQueryUserToken(sessionID)
	if err != nil {
		return nil, fmt.Errorf("WTSQueryUserToken: %w", err)
	}
	defer windows.CloseHandle(hUserToken)

	// CreateProcessAsUser needs a primary token; WTSQueryUserToken returns
	// one already, but DuplicateTokenEx is required if we want full
	// TOKEN_ALL_ACCESS for environment block creation. Cheap and safe.
	var hPrimary windows.Token
	err = windows.DuplicateTokenEx(
		windows.Token(hUserToken),
		windows.MAXIMUM_ALLOWED,
		nil,
		windows.SecurityImpersonation,
		windows.TokenPrimary,
		&hPrimary,
	)
	if err != nil {
		return nil, fmt.Errorf("DuplicateTokenEx: %w", err)
	}
	defer hPrimary.Close()

	// Build a per-user environment block so %USERPROFILE%, %APPDATA% etc.
	// resolve to the user's profile (not SYSTEM's). Without this, the UI
	// loads its config from C:\Windows\System32\config\systemprofile.
	var envBlock *uint16
	if err := createEnvironmentBlock(&envBlock, hPrimary, false); err != nil {
		return nil, fmt.Errorf("CreateEnvironmentBlock: %w", err)
	}
	defer destroyEnvironmentBlock(envBlock)

	binaryUTF16, err := windows.UTF16PtrFromString(l.binary)
	if err != nil {
		return nil, fmt.Errorf("binary path utf16: %w", err)
	}
	desktopUTF16, err := windows.UTF16PtrFromString("winsta0\\default")
	if err != nil {
		return nil, fmt.Errorf("desktop utf16: %w", err)
	}

	si := windows.StartupInfo{
		Cb:      uint32(unsafe.Sizeof(windows.StartupInfo{})),
		Desktop: desktopUTF16,
	}
	pi := windows.ProcessInformation{}

	const (
		createUnicodeEnvironment = 0x00000400
		createNoWindow           = 0x08000000
	)

	if err := createProcessAsUser(
		hPrimary,
		binaryUTF16,
		nil, // command line — let the binary path stand
		nil, nil, false,
		createUnicodeEnvironment|createNoWindow,
		unsafe.Pointer(envBlock),
		nil,
		&si,
		&pi,
	); err != nil {
		return nil, fmt.Errorf("CreateProcessAsUser: %w", err)
	}
	// We never use the thread handle.
	windows.CloseHandle(pi.Thread)

	exitCh := make(chan ProcessExit, 1)
	go l.waitForExit(ctx, pi.Process, exitCh)
	return exitCh, nil
}

func (l *WindowsLauncher) waitForExit(ctx context.Context, h windows.Handle, ch chan<- ProcessExit) {
	defer close(ch)
	defer windows.CloseHandle(h)

	// Block on the process handle. WaitForSingleObject is event-driven,
	// no polling. We rely on ctx for shutdown propagation by closing the
	// handle out of band — but the simpler approach is to use a brief
	// timeout loop so ctx cancellation can short-circuit cleanly.
	const pollMs = 500
	for {
		state, err := windows.WaitForSingleObject(h, pollMs)
		switch state {
		case windows.WAIT_OBJECT_0:
			var code uint32
			_ = windows.GetExitCodeProcess(h, &code)
			ch <- ProcessExit{ExitCode: int(int32(code))}
			return
		case uint32(windows.WAIT_TIMEOUT):
			if ctx.Err() != nil {
				// The watchdog is shutting down; surface a synthetic
				// "exit" so the loop can drain. We do NOT terminate the
				// child — the UI is allowed to keep running across a
				// service restart cycle.
				ch <- ProcessExit{ExitCode: -1, Err: ctx.Err()}
				return
			}
			continue
		default:
			ch <- ProcessExit{ExitCode: -1, Err: fmt.Errorf("WaitForSingleObject: state=%d err=%v", state, err)}
			return
		}
	}
}

// NewPlatformLauncher returns a Windows launcher instance.
func NewPlatformLauncher(binaryPath string) ProcessLauncher {
	return NewWindowsLauncher(binaryPath)
}

// --- Win32 syscall plumbing ---------------------------------------------

var (
	modWtsapi32 = windows.NewLazySystemDLL("wtsapi32.dll")
	modAdvapi32 = windows.NewLazySystemDLL("advapi32.dll")
	modUserenv  = windows.NewLazySystemDLL("userenv.dll")

	procWTSGetActiveConsoleSessionID = windows.NewLazySystemDLL("kernel32.dll").NewProc("WTSGetActiveConsoleSessionId")
	procWTSQueryUserToken            = modWtsapi32.NewProc("WTSQueryUserToken")
	procCreateProcessAsUserW         = modAdvapi32.NewProc("CreateProcessAsUserW")
	procCreateEnvironmentBlock       = modUserenv.NewProc("CreateEnvironmentBlock")
	procDestroyEnvironmentBlock      = modUserenv.NewProc("DestroyEnvironmentBlock")
)

// activeSessionID returns the WTSActive session ID currently attached to
// the physical console (which is the interactive desktop on a normal
// workstation). 0xFFFFFFFF means no active session.
func activeSessionID() (uint32, error) {
	r, _, _ := procWTSGetActiveConsoleSessionID.Call()
	return uint32(r), nil
}

func wtsQueryUserToken(sessionID uint32) (windows.Handle, error) {
	var token windows.Handle
	r, _, e := procWTSQueryUserToken.Call(uintptr(sessionID), uintptr(unsafe.Pointer(&token)))
	if r == 0 {
		return 0, e
	}
	return token, nil
}

func createEnvironmentBlock(env **uint16, token windows.Token, inherit bool) error {
	var inh uintptr
	if inherit {
		inh = 1
	}
	r, _, e := procCreateEnvironmentBlock.Call(
		uintptr(unsafe.Pointer(env)),
		uintptr(token),
		inh,
	)
	if r == 0 {
		return e
	}
	return nil
}

func destroyEnvironmentBlock(env *uint16) {
	if env == nil {
		return
	}
	procDestroyEnvironmentBlock.Call(uintptr(unsafe.Pointer(env)))
}

func createProcessAsUser(
	token windows.Token,
	appName *uint16,
	commandLine *uint16,
	procAttr, threadAttr *syscall.SecurityAttributes,
	inheritHandles bool,
	creationFlags uint32,
	env unsafe.Pointer,
	currentDir *uint16,
	si *windows.StartupInfo,
	pi *windows.ProcessInformation,
) error {
	var inh uintptr
	if inheritHandles {
		inh = 1
	}
	r, _, e := procCreateProcessAsUserW.Call(
		uintptr(token),
		uintptr(unsafe.Pointer(appName)),
		uintptr(unsafe.Pointer(commandLine)),
		uintptr(unsafe.Pointer(procAttr)),
		uintptr(unsafe.Pointer(threadAttr)),
		inh,
		uintptr(creationFlags),
		uintptr(env),
		uintptr(unsafe.Pointer(currentDir)),
		uintptr(unsafe.Pointer(si)),
		uintptr(unsafe.Pointer(pi)),
	)
	if r == 0 {
		return e
	}
	return nil
}
