// R-T8 (2026-05-05) — Tests for QUIC connection migration.
//
// Smoke tests that exercise the *quic.Conn capture flow and MigrateToFD
// error paths. The success path (full migration with PATH_CHALLENGE /
// PATH_RESPONSE on a real socket pair) is validated end-to-end on Android
// device — JVM-only tests would need a complex two-server fixture and
// would not exercise the gomobile bridge anyway.

package tunnel

import (
	"context"
	"errors"
	"testing"

	lecrypto "github.com/velia-the-veil/le_voile/internal/crypto"
)

func TestClient_MigrateToFD_NoActiveConn_ReturnsSentinel(t *testing.T) {
	// Fresh client without Connect — no QUIC handle has been captured yet.
	c := &Client{state: NewStateManager()}

	// fd=-1 is invalid ; MigrateToFD should detect "no conn" first and
	// return the sentinel BEFORE touching the fd.
	err := c.MigrateToFD(context.Background(), -1)
	if !errors.Is(err, ErrMigrationNoActiveConn) {
		t.Fatalf("expected ErrMigrationNoActiveConn, got %v", err)
	}
}

func TestClient_migrationCapture_BeforeConnect_Nil(t *testing.T) {
	c := &Client{state: NewStateManager()}

	conn, transport := c.migrationCapture()
	if conn != nil {
		t.Errorf("expected captured conn to be nil before Connect, got %T", conn)
	}
	if transport != nil {
		t.Errorf("expected captured transport to be nil before Connect, got %T", transport)
	}
}

func TestClient_migrationCapture_AfterConnect_NonNil(t *testing.T) {
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

	// Trigger HTTP/3 RoundTrip so the lazy Dial actually fires and the
	// custom callback captures the *quic.Conn / *quic.Transport.
	// Connect already calls verifyRelay which does a /verify POST → triggers
	// dial. So capture should already be populated.
	conn, transport := client.migrationCapture()
	if conn == nil {
		t.Errorf("expected captured *quic.Conn after Connect, got nil")
	}
	if transport == nil {
		t.Errorf("expected captured *quic.Transport after Connect, got nil")
	}
}

func TestClient_ResetTransport_ClearsCapture(t *testing.T) {
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

	// Sanity : capture is populated.
	if conn, _ := client.migrationCapture(); conn == nil {
		t.Fatal("precondition: expected non-nil capture after Connect")
	}

	// Disconnect → ResetTransport → capture cleared.
	if err := client.Disconnect(); err != nil {
		t.Fatalf("Disconnect: %v", err)
	}

	conn, transport := client.migrationCapture()
	if conn != nil {
		t.Errorf("expected capture cleared after Disconnect, conn=%T still set", conn)
	}
	if transport != nil {
		t.Errorf("expected capture cleared after Disconnect, transport=%T still set", transport)
	}
}

func TestClient_MigrateToFD_AfterDisconnect_ReturnsSentinel(t *testing.T) {
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
	if err := client.Disconnect(); err != nil {
		t.Fatalf("Disconnect: %v", err)
	}

	// After Disconnect the captured handles are cleared — migration
	// must refuse cleanly.
	err = client.MigrateToFD(context.Background(), -1)
	if !errors.Is(err, ErrMigrationNoActiveConn) {
		t.Fatalf("expected ErrMigrationNoActiveConn after Disconnect, got %v", err)
	}
}
