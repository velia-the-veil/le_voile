package stun

import (
	"context"
	"fmt"
	"net"
	"sync"
	"sync/atomic"
)

const (
	// DefaultPort is the standard STUN UDP port.
	DefaultPort = 3478
	// DefaultTLSPort is the standard STUN-over-DTLS port.
	DefaultTLSPort = 5349
	// maxPacketSize is the maximum UDP packet size (MTU).
	maxPacketSize = 1500
	// maxConcurrent is the maximum number of concurrent packet handlers.
	maxConcurrent = 50
)

// ForwardFunc is called for packets that should pass through transparently
// (non-STUN or STUN non-Binding-Request).
type ForwardFunc func(packet []byte, src *net.UDPAddr)

// InterceptFunc is called for STUN Binding Request packets that need to be
// relayed via the tunnel (implemented in story 5.2).
type InterceptFunc func(packet []byte, src *net.UDPAddr, hdr *Header)

// Interceptor listens on STUN ports and classifies incoming UDP packets.
// STUN Binding Requests are intercepted; everything else is forwarded.
type Interceptor struct {
	port1       int
	port2       int
	onForward   ForwardFunc
	onIntercept InterceptFunc

	conns []*net.UDPConn
	addrs []*net.UDPAddr
	ready chan struct{}

	mu      sync.Mutex
	active  bool
	enabled atomic.Bool
}

// NewInterceptor creates an Interceptor that listens on the given two ports.
// Use port 0 for ephemeral port assignment (useful in tests).
// onForward is called for passthrough packets; onIntercept for Binding Requests.
func NewInterceptor(port1, port2 int, onForward ForwardFunc, onIntercept InterceptFunc) *Interceptor {
	i := &Interceptor{
		port1:       port1,
		port2:       port2,
		onForward:   onForward,
		onIntercept: onIntercept,
		ready:       make(chan struct{}),
	}
	i.enabled.Store(true)
	return i
}

// SetEnabled enables or disables the interceptor. When disabled, all incoming
// packets are dropped immediately without classification (kill switch integration).
func (i *Interceptor) SetEnabled(enabled bool) {
	i.enabled.Store(enabled)
}

// Ready returns a channel closed when both listeners are bound and accepting.
func (i *Interceptor) Ready() <-chan struct{} {
	return i.ready
}

// Active reports whether the interceptor is currently running.
func (i *Interceptor) Active() bool {
	i.mu.Lock()
	defer i.mu.Unlock()
	return i.active
}

// Addrs returns a copy of the resolved listen addresses (available after Ready).
func (i *Interceptor) Addrs() []*net.UDPAddr {
	i.mu.Lock()
	defer i.mu.Unlock()
	cp := make([]*net.UDPAddr, len(i.addrs))
	copy(cp, i.addrs)
	return cp
}

// Start begins listening on both ports and blocks until ctx is cancelled.
// Returns nil on graceful shutdown. Start must not be called more than once;
// create a new Interceptor for each lifecycle.
func (i *Interceptor) Start(ctx context.Context) error {
	i.mu.Lock()
	if i.active {
		i.mu.Unlock()
		return fmt.Errorf("stun: start: interceptor already running")
	}
	i.mu.Unlock()

	ports := []int{i.port1, i.port2}
	var conns []*net.UDPConn
	var addrs []*net.UDPAddr

	for _, port := range ports {
		addr, err := net.ResolveUDPAddr("udp", fmt.Sprintf("127.0.0.1:%d", port))
		if err != nil {
			closeAll(conns)
			return fmt.Errorf("stun: resolve addr port %d: %w", port, err)
		}
		conn, err := net.ListenUDP("udp", addr)
		if err != nil {
			closeAll(conns)
			return fmt.Errorf("stun: listen port %d: %w", port, err)
		}
		conns = append(conns, conn)
		addrs = append(addrs, conn.LocalAddr().(*net.UDPAddr))
	}

	i.mu.Lock()
	i.conns = conns
	i.addrs = addrs
	i.active = true
	i.mu.Unlock()

	close(i.ready)

	// Close connections on context cancel.
	go func() {
		<-ctx.Done()
		closeAll(conns)
	}()

	// Run read loops for each listener.
	var wg sync.WaitGroup
	errCh := make(chan error, len(conns))

	for _, conn := range conns {
		wg.Add(1)
		go func(c *net.UDPConn) {
			defer wg.Done()
			if err := i.readLoop(ctx, c); err != nil {
				errCh <- err
			}
		}(conn)
	}

	wg.Wait()

	i.mu.Lock()
	i.active = false
	i.mu.Unlock()

	select {
	case err := <-errCh:
		return err
	default:
		return nil
	}
}

// readLoop reads UDP packets from conn and dispatches them for classification.
func (i *Interceptor) readLoop(ctx context.Context, conn *net.UDPConn) error {
	sem := make(chan struct{}, maxConcurrent)
	buf := make([]byte, maxPacketSize)
	for {
		n, clientAddr, err := conn.ReadFromUDP(buf)
		if err != nil {
			select {
			case <-ctx.Done():
				return nil
			default:
				return fmt.Errorf("stun: read: %w", err)
			}
		}
		if n == 0 {
			continue
		}

		// Copy packet for handler (buffer will be reused).
		packet := make([]byte, n)
		copy(packet, buf[:n])

		sem <- struct{}{}
		go func(pkt []byte, addr *net.UDPAddr) {
			defer func() { <-sem }()
			i.handlePacket(pkt, addr)
		}(packet, clientAddr)
	}
}

// handlePacket classifies a packet and routes it to the appropriate handler.
//   - Not STUN → forward (passthrough)
//   - STUN but not Binding Request → forward (passthrough)
//   - STUN Binding Request → intercept (queue for tunnel relay)
func (i *Interceptor) handlePacket(packet []byte, src *net.UDPAddr) {
	// Kill switch: drop immediately when disabled.
	if !i.enabled.Load() {
		return
	}

	if !IsSTUN(packet) {
		if i.onForward != nil {
			i.onForward(packet, src)
		}
		return
	}

	if !IsBindingRequest(packet) {
		if i.onForward != nil {
			i.onForward(packet, src)
		}
		return
	}

	// STUN Binding Request — intercept.
	hdr, err := ParseHeader(packet)
	if err != nil {
		// Shouldn't happen since IsSTUN passed, but be safe.
		if i.onForward != nil {
			i.onForward(packet, src)
		}
		return
	}

	if i.onIntercept != nil {
		i.onIntercept(packet, src, hdr)
	}
}

// closeAll closes all UDP connections.
func closeAll(conns []*net.UDPConn) {
	for _, c := range conns {
		if c != nil {
			c.Close()
		}
	}
}
