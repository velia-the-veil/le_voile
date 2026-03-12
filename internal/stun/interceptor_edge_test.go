package stun

import (
	"context"
	"net"
	"testing"
	"time"
)

func TestInterceptor_DoubleStart(t *testing.T) {
	t.Parallel()

	i := NewInterceptor(0, 0, nil, nil)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	errCh := make(chan error, 1)
	go func() {
		errCh <- i.Start(ctx)
	}()

	select {
	case <-i.Ready():
	case <-time.After(2 * time.Second):
		t.Fatal("interceptor did not become ready in time")
	}

	// Second Start should return an error immediately.
	err := i.Start(ctx)
	if err == nil {
		t.Fatal("expected error from second Start(), got nil")
	}

	const want = "interceptor already running"
	if got := err.Error(); got != "stun: start: "+want {
		t.Errorf("error = %q, want it to contain %q", got, want)
	}

	cancel()
	select {
	case err := <-errCh:
		if err != nil {
			t.Errorf("first Start() returned error: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("interceptor did not stop in time")
	}
}

func TestInterceptor_NilCallbacks_BindingRequest(t *testing.T) {
	t.Parallel()

	// NewInterceptor with nil callbacks should not panic when handling a STUN
	// Binding Request (which would normally call onIntercept).
	i := NewInterceptor(0, 0, nil, nil)
	pkt := validBindingRequest()
	src := &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 9999}

	// Must not panic.
	i.handlePacket(pkt, src)
}

func TestInterceptor_NilCallbacks_NonSTUN(t *testing.T) {
	t.Parallel()

	// NewInterceptor with nil callbacks should not panic when handling a
	// non-STUN packet (which would normally call onForward).
	i := NewInterceptor(0, 0, nil, nil)
	pkt := []byte("this is definitely not a STUN packet")
	src := &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 9999}

	// Must not panic.
	i.handlePacket(pkt, src)
}

func TestInterceptor_Addrs_TwoDistinct(t *testing.T) {
	t.Parallel()

	i := NewInterceptor(0, 0, nil, nil)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	errCh := make(chan error, 1)
	go func() {
		errCh <- i.Start(ctx)
	}()

	select {
	case <-i.Ready():
	case <-time.After(2 * time.Second):
		t.Fatal("interceptor did not become ready in time")
	}

	addrs := i.Addrs()
	if len(addrs) != 2 {
		t.Fatalf("Addrs() returned %d addresses, want 2", len(addrs))
	}

	// Both addresses should be distinct (different ports since we use ephemeral).
	if addrs[0].Port == addrs[1].Port {
		t.Errorf("expected two distinct ports, both are %d", addrs[0].Port)
	}

	// Verify the returned slice is a copy (modifying it should not affect the
	// interceptor's internal state).
	addrs[0] = nil
	addrs2 := i.Addrs()
	if addrs2[0] == nil {
		t.Error("Addrs() did not return a defensive copy")
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
}
