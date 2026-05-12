package relay

import (
	"crypto/ed25519"
	"crypto/rand"
	"testing"
)

// TestSessionToken_NonceFreshness covers fix C4: two tokens created back to
// back for the same client IP must differ. Without a nonce, the payload
// would be deterministic (IPHash is SHA256 of IP, Issued is seconds-grained)
// and two tokens minted within the same second would be identical — which
// simplifies replay forensics and blocks future server-side replay caches
// from ever being useful.
func TestSessionToken_NonceFreshness(t *testing.T) {
	_, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("GenerateKey: %v", err)
	}

	t1, err := CreateSessionToken(priv, "203.0.113.5")
	if err != nil {
		t.Fatalf("CreateSessionToken #1: %v", err)
	}
	t2, err := CreateSessionToken(priv, "203.0.113.5")
	if err != nil {
		t.Fatalf("CreateSessionToken #2: %v", err)
	}

	if t1 == t2 {
		t.Fatalf("two tokens for same IP must differ (nonce missing?)\n  #1 = %s\n  #2 = %s", t1, t2)
	}
}

// TestSessionToken_NoncePresent parses the emitted token and confirms the
// Nonce field is populated with a 32-hex-char value.
func TestSessionToken_NoncePresent(t *testing.T) {
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("GenerateKey: %v", err)
	}
	token, err := CreateSessionToken(priv, "203.0.113.5")
	if err != nil {
		t.Fatalf("CreateSessionToken: %v", err)
	}

	payload, err := VerifySessionToken(pub, token)
	if err != nil {
		t.Fatalf("VerifySessionToken: %v", err)
	}
	if payload.Nonce == "" {
		t.Error("payload.Nonce must not be empty")
	}
	if len(payload.Nonce) != 32 {
		t.Errorf("payload.Nonce len = %d, want 32 (16 bytes hex)", len(payload.Nonce))
	}
}
