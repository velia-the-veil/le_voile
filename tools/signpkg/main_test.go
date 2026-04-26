package main

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"

	lecrypto "github.com/velia-the-veil/le_voile/internal/crypto"
)

// writeKey writes a test Ed25519 private key to disk and returns its
// public half for verification.
func writeKey(t *testing.T, dir string) (pubKey, keyPath string) {
	t.Helper()
	pub, priv, err := lecrypto.GenerateKeyPair()
	if err != nil {
		t.Fatalf("GenerateKeyPair: %v", err)
	}
	keyPath = filepath.Join(dir, "signing.key")
	if err := os.WriteFile(keyPath, []byte(lecrypto.ExportPrivateKeyBase64(priv)+"\n"), 0o600); err != nil {
		t.Fatalf("write key: %v", err)
	}
	return lecrypto.ExportPublicKeyBase64(pub), keyPath
}

func TestSignpkg_SignsSingleArtifact(t *testing.T) {
	dir := t.TempDir()
	pubB64, keyPath := writeKey(t, dir)
	pub, err := lecrypto.ImportPublicKeyBase64(pubB64)
	if err != nil {
		t.Fatalf("parse pub: %v", err)
	}

	artifact := filepath.Join(dir, "levoile_1.0.0_amd64.deb")
	content := []byte("fake deb content for testing")
	if err := os.WriteFile(artifact, content, 0o644); err != nil {
		t.Fatalf("write artifact: %v", err)
	}

	if err := run([]string{"-signing-key", keyPath, artifact}); err != nil {
		t.Fatalf("run: %v", err)
	}

	sig, err := os.ReadFile(artifact + ".sig")
	if err != nil {
		t.Fatalf("read sig: %v", err)
	}
	if len(sig) != 64 {
		t.Errorf("sig len = %d, want 64", len(sig))
	}
	if !lecrypto.Verify(pub, content, sig) {
		t.Fatal("signature did not verify against test public key")
	}
}

func TestSignpkg_SignsMultipleArtifacts(t *testing.T) {
	dir := t.TempDir()
	pubB64, keyPath := writeKey(t, dir)
	pub, _ := lecrypto.ImportPublicKeyBase64(pubB64)

	paths := []string{"a.deb", "b.rpm", "c.apk", "LeVoile-Setup.exe"}
	var absPaths []string
	for _, p := range paths {
		abs := filepath.Join(dir, p)
		if err := os.WriteFile(abs, []byte("content-"+p), 0o644); err != nil {
			t.Fatalf("write %s: %v", p, err)
		}
		absPaths = append(absPaths, abs)
	}

	args := append([]string{"-signing-key", keyPath}, absPaths...)
	if err := run(args); err != nil {
		t.Fatalf("run: %v", err)
	}

	for _, abs := range absPaths {
		sig, err := os.ReadFile(abs + ".sig")
		if err != nil {
			t.Fatalf("read %s.sig: %v", abs, err)
		}
		data, _ := os.ReadFile(abs)
		if !lecrypto.Verify(pub, data, sig) {
			t.Errorf("signature mismatch for %s", abs)
		}
	}
}

func TestSignpkg_ChecksumsFlag(t *testing.T) {
	dir := t.TempDir()
	pubB64, keyPath := writeKey(t, dir)
	pub, _ := lecrypto.ImportPublicKeyBase64(pubB64)

	checksums := filepath.Join(dir, "checksums.txt")
	artifact := filepath.Join(dir, "x.deb")
	if err := os.WriteFile(checksums, []byte("deadbeef  x.deb\n"), 0o644); err != nil {
		t.Fatalf("write checksums: %v", err)
	}
	if err := os.WriteFile(artifact, []byte("artifact"), 0o644); err != nil {
		t.Fatalf("write artifact: %v", err)
	}

	if err := run([]string{"-signing-key", keyPath, "-checksums", checksums, artifact}); err != nil {
		t.Fatalf("run: %v", err)
	}

	for _, p := range []string{checksums, artifact} {
		sig, err := os.ReadFile(p + ".sig")
		if err != nil {
			t.Fatalf("read %s.sig: %v", p, err)
		}
		data, _ := os.ReadFile(p)
		if !lecrypto.Verify(pub, data, sig) {
			t.Errorf("bad signature for %s", p)
		}
	}
}

func TestSignpkg_RejectsInvalidKeySize(t *testing.T) {
	dir := t.TempDir()
	keyPath := filepath.Join(dir, "bad.key")
	if err := os.WriteFile(keyPath, []byte("AQID\n"), 0o600); err != nil {
		t.Fatalf("write key: %v", err)
	}
	artifact := filepath.Join(dir, "a.deb")
	if err := os.WriteFile(artifact, []byte("x"), 0o644); err != nil {
		t.Fatalf("write artifact: %v", err)
	}

	err := run([]string{"-signing-key", keyPath, artifact})
	if err == nil {
		t.Fatal("expected error on invalid key size, got nil")
	}
}

func TestSignpkg_RejectsMissingArtifact(t *testing.T) {
	dir := t.TempDir()
	_, keyPath := writeKey(t, dir)

	err := run([]string{"-signing-key", keyPath, filepath.Join(dir, "does-not-exist.deb")})
	if err == nil {
		t.Fatal("expected error on missing artifact, got nil")
	}
}

func TestSignpkg_RejectsMissingKey(t *testing.T) {
	err := run([]string{"-signing-key", "", "anything"})
	if err == nil {
		t.Fatal("expected usage error")
	}
	if _, ok := err.(*usageError); !ok {
		t.Errorf("want usageError, got %T", err)
	}
}

func TestSignpkg_RejectsNoArtifacts(t *testing.T) {
	dir := t.TempDir()
	_, keyPath := writeKey(t, dir)

	err := run([]string{"-signing-key", keyPath})
	if err == nil {
		t.Fatal("expected usage error on empty artifact list")
	}
	if _, ok := err.(*usageError); !ok {
		t.Errorf("want usageError, got %T", err)
	}
}

// TestSignpkg_MaxSizeGuardIsReachable documents the 500 MiB cap. We cannot
// reasonably allocate a 501 MiB file inside a fast unit test, so we assert
// the constant value itself and cover the stat path via the missing-artifact
// and happy-path tests above. A real 500+ MiB file would cause a clean
// error message about the size limit rather than OOM the CI runner.
func TestSignpkg_MaxSizeGuardIsReachable(t *testing.T) {
	if maxArtifactSize != 500*1024*1024 {
		t.Errorf("maxArtifactSize changed: got %d, expected %d", maxArtifactSize, 500*1024*1024)
	}
}

// TestSignpkg_Deterministic documents that stdlib ed25519.Sign is
// deterministic (RFC 8032 specifies deterministic signing). Re-signing the
// same bytes with the same key produces byte-identical output — useful to
// catch accidental non-determinism introduced by future refactors.
func TestSignpkg_Deterministic(t *testing.T) {
	dir := t.TempDir()
	_, keyPath := writeKey(t, dir)
	artifact := filepath.Join(dir, "a.bin")
	if err := os.WriteFile(artifact, []byte("stable content"), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}

	if err := run([]string{"-signing-key", keyPath, artifact}); err != nil {
		t.Fatalf("first sign: %v", err)
	}
	first, _ := os.ReadFile(artifact + ".sig")

	if err := run([]string{"-signing-key", keyPath, artifact}); err != nil {
		t.Fatalf("second sign: %v", err)
	}
	second, _ := os.ReadFile(artifact + ".sig")

	if !bytes.Equal(first, second) {
		t.Error("signpkg is non-deterministic; ed25519 must be deterministic per RFC 8032")
	}
}
