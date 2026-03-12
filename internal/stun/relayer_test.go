package stun

import (
	"context"
	"errors"
	"net"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// mockTunnelRelay is a mock implementation of TunnelRelay for testing.
type mockTunnelRelay struct {
	mu       sync.Mutex
	response []byte
	err      error
	calls    int
	delay    time.Duration
}

func (m *mockTunnelRelay) SendSTUNRelay(ctx context.Context, stunPacket []byte, targetAddr string) ([]byte, error) {
	if m.delay > 0 {
		time.Sleep(m.delay)
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	m.calls++
	if m.err != nil {
		return nil, m.err
	}
	resp := make([]byte, len(m.response))
	copy(resp, m.response)
	return resp, nil
}

func (m *mockTunnelRelay) getCalls() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.calls
}

// mockStateChecker is a mock implementation of TunnelStateChecker.
type mockStateChecker struct {
	connected atomic.Bool
}

func (m *mockStateChecker) IsConnected() bool {
	return m.connected.Load()
}

func TestRelayer_HandleIntercept(t *testing.T) {
	// Start a UDP listener to receive the relayed response.
	listenAddr, err := net.ResolveUDPAddr("udp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	udpConn, err := net.ListenUDP("udp", listenAddr)
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	defer udpConn.Close()

	srcAddr := udpConn.LocalAddr().(*net.UDPAddr)

	// Build a mock STUN Binding Success Response.
	stunResp := validBindingRequest()
	byteOrder.PutUint16(stunResp[0:2], 0x0101) // Binding Success Response

	mockTunnel := &mockTunnelRelay{response: stunResp}
	state := &mockStateChecker{}
	state.connected.Store(true)

	relayer := NewRelayer(mockTunnel, state, "stun.l.google.com:19302")
	stunPkt := validBindingRequest()
	hdr, _ := ParseHeader(stunPkt)

	relayer.HandleIntercept(stunPkt, srcAddr, hdr)

	// Wait for the response from the relayer.
	udpConn.SetReadDeadline(time.Now().Add(2 * time.Second))
	buf := make([]byte, 1500)
	n, _, err := udpConn.ReadFromUDP(buf)
	if err != nil {
		t.Fatalf("read response: %v", err)
	}

	if n != len(stunResp) {
		t.Errorf("response length = %d, want %d", n, len(stunResp))
	}

	// Verify it's a Binding Success Response.
	respType := byteOrder.Uint16(buf[0:2])
	if respType != 0x0101 {
		t.Errorf("response type = 0x%04X, want 0x0101", respType)
	}

	if mockTunnel.getCalls() != 1 {
		t.Errorf("tunnel calls = %d, want 1", mockTunnel.getCalls())
	}
}

func TestRelayer_HandleIntercept_TunnelDisconnected(t *testing.T) {
	mockTunnel := &mockTunnelRelay{response: validBindingRequest()}
	state := &mockStateChecker{}
	state.connected.Store(false) // Tunnel disconnected

	relayer := NewRelayer(mockTunnel, state, "stun.l.google.com:19302")
	stunPkt := validBindingRequest()
	hdr, _ := ParseHeader(stunPkt)

	relayer.HandleIntercept(stunPkt, &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 12345}, hdr)

	// Give time for any goroutine to fire (should not).
	time.Sleep(100 * time.Millisecond)

	if mockTunnel.getCalls() != 0 {
		t.Errorf("tunnel calls = %d, want 0 (should not relay when disconnected)", mockTunnel.getCalls())
	}
}

func TestRelayer_HandleIntercept_TunnelError(t *testing.T) {
	mockTunnel := &mockTunnelRelay{err: errors.New("tunnel error")}
	state := &mockStateChecker{}
	state.connected.Store(true)

	relayer := NewRelayer(mockTunnel, state, "stun.l.google.com:19302")
	stunPkt := validBindingRequest()
	hdr, _ := ParseHeader(stunPkt)

	// Should not panic — errors are silently dropped.
	relayer.HandleIntercept(stunPkt, &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 12345}, hdr)

	time.Sleep(100 * time.Millisecond)

	if mockTunnel.getCalls() != 1 {
		t.Errorf("tunnel calls = %d, want 1", mockTunnel.getCalls())
	}
}

func TestRelayer_Disabled_DropsAll(t *testing.T) {
	mockTunnel := &mockTunnelRelay{response: validBindingRequest()}
	state := &mockStateChecker{}
	state.connected.Store(true)

	relayer := NewRelayer(mockTunnel, state, "stun.l.google.com:19302")
	relayer.SetEnabled(false) // Disable — kill switch active

	stunPkt := validBindingRequest()
	hdr, _ := ParseHeader(stunPkt)

	relayer.HandleIntercept(stunPkt, &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 12345}, hdr)

	// Give time for any goroutine to fire (should not).
	time.Sleep(100 * time.Millisecond)

	if mockTunnel.getCalls() != 0 {
		t.Errorf("tunnel calls = %d, want 0 (should drop all when disabled)", mockTunnel.getCalls())
	}
}

func TestRelayer_HandleIntercept_Concurrency(t *testing.T) {
	mockTunnel := &mockTunnelRelay{
		response: validBindingRequest(),
		delay:    50 * time.Millisecond,
	}
	state := &mockStateChecker{}
	state.connected.Store(true)

	relayer := NewRelayer(mockTunnel, state, "stun.l.google.com:19302")
	stunPkt := validBindingRequest()
	hdr, _ := ParseHeader(stunPkt)

	// Send 25 requests — 20 should be accepted, 5 dropped.
	for i := 0; i < 25; i++ {
		relayer.HandleIntercept(stunPkt, &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 12345 + i}, hdr)
	}

	// Wait for all goroutines to complete.
	time.Sleep(200 * time.Millisecond)

	calls := mockTunnel.getCalls()
	if calls != maxRelayerConcurrent {
		t.Errorf("tunnel calls = %d, want exactly %d (semaphore should limit concurrency)", calls, maxRelayerConcurrent)
	}
}
