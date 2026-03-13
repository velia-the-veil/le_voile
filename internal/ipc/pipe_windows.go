//go:build windows

package ipc

import (
	"net"

	"github.com/Microsoft/go-winio"
)

// PipeName is the named pipe path on Windows.
const PipeName = `\\.\pipe\levoile`

// platformListener implements Listener for Windows named pipes.
type platformListener struct {
	listener net.Listener
}

// newPlatformListener creates a Windows named pipe listener.
func newPlatformListener() *platformListener {
	return &platformListener{}
}

// NewPlatformListener creates the platform-appropriate IPC listener.
func NewPlatformListener() Listener {
	return newPlatformListener()
}

// Listen starts listening on the Windows named pipe.
// The pipe is restricted to SYSTEM and the built-in Administrators group.
func (pl *platformListener) Listen() (net.Listener, error) {
	cfg := &winio.PipeConfig{
		// D:P — DACL (protected, no inheritance)
		// A — Allow; GA — Generic All; GR — Generic Read; GW — Generic Write
		// BA = Built-in Administrators; SY = Local System; IU = Interactive Users
		// IU gets read+write only (enough for IPC commands), not full control.
		SecurityDescriptor: "D:P(A;;GA;;;BA)(A;;GA;;;SY)(A;;GRGW;;;IU)",
	}
	l, err := winio.ListenPipe(PipeName, cfg)
	if err != nil {
		return nil, err
	}
	pl.listener = l
	return l, nil
}

// Cleanup is a no-op on Windows. Named pipes don't leave filesystem artifacts
// and the listener is already closed by the server shutdown goroutine.
func (pl *platformListener) Cleanup() error {
	return nil
}

// dialPlatform connects to the IPC server via Windows named pipe.
func dialPlatform() (net.Conn, error) {
	return winio.DialPipe(PipeName, nil)
}
