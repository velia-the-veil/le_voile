package crypto

import (
	"crypto/ed25519"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// TestOpenSSLPKeyutlCompat proves the PKIX PEM emitted by ExportPublicKeyPEM
// is consumable by `openssl pkeyutl -verify -rawin`. This is the canonical
// verification path used by the AUR PKGBUILD verify() hook — if the format
// or OID drifts, all Arch installations break silently.
//
// Skipped when openssl binary is absent or < 1.1.1 (pre-rawin flag). CI
// (ubuntu-latest) ships OpenSSL 3.x, so this runs there.
func TestOpenSSLPKeyutlCompat(t *testing.T) {
	osslBin, err := exec.LookPath("openssl")
	if err != nil {
		t.Skip("openssl binary not found in PATH — skipping")
	}
	out, err := exec.Command(osslBin, "version").Output()
	if err != nil {
		t.Skipf("openssl version failed: %v", err)
	}
	if !strings.HasPrefix(string(out), "OpenSSL 1.1.1") &&
		!strings.HasPrefix(string(out), "OpenSSL 3.") {
		t.Skipf("openssl too old for -rawin: %s", strings.TrimSpace(string(out)))
	}

	pub, priv, err := GenerateKeyPair()
	if err != nil {
		t.Fatalf("GenerateKeyPair: %v", err)
	}
	message := []byte("story 7.4 openssl compat test payload")
	sig, err := Sign(priv, message)
	if err != nil {
		t.Fatalf("Sign: %v", err)
	}

	dir := t.TempDir()
	pemBytes, err := ExportPublicKeyPEM(pub)
	if err != nil {
		t.Fatalf("ExportPublicKeyPEM: %v", err)
	}
	pemPath := filepath.Join(dir, "pub.pem")
	artifactPath := filepath.Join(dir, "artifact.bin")
	sigPath := filepath.Join(dir, "artifact.bin.sig")
	for _, f := range []struct {
		path string
		data []byte
	}{
		{pemPath, pemBytes},
		{artifactPath, message},
		{sigPath, sig},
	} {
		if err := os.WriteFile(f.path, f.data, 0o644); err != nil {
			t.Fatalf("write %s: %v", f.path, err)
		}
	}

	// Invoke openssl pkeyutl -verify -rawin — identical to the PKGBUILD hook.
	cmd := exec.Command(osslBin, "pkeyutl",
		"-verify", "-pubin", "-inkey", pemPath,
		"-rawin", "-in", artifactPath, "-sigfile", sigPath)
	combined, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("openssl pkeyutl rejected signature: %v\noutput: %s", err, combined)
	}
	if !strings.Contains(string(combined), "Signature Verified Successfully") {
		t.Errorf("unexpected openssl output: %s", combined)
	}
}

// TestOpenSSLPKeyutlRejectsTampered checks that openssl correctly rejects a
// tampered artifact — proves the PKGBUILD verify() will fail-close on
// modified .deb files.
func TestOpenSSLPKeyutlRejectsTampered(t *testing.T) {
	osslBin, err := exec.LookPath("openssl")
	if err != nil {
		t.Skip("openssl not found")
	}
	// Quick 1.1.1+/3.x gate — same as the happy-path test.
	verOut, err := exec.Command(osslBin, "version").Output()
	if err != nil {
		t.Skipf("openssl version: %v", err)
	}
	if !strings.HasPrefix(string(verOut), "OpenSSL 1.1.1") &&
		!strings.HasPrefix(string(verOut), "OpenSSL 3.") {
		t.Skipf("openssl too old: %s", strings.TrimSpace(string(verOut)))
	}

	pub, priv, err := GenerateKeyPair()
	if err != nil {
		t.Fatalf("GenerateKeyPair: %v", err)
	}
	sig, err := Sign(priv, []byte("original"))
	if err != nil {
		t.Fatalf("Sign: %v", err)
	}
	pemBytes, err := ExportPublicKeyPEM(pub)
	if err != nil {
		t.Fatalf("ExportPublicKeyPEM: %v", err)
	}

	dir := t.TempDir()
	pemPath := filepath.Join(dir, "pub.pem")
	artifactPath := filepath.Join(dir, "artifact.bin")
	sigPath := filepath.Join(dir, "artifact.bin.sig")
	_ = os.WriteFile(pemPath, pemBytes, 0o644)
	// Write TAMPERED content, not the original.
	_ = os.WriteFile(artifactPath, []byte("tampered"), 0o644)
	_ = os.WriteFile(sigPath, sig, 0o644)

	cmd := exec.Command(osslBin, "pkeyutl",
		"-verify", "-pubin", "-inkey", pemPath,
		"-rawin", "-in", artifactPath, "-sigfile", sigPath)
	if err := cmd.Run(); err == nil {
		t.Fatal("openssl accepted a signature for tampered content")
	}
}

// TestExportedPEMHasCorrectHeader provides a fast sanity check that the PEM
// header is "PUBLIC KEY" and not something else (e.g. "RSA PUBLIC KEY").
// PKGBUILD verify() heredoc assumes this exact header.
func TestExportedPEMHasCorrectHeader(t *testing.T) {
	pub, _, err := GenerateKeyPair()
	if err != nil {
		t.Fatalf("GenerateKeyPair: %v", err)
	}
	pemBytes, err := ExportPublicKeyPEM(pub)
	if err != nil {
		t.Fatalf("ExportPublicKeyPEM: %v", err)
	}
	s := string(pemBytes)
	if !strings.HasPrefix(s, "-----BEGIN PUBLIC KEY-----") {
		t.Errorf("PEM header wrong: %q", s[:30])
	}
	if !strings.Contains(s, "-----END PUBLIC KEY-----") {
		t.Error("PEM footer missing")
	}
}

// Compile-time reference to ed25519 so the import is used even when the
// openssl tests are skipped.
var _ = ed25519.PublicKeySize
