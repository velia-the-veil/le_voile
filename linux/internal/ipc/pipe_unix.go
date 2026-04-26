//go:build linux

package ipc

import (
	"fmt"
	"net"
	"os"
	"path/filepath"
	"syscall"
)

// SocketPath is the unix socket path on Linux/macOS.
// Uses /var/run for privileged service, avoids /tmp TOCTOU attacks.
const SocketPath = "/var/run/levoile/levoile.sock"

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
//
// Permissions model :
//   - dir   /run/levoile/         0750  levoile:levoile  (créé par systemd
//     RuntimeDirectory=levoile, repris ici en MkdirAll défensif si le
//     service est lancé hors systemd).
//   - socket /run/levoile/levoile.sock  0660  levoile:levoile.
//
// Le mode 0660 (group rw) est requis parce que sur Linux le service tourne
// en User=levoile et l'UI tourne en utilisateur de bureau — deux UIDs
// différents qui partagent le groupe levoile. Avec 0700, l'UI ne peut pas
// se connecter au socket. La protection contre les attaques TOCTOU venait
// de /tmp ; ici le socket est dans /run qui n'est pas world-writable, donc
// élargir au groupe levoile (membre = utilisateur autorisé) ne réintroduit
// pas la classe d'attaque.
func (pl *platformListener) Listen() (net.Listener, error) {
	// Create the socket directory with restricted permissions.
	socketDir := filepath.Dir(SocketPath)
	if err := os.MkdirAll(socketDir, 0o750); err != nil {
		return nil, fmt.Errorf("ipc: create socket dir: %w", err)
	}

	// Check for symlink before removing — prevent symlink attacks.
	if info, err := os.Lstat(SocketPath); err == nil {
		if info.Mode()&os.ModeSymlink != 0 {
			return nil, fmt.Errorf("ipc: socket path %s is a symlink, refusing to proceed", SocketPath)
		}
		// Only remove stale sockets or regular files, not symlinks.
		if info.Mode()&os.ModeSocket != 0 || info.Mode().IsRegular() {
			os.Remove(SocketPath)
		}
	}

	// Umask 0o007 → bind() crée le socket en 0o660 (rw owner+group, rien
	// pour other). Combiné au Chmod défensif ci-dessous au cas où un umask
	// custom plus restrictif serait actif côté process.
	oldUmask := syscall.Umask(0o007)
	l, err := net.Listen("unix", SocketPath)
	syscall.Umask(oldUmask)
	if err != nil {
		return nil, err
	}

	// Verify socket permissions after creation.
	if err := os.Chmod(SocketPath, 0o660); err != nil {
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
