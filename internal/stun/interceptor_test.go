package stun

import (
	"context"
	"net"
	"sync"
	"testing"
	"time"
)

func TestInterceptor_StartStop(t *testing.T) {
	i := NewInterceptor(0, 0, nil, nil) // port 0 = ephemeral
	ctx, cancel := context.WithCancel(context.Background())

	errCh := make(chan error, 1)
	go func() {
		errCh <- i.Start(ctx)
	}()

	select {
	case <-i.Ready():
	case <-time.After(2 * time.Second):
		t.Fatal("interceptor did not become ready in time")
	}

	if !i.Active() {
		t.Error("expected Active() = true after start")
	}

	cancel()

	select {
	case err := <-errCh:
		if err != nil {
			t.Errorf("Start() returned error: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("interceptor did not stop in time")
	}

	if i.Active() {
		t.Error("expected Active() = false after stop")
	}
}

func TestInterceptor_ReceivePacket(t *testing.T) {
	var mu sync.Mutex
	var forwarded [][]byte
	var intercepted [][]byte

	onForward := func(packet []byte, src *net.UDPAddr) {
		mu.Lock()
		defer mu.Unlock()
		cp := make([]byte, len(packet))
		copy(cp, packet)
		forwarded = append(forwarded, cp)
	}
	onIntercept := func(packet []byte, src *net.UDPAddr, hdr *Header) {
		mu.Lock()
		defer mu.Unlock()
		cp := make([]byte, len(packet))
		copy(cp, packet)
		intercepted = append(intercepted, cp)
	}

	i := NewInterceptor(0, 0, onForward, onIntercept)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	errCh := make(chan error, 1)
	go func() {
		errCh <- i.Start(ctx)
	}()

	select {
	case <-i.Ready():
	case <-time.After(2 * time.Second):
		t.Fatal("interceptor not ready")
	}

	// Get the actual listening address
	addrs := i.Addrs()
	if len(addrs) == 0 {
		t.Fatal("no listening addresses")
	}

	// Send a STUN Binding Request
	stunPkt := validBindingRequest()
	conn, err := net.DialUDP("udp", nil, addrs[0])
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer conn.Close()

	if _, err := conn.Write(stunPkt); err != nil {
		t.Fatalf("write STUN: %v", err)
	}

	// Send a non-STUN packet
	nonSTUN := []byte("hello world, this is not STUN at all")
	if _, err := conn.Write(nonSTUN); err != nil {
		t.Fatalf("write non-STUN: %v", err)
	}

	// Send a STUN non-binding-request (Binding Success Response 0x0101)
	stunNonBinding := validBindingRequest()
	byteOrder.PutUint16(stunNonBinding[0:2], 0x0101)
	if _, err := conn.Write(stunNonBinding); err != nil {
		t.Fatalf("write STUN non-binding: %v", err)
	}

	// Wait for processing
	time.Sleep(200 * time.Millisecond)

	mu.Lock()
	defer mu.Unlock()

	// Non-STUN and STUN-non-binding should be forwarded (2 packets)
	if len(forwarded) != 2 {
		t.Errorf("forwarded = %d packets, want 2", len(forwarded))
	}

	// STUN Binding Request should be intercepted (1 packet)
	if len(intercepted) != 1 {
		t.Errorf("intercepted = %d packets, want 1", len(intercepted))
	}
}

func TestInterceptor_HandlePacket_Classification(t *testing.T) {
	tests := []struct {
		name          string
		packet        []byte
		wantForward   bool
		wantIntercept bool
	}{
		{
			name:          "STUN Binding Request → intercept",
			packet:        validBindingRequest(),
			wantForward:   false,
			wantIntercept: true,
		},
		{
			name:          "non-STUN packet → forward",
			packet:        []byte("not a STUN packet at all!"),
			wantForward:   true,
			wantIntercept: false,
		},
		{
			name: "STUN Binding Success Response → forward",
			packet: func() []byte {
				pkt := validBindingRequest()
				byteOrder.PutUint16(pkt[0:2], 0x0101)
				return pkt
			}(),
			wantForward:   true,
			wantIntercept: false,
		},
		{
			name: "STUN Indication → forward",
			packet: func() []byte {
				pkt := validBindingRequest()
				byteOrder.PutUint16(pkt[0:2], 0x0011)
				return pkt
			}(),
			wantForward:   true,
			wantIntercept: false,
		},
		{
			name: "RTP packet (first 2 bits = 10) → forward",
			packet: func() []byte {
				pkt := make([]byte, 100)
				pkt[0] = 0x80 // RTP
				return pkt
			}(),
			wantForward:   true,
			wantIntercept: false,
		},
		{
			name:          "too short packet → forward",
			packet:        []byte{0x00, 0x01},
			wantForward:   true,
			wantIntercept: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var gotForward, gotIntercept bool

			onForward := func(packet []byte, src *net.UDPAddr) {
				gotForward = true
			}
			onIntercept := func(packet []byte, src *net.UDPAddr, hdr *Header) {
				gotIntercept = true
			}

			i := NewInterceptor(0, 0, onForward, onIntercept)
			i.handlePacket(tt.packet, &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 12345})

			if gotForward != tt.wantForward {
				t.Errorf("forward = %v, want %v", gotForward, tt.wantForward)
			}
			if gotIntercept != tt.wantIntercept {
				t.Errorf("intercept = %v, want %v", gotIntercept, tt.wantIntercept)
			}
		})
	}
}

func TestInterceptor_TURNPassthrough(t *testing.T) {
	var gotForward, gotIntercept bool

	onForward := func(packet []byte, src *net.UDPAddr) {
		gotForward = true
	}
	onIntercept := func(packet []byte, src *net.UDPAddr, hdr *Header) {
		gotIntercept = true
	}

	i := NewInterceptor(0, 0, onForward, onIntercept)

	// Build a TURN Allocate Request packet (type 0x0003, valid STUN framing).
	turnPkt := validBindingRequest()
	byteOrder.PutUint16(turnPkt[0:2], TypeAllocateRequest)

	i.handlePacket(turnPkt, &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 12345})

	if !gotForward {
		t.Error("expected TURN Allocate Request to be forwarded (passthrough), but ForwardFunc was NOT called")
	}
	if gotIntercept {
		t.Error("expected TURN Allocate Request NOT to be intercepted, but InterceptFunc was called")
	}
}

func TestInterceptor_RTPNotTouched(t *testing.T) {
	var gotForward, gotIntercept bool

	onForward := func(packet []byte, src *net.UDPAddr) {
		gotForward = true
	}
	onIntercept := func(packet []byte, src *net.UDPAddr, hdr *Header) {
		gotIntercept = true
	}

	i := NewInterceptor(0, 0, onForward, onIntercept)

	// Build an RTP packet: first 2 bits = 0b10 (version 2).
	rtpPkt := make([]byte, 100)
	rtpPkt[0] = 0x80 // 10000000 — RTP version 2

	i.handlePacket(rtpPkt, &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 12345})

	if !gotForward {
		t.Error("expected RTP packet to be forwarded (passthrough), but ForwardFunc was NOT called")
	}
	if gotIntercept {
		t.Error("expected RTP packet NOT to be intercepted, but InterceptFunc was called")
	}
}

func TestInterceptor_Disabled_DropsAll(t *testing.T) {
	var gotForward, gotIntercept bool

	onForward := func(packet []byte, src *net.UDPAddr) {
		gotForward = true
	}
	onIntercept := func(packet []byte, src *net.UDPAddr, hdr *Header) {
		gotIntercept = true
	}

	i := NewInterceptor(0, 0, onForward, onIntercept)
	i.SetEnabled(false) // Disable — kill switch active

	// Try a STUN Binding Request — should be dropped.
	i.handlePacket(validBindingRequest(), &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 12345})

	// Try a non-STUN packet — should also be dropped.
	i.handlePacket([]byte("not STUN"), &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 12346})

	if gotForward {
		t.Error("expected no forwarding when disabled, but ForwardFunc was called")
	}
	if gotIntercept {
		t.Error("expected no interception when disabled, but InterceptFunc was called")
	}
}

func TestInterceptor_TwoListeners(t *testing.T) {
	i := NewInterceptor(0, 0, nil, nil) // ephemeral ports
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	errCh := make(chan error, 1)
	go func() {
		errCh <- i.Start(ctx)
	}()

	select {
	case <-i.Ready():
	case <-time.After(2 * time.Second):
		t.Fatal("not ready")
	}

	addrs := i.Addrs()
	if len(addrs) != 2 {
		t.Errorf("expected 2 listeners, got %d", len(addrs))
	}

	cancel()
	<-errCh
}

// TestSTUNProxy_TURNFallback simulates a complete ICE-like scenario:
// - A Binding Request is intercepted (srflx candidate = VPS IP)
// - An Allocate Request (TURN) passes through transparently (relay candidate)
// - Only Binding Request is intercepted; TURN is not touched
func TestSTUNProxy_TURNFallback(t *testing.T) {
	var mu sync.Mutex
	var forwarded []uint16   // message types that were forwarded
	var intercepted []uint16 // message types that were intercepted

	onForward := func(packet []byte, src *net.UDPAddr) {
		mu.Lock()
		defer mu.Unlock()
		if len(packet) >= 2 {
			msgType := byteOrder.Uint16(packet[0:2])
			forwarded = append(forwarded, msgType)
		}
	}
	onIntercept := func(packet []byte, src *net.UDPAddr, hdr *Header) {
		mu.Lock()
		defer mu.Unlock()
		intercepted = append(intercepted, hdr.Type)
	}

	i := NewInterceptor(0, 0, onForward, onIntercept)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	errCh := make(chan error, 1)
	go func() {
		errCh <- i.Start(ctx)
	}()

	select {
	case <-i.Ready():
	case <-time.After(2 * time.Second):
		t.Fatal("interceptor not ready")
	}

	addrs := i.Addrs()
	conn, err := net.DialUDP("udp", nil, addrs[0])
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer conn.Close()

	// 1. Send STUN Binding Request — should be intercepted
	bindingReq := validBindingRequest()
	conn.Write(bindingReq)

	// 2. Send TURN Allocate Request — should be forwarded (transparent)
	allocateReq := validBindingRequest()
	byteOrder.PutUint16(allocateReq[0:2], TypeAllocateRequest)
	conn.Write(allocateReq)

	// 3. Send RTP packet — should be forwarded (transparent)
	rtpPkt := make([]byte, 100)
	rtpPkt[0] = 0x80 // RTP version 2
	conn.Write(rtpPkt)

	time.Sleep(200 * time.Millisecond)

	mu.Lock()
	defer mu.Unlock()

	// Binding Request must be intercepted
	if len(intercepted) != 1 || intercepted[0] != TypeBindingRequest {
		t.Errorf("intercepted = %v, want [0x0001 (Binding Request)]", intercepted)
	}

	// Allocate Request must be forwarded (it's STUN framing but TURN type)
	foundAllocate := false
	for _, msgType := range forwarded {
		if msgType == TypeAllocateRequest {
			foundAllocate = true
			break
		}
	}
	if !foundAllocate {
		t.Errorf("Allocate Request (0x0003) not found in forwarded: %v", forwarded)
	}

	// We expect at least 2 forwarded: Allocate + RTP (non-STUN)
	if len(forwarded) < 2 {
		t.Errorf("forwarded = %d packets, want >= 2", len(forwarded))
	}
}
