//go:build linux || darwin

package ipc

import (
	"net"
	"os"
)

// SocketPath is the unix socket path on Linux/macOS.
const SocketPath = "/tmp/levoile.sock"

// platformListener implements Listener for unix sockets.
type platformListener struct {
	listener net.Listener
}

// newPlatformListener creates a unix socket listener.
func newPlatformListener() *platformListener {
	return &platformListener{}
}

// NewPlatformListener creates the platform-appropriate IPC listener.
func NewPlatformListener() Listener {
	return newPlatformListener()
}

// Listen starts listening on the unix socket. Removes stale socket first.
func (pl *platformListener) Listen() (net.Listener, error) {
	// Remove stale socket file if it exists.
	os.Remove(SocketPath)

	l, err := net.Listen("unix", SocketPath)
	if err != nil {
		return nil, err
	}
	pl.listener = l
	return l, nil
}

// Cleanup removes the unix socket file.
// The listener is already closed by the server shutdown goroutine.
func (pl *platformListener) Cleanup() error {
	return os.Remove(SocketPath)
}

// dialPlatform connects to the IPC server via unix socket.
func dialPlatform() (net.Conn, error) {
	return net.Dial("unix", SocketPath)
}
