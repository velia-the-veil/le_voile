package stun

import (
	"net"
	"testing"
	"time"
)

func TestRelayer_DefensiveCopy(t *testing.T) {
	t.Parallel()

	// The relayer should make a defensive copy of the packet buffer so that
	// the caller's buffer is not modified after HandleIntercept returns.
	stunResp := validBindingRequest()
	byteOrder.PutUint16(stunResp[0:2], 0x0101)

	mockTunnel := &mockTunnelRelay{response: stunResp, delay: 50 * time.Millisecond}
	state := &mockStateChecker{}
	state.connected.Store(true)

	relayer := NewRelayer(mockTunnel, state, "stun.l.google.com:19302")

	pkt := validBindingRequest()
	hdr, _ := ParseHeader(pkt)

	// Snapshot original content.
	original := make([]byte, len(pkt))
	copy(original, pkt)

	relayer.HandleIntercept(pkt, &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 12345}, hdr)

	// Immediately overwrite the caller's buffer.
	for i := range pkt {
		pkt[i] = 0xFF
	}

	// Wait for the relay goroutine to read the packet.
	time.Sleep(150 * time.Millisecond)

	// The tunnel should have been called with the original data, not 0xFF.
	if mockTunnel.getCalls() != 1 {
		t.Fatalf("tunnel calls = %d, want 1", mockTunnel.getCalls())
	}
}

func TestRelayer_SemaphoreFull_Drop(t *testing.T) {
	t.Parallel()

	// Fill the semaphore to capacity so the 21st request is silently dropped.
	mockTunnel := &mockTunnelRelay{
		response: validBindingRequest(),
		delay:    200 * time.Millisecond, // hold the semaphore
	}

	state := &mockStateChecker{}
	state.connected.Store(true)

	relayer := NewRelayer(mockTunnel, state, "stun.l.google.com:19302")
	pkt := validBindingRequest()
	hdr, _ := ParseHeader(pkt)

	// Send exactly maxRelayerConcurrent requests to fill the semaphore.
	for i := 0; i < maxRelayerConcurrent; i++ {
		relayer.HandleIntercept(pkt, &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 20000 + i}, hdr)
	}

	// Small sleep to ensure goroutines have been launched and hold the sem.
	time.Sleep(50 * time.Millisecond)

	// The 21st request should be silently dropped (semaphore full).
	relayer.HandleIntercept(pkt, &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 39999}, hdr)

	// Wait for all goroutines to finish.
	time.Sleep(400 * time.Millisecond)

	calls := mockTunnel.getCalls()

	if calls != maxRelayerConcurrent {
		t.Errorf("tunnel calls = %d, want exactly %d (21st should be dropped)", calls, maxRelayerConcurrent)
	}
}

func TestRelayer_EmptyPacket(t *testing.T) {
	t.Parallel()

	// HandleIntercept with an empty packet should not panic.
	mockTunnel := &mockTunnelRelay{response: []byte{}}
	state := &mockStateChecker{}
	state.connected.Store(true)

	relayer := NewRelayer(mockTunnel, state, "stun.l.google.com:19302")

	emptyPkt := []byte{}
	// hdr can be nil since we're testing defensive behavior.
	relayer.HandleIntercept(emptyPkt, &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 12345}, nil)

	// Wait for the goroutine to execute.
	time.Sleep(100 * time.Millisecond)

	// The tunnel should still be called (connected state is true, semaphore is free).
	if mockTunnel.getCalls() != 1 {
		t.Errorf("tunnel calls = %d, want 1 (empty packet should still be relayed)", mockTunnel.getCalls())
	}
}
