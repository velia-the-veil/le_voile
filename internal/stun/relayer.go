package stun

import (
	"context"
	"net"
	"sync/atomic"
	"time"
)

const (
	// relayTimeout is the maximum time for a single STUN relay operation.
	relayTimeout = 10 * time.Second
)

const (
	// maxRelayerConcurrent is the maximum number of concurrent STUN relays.
	maxRelayerConcurrent = 20
)

// TunnelRelay is the interface for sending STUN packets through the tunnel.
type TunnelRelay interface {
	SendSTUNRelay(ctx context.Context, stunPacket []byte, targetAddr string) ([]byte, error)
}

// TunnelStateChecker checks whether the tunnel is connected.
type TunnelStateChecker interface {
	IsConnected() bool
}

// Relayer relays intercepted STUN Binding Requests through the tunnel.
type Relayer struct {
	tunnel        TunnelRelay
	stateChecker  TunnelStateChecker
	defaultTarget string
	sem           chan struct{}
	enabled       atomic.Bool
}

// NewRelayer creates a Relayer that sends STUN packets through the tunnel.
func NewRelayer(tunnel TunnelRelay, stateChecker TunnelStateChecker, defaultTarget string) *Relayer {
	r := &Relayer{
		tunnel:        tunnel,
		stateChecker:  stateChecker,
		defaultTarget: defaultTarget,
		sem:           make(chan struct{}, maxRelayerConcurrent),
	}
	r.enabled.Store(true)
	return r
}

// SetEnabled enables or disables the relayer. When disabled, all packets
// are dropped immediately without any processing (kill switch integration).
func (r *Relayer) SetEnabled(enabled bool) {
	r.enabled.Store(enabled)
}

// HandleIntercept implements InterceptFunc. It relays a STUN Binding Request
// through the tunnel and sends the response back to the original client.
func (r *Relayer) HandleIntercept(packet []byte, src *net.UDPAddr, hdr *Header) {
	// Kill switch: drop immediately when disabled.
	if !r.enabled.Load() {
		return
	}

	// Check tunnel state before relaying.
	if !r.stateChecker.IsConnected() {
		return // drop silently
	}

	// Defensive copy of packet (buffer may be reused by caller).
	pkt := make([]byte, len(packet))
	copy(pkt, packet)

	// Copy src address for goroutine safety.
	srcCopy := &net.UDPAddr{
		IP:   make(net.IP, len(src.IP)),
		Port: src.Port,
		Zone: src.Zone,
	}
	copy(srcCopy.IP, src.IP)

	// Acquire semaphore (non-blocking — drop if at capacity).
	select {
	case r.sem <- struct{}{}:
	default:
		return // drop silently — at capacity
	}

	go func() {
		defer func() { <-r.sem }()
		r.relay(pkt, srcCopy)
	}()
}

func (r *Relayer) relay(packet []byte, src *net.UDPAddr) {
	ctx, cancel := context.WithTimeout(context.Background(), relayTimeout)
	defer cancel()

	resp, err := r.tunnel.SendSTUNRelay(ctx, packet, r.defaultTarget)
	if err != nil {
		return // drop silently
	}

	// Send response back to original client via UDP.
	conn, err := net.DialUDP("udp", nil, src)
	if err != nil {
		return // drop silently
	}
	defer conn.Close()

	conn.Write(resp)
}
