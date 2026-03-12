package stun

import (
	"net"
	"sync/atomic"
	"testing"
	"time"
)

// TestRelayer_EnableDisableToggle verifies that disabling and re-enabling
// the relayer correctly resumes STUN relay processing (kill switch cycle).
func TestRelayer_EnableDisableToggle(t *testing.T) {
	t.Parallel()

	stunResp := validBindingRequest()
	byteOrder.PutUint16(stunResp[0:2], 0x0101)

	mockTunnel := &mockTunnelRelay{response: stunResp}
	state := &mockStateChecker{}
	state.connected.Store(true)

	relayer := NewRelayer(mockTunnel, state, "stun.l.google.com:19302")
	pkt := validBindingRequest()
	hdr, _ := ParseHeader(pkt)
	src := &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 12345}

	// Phase 1: Enabled (default) — should relay.
	relayer.HandleIntercept(pkt, src, hdr)
	time.Sleep(100 * time.Millisecond)
	if mockTunnel.getCalls() != 1 {
		t.Errorf("[P0] phase 1: tunnel calls = %d, want 1", mockTunnel.getCalls())
	}

	// Phase 2: Disable (kill switch active) — should drop.
	relayer.SetEnabled(false)
	relayer.HandleIntercept(pkt, src, hdr)
	time.Sleep(100 * time.Millisecond)
	if mockTunnel.getCalls() != 1 {
		t.Errorf("[P0] phase 2: tunnel calls = %d, want 1 (no change)", mockTunnel.getCalls())
	}

	// Phase 3: Re-enable — should relay again.
	relayer.SetEnabled(true)
	relayer.HandleIntercept(pkt, src, hdr)
	time.Sleep(100 * time.Millisecond)
	if mockTunnel.getCalls() != 2 {
		t.Errorf("[P0] phase 3: tunnel calls = %d, want 2", mockTunnel.getCalls())
	}
}

// TestRelayer_Disabled_ConcurrentSetEnabled verifies that concurrent calls
// to SetEnabled and HandleIntercept don't cause data races.
func TestRelayer_Disabled_ConcurrentSetEnabled(t *testing.T) {
	t.Parallel()

	mockTunnel := &mockTunnelRelay{response: validBindingRequest()}
	state := &mockStateChecker{}
	state.connected.Store(true)

	relayer := NewRelayer(mockTunnel, state, "stun.l.google.com:19302")
	pkt := validBindingRequest()
	hdr, _ := ParseHeader(pkt)

	var done atomic.Bool

	// Toggle enabled state rapidly.
	go func() {
		for !done.Load() {
			relayer.SetEnabled(false)
			relayer.SetEnabled(true)
		}
	}()

	// Send requests concurrently.
	for i := 0; i < 50; i++ {
		relayer.HandleIntercept(pkt, &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 30000 + i}, hdr)
	}

	done.Store(true)
	time.Sleep(200 * time.Millisecond)

	// No panic = success. Some calls may have been relayed, some dropped.
	// The exact count depends on timing, but no race condition should occur.
	t.Logf("[P1] concurrent SetEnabled test: tunnel calls = %d (non-deterministic)", mockTunnel.getCalls())
}

// TestRelayer_DisabledAndDisconnected_NoPanic verifies that HandleIntercept
// handles both disabled state AND disconnected tunnel without panic.
func TestRelayer_DisabledAndDisconnected_NoPanic(t *testing.T) {
	t.Parallel()

	mockTunnel := &mockTunnelRelay{response: validBindingRequest()}
	state := &mockStateChecker{}
	state.connected.Store(false) // Disconnected

	relayer := NewRelayer(mockTunnel, state, "stun.l.google.com:19302")
	relayer.SetEnabled(false) // Also disabled

	pkt := validBindingRequest()
	hdr, _ := ParseHeader(pkt)

	// Must not panic — disabled check returns before state check.
	relayer.HandleIntercept(pkt, &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 12345}, hdr)
	time.Sleep(50 * time.Millisecond)

	if mockTunnel.getCalls() != 0 {
		t.Errorf("[P1] tunnel calls = %d, want 0", mockTunnel.getCalls())
	}
}
