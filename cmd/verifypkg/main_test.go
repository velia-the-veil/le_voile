package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	lecrypto "github.com/velia-the-veil/le_voile/internal/crypto"
)

// buildArtifactWithSig creates an artifact plus its valid signature on disk
// using a freshly generated Ed25519 key. Returns the artifact path, sig
// path, and the public key that validates the signature.
func buildArtifactWithSig(t *testing.T, content []byte) (artifactPath, sigPath, pubB64 string) {
	t.Helper()
	dir := t.TempDir()
	pub, priv, err := lecrypto.GenerateKeyPair()
	if err != nil {
		t.Fatalf("GenerateKeyPair: %v", err)
	}

	artifactPath = filepath.Join(dir, "artifact.bin")
	if err := os.WriteFile(artifactPath, content, 0o644); err != nil {
		t.Fatalf("write artifact: %v", err)
	}

	sig, err := lecrypto.Sign(priv, content)
	if err != nil {
		t.Fatalf("sign: %v", err)
	}
	sigPath = artifactPath + ".sig"
	if err := os.WriteFile(sigPath, sig, 0o644); err != nil {
		t.Fatalf("write sig: %v", err)
	}

	return artifactPath, sigPath, lecrypto.ExportPublicKeyBase64(pub)
}

func TestVerifypkg_ValidExplicitKey(t *testing.T) {
	artifact, sig, pubB64 := buildArtifactWithSig(t, []byte("content v1"))
	if err := run([]string{"-pubkey", pubB64, artifact, sig}); err != nil {
		t.Fatalf("run: %v", err)
	}
}

func TestVerifypkg_RejectsTamperedArtifact(t *testing.T) {
	artifact, sig, pubB64 := buildArtifactWithSig(t, []byte("original"))
	if err := os.WriteFile(artifact, []byte("tampered"), 0o644); err != nil {
		t.Fatalf("overwrite: %v", err)
	}
	err := run([]string{"-pubkey", pubB64, artifact, sig})
	if err == nil {
		t.Fatal("expected verification to fail on tampered artifact")
	}
}

func TestVerifypkg_RejectsWrongKey(t *testing.T) {
	artifact, sig, _ := buildArtifactWithSig(t, []byte("content"))
	otherPub, _, err := lecrypto.GenerateKeyPair()
	if err != nil {
		t.Fatalf("GenerateKeyPair: %v", err)
	}
	err = run([]string{"-pubkey", lecrypto.ExportPublicKeyBase64(otherPub), artifact, sig})
	if err == nil {
		t.Fatal("expected failure with wrong key")
	}
}

func TestVerifypkg_RejectsBadPubFlag(t *testing.T) {
	artifact, sig, _ := buildArtifactWithSig(t, []byte("x"))
	err := run([]string{"-pubkey", "not-base64!!!", artifact, sig})
	if err == nil {
		t.Fatal("expected parse failure on bad pubkey")
	}
}

func TestVerifypkg_UsesEmbeddedKeyWhenAvailable(t *testing.T) {
	// The embedded release key was provisioned at T3. This test signs with
	// NOT the embedded key (we don't have the private half here), so it
	// must fail — the important thing is it loads and reports a clear
	// failure, not a missing-key error.
	artifact, sig, _ := buildArtifactWithSig(t, []byte("x"))
	err := run([]string{artifact, sig})
	if err == nil {
		t.Fatal("expected verification to fail — signature uses an unrelated test key")
	}
	// The error should be about signature mismatch, not about missing
	// embedded key — that proves the embedded key loaded.
	if _, ok := err.(*usageError); ok {
		t.Fatalf("unexpected usage error: %v", err)
	}
}

func TestVerifypkg_TryNextNoOp(t *testing.T) {
	// With no rotation key configured, -try-next should still run and
	// simply fail verification (since the signature uses a test key).
	artifact, sig, _ := buildArtifactWithSig(t, []byte("x"))
	err := run([]string{"-try-next", artifact, sig})
	if err == nil {
		t.Fatal("expected failure")
	}
}

func TestVerifypkg_BadInvocation(t *testing.T) {
	err := run([]string{"only-one-arg"})
	if err == nil {
		t.Fatal("expected usage error")
	}
	if _, ok := err.(*usageError); !ok {
		t.Errorf("want usageError, got %T", err)
	}
}

func TestVerifypkg_MissingArtifactFile(t *testing.T) {
	dir := t.TempDir()
	err := run([]string{"-pubkey", "AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA=",
		filepath.Join(dir, "absent.bin"), filepath.Join(dir, "absent.bin.sig")})
	if err == nil {
		t.Fatal("expected I/O error on missing artifact")
	}
}

// TestVerifypkg_RejectsWrongSignatureSize covers H3: a truncated / padded /
// CRLF-mangled .sig must fail-loud before Verify() is even called, with a
// clear size diagnostic (not a generic "did not verify").
func TestVerifypkg_RejectsWrongSignatureSize(t *testing.T) {
	dir := t.TempDir()
	artifact := filepath.Join(dir, "x.deb")
	sig := filepath.Join(dir, "x.deb.sig")
	if err := os.WriteFile(artifact, []byte("x"), 0o644); err != nil {
		t.Fatalf("write artifact: %v", err)
	}
	// 32-byte "sig" instead of 64 — typical truncation.
	if err := os.WriteFile(sig, make([]byte, 32), 0o644); err != nil {
		t.Fatalf("write sig: %v", err)
	}
	err := run([]string{"-pubkey", "AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA=", artifact, sig})
	if err == nil {
		t.Fatal("expected size-guard error")
	}
	if got := err.Error(); !strings.Contains(got, "expected 64 bytes, got 32") {
		t.Errorf("expected size-specific error, got: %s", got)
	}
}

// TestVerifypkg_RejectsBloatedSignature: a 65-byte file (e.g. one trailing
// newline added by git CRLF conversion) must also be rejected up front.
func TestVerifypkg_RejectsBloatedSignature(t *testing.T) {
	dir := t.TempDir()
	artifact := filepath.Join(dir, "x.deb")
	sig := filepath.Join(dir, "x.deb.sig")
	_ = os.WriteFile(artifact, []byte("x"), 0o644)
	_ = os.WriteFile(sig, append(make([]byte, 64), '\n'), 0o644)
	err := run([]string{"-pubkey", "AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA=", artifact, sig})
	if err == nil {
		t.Fatal("expected size-guard error for 65-byte sig")
	}
	if !strings.Contains(err.Error(), "got 65") {
		t.Errorf("expected 'got 65' in error, got: %s", err)
	}
}
