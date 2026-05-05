// R-T8 (2026-05-05) — Tests for the heartbeat /health probe.
//
// Smoke tests focus on :
//   - pingHealth() returns nil when relay /health responds 200.
//   - pingHealth() returns error when relay is unreachable.
//   - startHeartbeat / stopHeartbeat are idempotent.
//
// The full goroutine lifecycle (timing-dependent : 5s ticks * 2 fails =
// ~15s minimum to flip state) is NOT exercised here — too slow for unit
// tests, validated end-to-end on Android device. We keep the heartbeat
// constants un-mocked so production behaviour is what the test verifies.

package tunnel

import (
	"context"
	"testing"
	"time"

	lecrypto "github.com/velia-the-veil/le_voile/internal/crypto"
)

func TestClient_pingHealth_ConnectedRelay_ReturnsNil(t *testing.T) {
	pub, priv, err := lecrypto.GenerateKeyPair()
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}

	addr, cleanup := startTestRelay(t, priv)
	defer cleanup()

	pubB64 := lecrypto.ExportPublicKeyBase64(pub)
	client := newTestClient(t, addr, pubB64)
	defer client.Disconnect()

	if err := client.Connect(context.Background()); err != nil {
		t.Fatalf("Connect: %v", err)
	}

	// /health is served by the test relay (cf. startTestRelay) so this
	// must succeed.
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := client.pingHealth(ctx); err != nil {
		t.Fatalf("pingHealth: %v", err)
	}
}

func TestClient_pingHealth_UnreachableRelay_ReturnsError(t *testing.T) {
	pub, _, err := lecrypto.GenerateKeyPair()
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}

	pubB64 := lecrypto.ExportPublicKeyBase64(pub)
	// 192.0.2.x is TEST-NET-1 (RFC 5737) — guaranteed unreachable.
	client := newTestClient(t, "192.0.2.1:443", pubB64)

	// Don't Connect — pingHealth should fail at the transport level
	// (no QUIC handshake possible to a black-hole address).
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	if err := client.pingHealth(ctx); err == nil {
		t.Fatal("expected error from pingHealth on unreachable relay, got nil")
	}
}

func TestClient_StartStopHeartbeat_Idempotent(t *testing.T) {
	c := &Client{state: NewStateManager()}

	// startHeartbeat called twice should NOT spawn two goroutines —
	// the second call returns silently because heartbeatStop is non-nil.
	c.startHeartbeat(context.Background())

	c.heartbeatMu.Lock()
	stop1 := c.heartbeatStop
	c.heartbeatMu.Unlock()
	if stop1 == nil {
		t.Fatal("startHeartbeat #1 did not register a stop channel")
	}

	c.startHeartbeat(context.Background())

	c.heartbeatMu.Lock()
	stop2 := c.heartbeatStop
	c.heartbeatMu.Unlock()
	if stop2 != stop1 {
		t.Errorf("startHeartbeat #2 replaced the stop channel — not idempotent")
	}

	// stopHeartbeat closes the channel and clears the field.
	c.stopHeartbeat()

	c.heartbeatMu.Lock()
	stop3 := c.heartbeatStop
	c.heartbeatMu.Unlock()
	if stop3 != nil {
		t.Errorf("stopHeartbeat did not clear heartbeatStop, still %v", stop3)
	}

	// Calling stopHeartbeat again is a no-op (must not panic on close
	// of nil channel).
	c.stopHeartbeat()

	// And we can start a fresh one afterwards.
	c.startHeartbeat(context.Background())
	defer c.stopHeartbeat()

	c.heartbeatMu.Lock()
	stop4 := c.heartbeatStop
	c.heartbeatMu.Unlock()
	if stop4 == nil {
		t.Error("startHeartbeat after stop failed to register a new channel")
	}
	if stop4 == stop1 {
		t.Error("startHeartbeat after stop reused the closed channel")
	}
}

func TestClient_HeartbeatLoop_NotConnected_NoStateChange(t *testing.T) {
	c := &Client{state: NewStateManager()}
	// state defaults to Disconnected — heartbeat must not flip anything
	// when state != StateConnected (skip branch in heartbeatLoop).

	stop := make(chan struct{})

	// Run the loop briefly in a goroutine, then stop it. We tolerate that
	// no /health probe is even attempted (state guard short-circuits).
	done := make(chan struct{})
	go func() {
		c.heartbeatLoop(context.Background(), stop)
		close(done)
	}()

	// Let the loop run for a bit (less than heartbeatInterval=5s) — no
	// tick fires, nothing happens.
	time.Sleep(100 * time.Millisecond)

	close(stop)

	select {
	case <-done:
		// loop exited cleanly
	case <-time.After(2 * time.Second):
		t.Fatal("heartbeatLoop did not exit after stop closed")
	}

	if c.state.Get() != StateDisconnected {
		t.Errorf("state changed unexpectedly to %q", c.state.Get())
	}
}

func TestClient_Connect_StartsHeartbeat(t *testing.T) {
	pub, priv, err := lecrypto.GenerateKeyPair()
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}

	addr, cleanup := startTestRelay(t, priv)
	defer cleanup()

	pubB64 := lecrypto.ExportPublicKeyBase64(pub)
	client := newTestClient(t, addr, pubB64)
	defer client.Disconnect()

	if err := client.Connect(context.Background()); err != nil {
		t.Fatalf("Connect: %v", err)
	}

	client.heartbeatMu.Lock()
	stop := client.heartbeatStop
	client.heartbeatMu.Unlock()

	if stop == nil {
		t.Error("Connect did not start the heartbeat goroutine (heartbeatStop is nil)")
	}
}

func TestClient_Disconnect_StopsHeartbeat(t *testing.T) {
	pub, priv, err := lecrypto.GenerateKeyPair()
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}

	addr, cleanup := startTestRelay(t, priv)
	defer cleanup()

	pubB64 := lecrypto.ExportPublicKeyBase64(pub)
	client := newTestClient(t, addr, pubB64)

	if err := client.Connect(context.Background()); err != nil {
		t.Fatalf("Connect: %v", err)
	}

	// Sanity : heartbeat is running.
	client.heartbeatMu.Lock()
	hadStop := client.heartbeatStop
	client.heartbeatMu.Unlock()
	if hadStop == nil {
		t.Fatal("precondition: heartbeat not running after Connect")
	}

	if err := client.Disconnect(); err != nil {
		t.Fatalf("Disconnect: %v", err)
	}

	client.heartbeatMu.Lock()
	stop := client.heartbeatStop
	client.heartbeatMu.Unlock()
	if stop != nil {
		t.Errorf("Disconnect did not stop heartbeat (heartbeatStop still %v)", stop)
	}
}
