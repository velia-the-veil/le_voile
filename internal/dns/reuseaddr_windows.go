//go:build windows

package dns

import (
	"context"
	"net"
	"syscall"

	"golang.org/x/sys/windows"
)

// listenUDPReuseAddr binds a UDP socket with SO_REUSEADDR so that a specific
// address (e.g. 127.0.0.1:53) can coexist with a wildcard bind (0.0.0.0:53)
// held by another service like SharedAccess/ICS.
func listenUDPReuseAddr(network string, addr *net.UDPAddr) (*net.UDPConn, error) {
	lc := net.ListenConfig{
		Control: func(_, _ string, c syscall.RawConn) error {
			return c.Control(func(fd uintptr) {
				windows.SetsockoptInt(windows.Handle(fd), windows.SOL_SOCKET, windows.SO_REUSEADDR, 1)
			})
		},
	}
	conn, err := lc.ListenPacket(context.Background(), network, addr.String())
	if err != nil {
		return nil, err
	}
	return conn.(*net.UDPConn), nil
}
