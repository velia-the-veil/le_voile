//go:build linux || darwin

package ipc

import (
	"fmt"
	"net"
	"os"
	"syscall"
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
// The socket file is created with 0o700 permissions to restrict access to the owner.
func (pl *platformListener) Listen() (net.Listener, error) {
	// Check for symlink before removing — prevent symlink attacks in /tmp.
	if info, err := os.Lstat(SocketPath); err == nil {
		if info.Mode()&os.ModeSymlink != 0 {
			return nil, fmt.Errorf("ipc: socket path %s is a symlink, refusing to proceed", SocketPath)
		}
		// Only remove stale sockets or regular files, not symlinks.
		if info.Mode()&os.ModeSocket != 0 || info.Mode().IsRegular() {
			os.Remove(SocketPath)
		}
	}

	// Set umask to restrict socket permissions at creation time.
	oldUmask := syscall.Umask(0o077)
	l, err := net.Listen("unix", SocketPath)
	syscall.Umask(oldUmask)
	if err != nil {
		return nil, err
	}

	// Verify socket permissions after creation.
	if err := os.Chmod(SocketPath, 0o700); err != nil {
		l.Close()
		os.Remove(SocketPath)
		return nil, fmt.Errorf("ipc: chmod socket: %w", err)
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
