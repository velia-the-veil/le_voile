package stun

import (
	"net"
	"sync"
	"testing"
)

// TestInterceptor_EnableDisableToggle verifies that disabling and re-enabling
// the interceptor correctly resumes packet processing (kill switch cycle).
func TestInterceptor_EnableDisableToggle(t *testing.T) {
	t.Parallel()

	var mu sync.Mutex
	var interceptCount int

	onIntercept := func(packet []byte, src *net.UDPAddr, hdr *Header) {
		mu.Lock()
		interceptCount++
		mu.Unlock()
	}

	i := NewInterceptor(0, 0, nil, onIntercept)
	src := &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 9999}
	pkt := validBindingRequest()

	// Phase 1: Enabled (default) — should intercept.
	i.handlePacket(pkt, src)
	mu.Lock()
	if interceptCount != 1 {
		t.Errorf("[P0] phase 1: interceptCount = %d, want 1", interceptCount)
	}
	mu.Unlock()

	// Phase 2: Disable (kill switch active) — should drop.
	i.SetEnabled(false)
	i.handlePacket(pkt, src)
	mu.Lock()
	if interceptCount != 1 {
		t.Errorf("[P0] phase 2: interceptCount = %d, want 1 (no change)", interceptCount)
	}
	mu.Unlock()

	// Phase 3: Re-enable (kill switch deactivated) — should intercept again.
	i.SetEnabled(true)
	i.handlePacket(pkt, src)
	mu.Lock()
	if interceptCount != 2 {
		t.Errorf("[P0] phase 3: interceptCount = %d, want 2", interceptCount)
	}
	mu.Unlock()
}

// TestInterceptor_TURNPassthrough_AllTypes verifies that ALL TURN message types
// are forwarded (not intercepted) when passing through the interceptor.
func TestInterceptor_TURNPassthrough_AllTypes(t *testing.T) {
	t.Parallel()

	turnTypes := []struct {
		name    string
		msgType uint16
	}{
		{"AllocateRequest (0x0003)", TypeAllocateRequest},
		{"CreatePermission (0x0008)", TypeCreatePermission},
		{"ChannelBind (0x0009)", TypeChannelBind},
		{"SendIndication (0x0016)", TypeSendIndication},
		{"DataIndication (0x0017)", TypeDataIndication},
	}

	for _, tt := range turnTypes {
		t.Run(tt.name, func(t *testing.T) {
			var gotForward, gotIntercept bool

			onForward := func(packet []byte, src *net.UDPAddr) {
				gotForward = true
			}
			onIntercept := func(packet []byte, src *net.UDPAddr, hdr *Header) {
				gotIntercept = true
			}

			i := NewInterceptor(0, 0, onForward, onIntercept)
			pkt := validBindingRequest()
			byteOrder.PutUint16(pkt[0:2], tt.msgType)
			src := &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 12345}

			i.handlePacket(pkt, src)

			if !gotForward {
				t.Errorf("[P0] TURN type %s was NOT forwarded (passthrough)", tt.name)
			}
			if gotIntercept {
				t.Errorf("[P0] TURN type %s was intercepted (should NOT be)", tt.name)
			}
		})
	}
}

// TestInterceptor_Disabled_TURNAlsoDropped verifies that when the interceptor
// is disabled (kill switch), even TURN traffic is dropped — no forwarding.
func TestInterceptor_Disabled_TURNAlsoDropped(t *testing.T) {
	t.Parallel()

	var gotForward bool
	onForward := func(packet []byte, src *net.UDPAddr) {
		gotForward = true
	}

	i := NewInterceptor(0, 0, onForward, nil)
	i.SetEnabled(false) // Kill switch active

	// Send a TURN Allocate Request — should be dropped, not forwarded.
	turnPkt := validBindingRequest()
	byteOrder.PutUint16(turnPkt[0:2], TypeAllocateRequest)
	i.handlePacket(turnPkt, &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 12345})

	if gotForward {
		t.Error("[P0] TURN packet was forwarded when disabled — should be dropped by kill switch")
	}
}

// TestInterceptor_Disabled_ForwardAlsoDropped verifies that when disabled,
// non-STUN packets are also dropped (not forwarded).
func TestInterceptor_Disabled_ForwardAlsoDropped(t *testing.T) {
	t.Parallel()

	var gotForward bool
	onForward := func(packet []byte, src *net.UDPAddr) {
		gotForward = true
	}

	i := NewInterceptor(0, 0, onForward, nil)
	i.SetEnabled(false)

	// STUN non-binding (Binding Success Response) — should be dropped.
	stunNonBinding := validBindingRequest()
	byteOrder.PutUint16(stunNonBinding[0:2], 0x0101) // Binding Success Response
	i.handlePacket(stunNonBinding, &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 12345})

	if gotForward {
		t.Error("[P0] STUN non-binding was forwarded when disabled — should be dropped")
	}
}
