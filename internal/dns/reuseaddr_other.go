//go:build linux

package dns

import "net"

// listenUDPReuseAddr on non-Windows simply calls net.ListenUDP.
func listenUDPReuseAddr(network string, addr *net.UDPAddr) (*net.UDPConn, error) {
	return net.ListenUDP(network, addr)
}
