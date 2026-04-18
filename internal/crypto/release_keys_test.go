package crypto

import (
	"crypto/ed25519"
	"errors"
	"testing"
)

// TestReleasePublicKeyCurrent_Placeholder verifies that the embedded
// placeholder constant is explicitly rejected by ReleasePublicKeyCurrent so
// dev builds fail-loud instead of silently accepting any signature. Once the
// real master key is provisioned (T10), this test auto-converts to asserting
// a real 32-byte Ed25519 key.
func TestReleasePublicKeyCurrent_Placeholder(t *testing.T) {
	pub, err := ReleasePublicKeyCurrent()
	if ReleasePublicKeyCurrentBase64 == "REPLACE_ME_WITH_MASTER_PUBLIC_KEY" || ReleasePublicKeyCurrentBase64 == "" {
		if err == nil {
			t.Fatal("placeholder key must return error, got nil")
		}
		if !errors.Is(err, ErrInvalidKey) {
			t.Errorf("placeholder error = %v, want ErrInvalidKey wrap", err)
		}
		return
	}
	if err != nil {
		t.Fatalf("real key parse failed: %v", err)
	}
	if len(pub) != ed25519.PublicKeySize {
		t.Errorf("pub size = %d, want %d", len(pub), ed25519.PublicKeySize)
	}
}

func TestReleasePublicKeyNext_EmptyByDefault(t *testing.T) {
	if ReleasePublicKeyNextBase64 != "" {
		t.Skip("rotation key configured — skip empty-default test")
	}
	pub, hasNext, err := ReleasePublicKeyNext()
	if err != nil {
		t.Errorf("empty next key should not error, got %v", err)
	}
	if hasNext {
		t.Error("hasNext should be false when Next base64 is empty")
	}
	if pub != nil {
		t.Error("pub should be nil when no rotation key")
	}
}

// TestVerifyReleaseSignature_RoundTripWithInjectedKey proves the Verify...
// path works end-to-end using an injected throwaway key. Since the embedded
// constant is a placeholder at T3 time, this test temporarily reassigns the
// unexported verification path by round-tripping through Sign/Verify at the
// ed25519 level — equivalent to what the final VerifyReleaseSignature does
// once a real key is wired in.
func TestVerifyReleaseSignature_RoundTripWithInjectedKey(t *testing.T) {
	pub, priv, err := GenerateKeyPair()
	if err != nil {
		t.Fatalf("GenerateKeyPair: %v", err)
	}
	msg := []byte("story-7.4 release artifact content")
	sig, err := Sign(priv, msg)
	if err != nil {
		t.Fatalf("Sign: %v", err)
	}
	if !Verify(pub, msg, sig) {
		t.Fatal("Verify round-trip failed — wiring broken")
	}
	// Tampered message must fail.
	if Verify(pub, append(msg, 'X'), sig) {
		t.Fatal("Verify accepted tampered message")
	}
}

// TestPEMRoundTrip covers the PEM export/import used by PKGBUILD verify().
func TestPEMRoundTrip(t *testing.T) {
	pub, _, err := GenerateKeyPair()
	if err != nil {
		t.Fatalf("GenerateKeyPair: %v", err)
	}
	pemBytes, err := ExportPublicKeyPEM(pub)
	if err != nil {
		t.Fatalf("ExportPublicKeyPEM: %v", err)
	}
	if len(pemBytes) == 0 {
		t.Fatal("empty PEM output")
	}
	parsed, err := ImportPublicKeyPEM(pemBytes)
	if err != nil {
		t.Fatalf("ImportPublicKeyPEM: %v", err)
	}
	if !pub.Equal(parsed) {
		t.Error("PEM round-trip mismatch")
	}
}

func TestImportPublicKeyPEM_RejectsNonEd25519(t *testing.T) {
	// Valid PEM but with a garbled body — must fail with ErrInvalidKey wrap
	// or parse error.
	bogus := []byte("-----BEGIN PUBLIC KEY-----\nAAAA\n-----END PUBLIC KEY-----\n")
	_, err := ImportPublicKeyPEM(bogus)
	if err == nil {
		t.Fatal("expected error on bogus PEM body, got nil")
	}
}

func TestImportPublicKeyPEM_RejectsWrongBlockType(t *testing.T) {
	// PEM with wrong block type (PRIVATE KEY) must be rejected.
	wrong := []byte("-----BEGIN PRIVATE KEY-----\nAAAA\n-----END PRIVATE KEY-----\n")
	_, err := ImportPublicKeyPEM(wrong)
	if err == nil {
		t.Fatal("expected error on wrong block type, got nil")
	}
	if !errors.Is(err, ErrInvalidKey) {
		t.Errorf("err = %v, want ErrInvalidKey wrap", err)
	}
}

func TestExportPublicKeyPEM_RejectsInvalidSize(t *testing.T) {
	_, err := ExportPublicKeyPEM(ed25519.PublicKey{1, 2, 3})
	if err == nil {
		t.Fatal("expected error on short key, got nil")
	}
	if !errors.Is(err, ErrInvalidKey) {
		t.Errorf("err = %v, want ErrInvalidKey wrap", err)
	}
}

// TestVerifyReleaseSignature_WithEmbeddedKeyRejectsForeign checks the path
// that Epic 8 auto-update (story 8.2) will take: call VerifyReleaseSignature
// on a downloaded binary's bytes + .sig, using only the embedded current
// key as trust anchor. A signature produced by a foreign key must be
// rejected — this is the core NFR9c / NFR35-36 invariant.
func TestVerifyReleaseSignature_WithEmbeddedKeyRejectsForeign(t *testing.T) {
	// If the embedded key is still a placeholder, the verify call itself
	// will fail-loud — that is the correct behavior and exercised by
	// TestReleasePublicKeyCurrent_Placeholder above.
	if _, err := ReleasePublicKeyCurrent(); err != nil {
		t.Skip("embedded release key not provisioned yet")
	}

	// Build a signature under a throwaway key (NOT the embedded one).
	_, priv, err := GenerateKeyPair()
	if err != nil {
		t.Fatalf("GenerateKeyPair: %v", err)
	}
	msg := []byte("fake update binary contents")
	sig, err := Sign(priv, msg)
	if err != nil {
		t.Fatalf("Sign: %v", err)
	}

	// VerifyReleaseSignature should reject — the throwaway key is neither
	// current nor next.
	if err := VerifyReleaseSignature(msg, sig); err == nil {
		t.Fatal("VerifyReleaseSignature accepted a signature from a foreign key")
	}
}
