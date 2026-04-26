package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	lecrypto "github.com/velia-the-veil/le_voile/internal/crypto"
)

func TestGenkey_WritesKeyPair(t *testing.T) {
	dir := t.TempDir()
	base := filepath.Join(dir, "signing")

	if err := run([]string{"-out", base}); err != nil {
		t.Fatalf("run: %v", err)
	}

	keyBytes, err := os.ReadFile(base + ".key")
	if err != nil {
		t.Fatalf("read key: %v", err)
	}
	priv, err := lecrypto.ImportPrivateKeyBase64(strings.TrimSpace(string(keyBytes)))
	if err != nil {
		t.Fatalf("parse priv: %v", err)
	}

	pubBytes, err := os.ReadFile(base + ".pub")
	if err != nil {
		t.Fatalf("read pub: %v", err)
	}
	pub, err := lecrypto.ImportPublicKeyBase64(strings.TrimSpace(string(pubBytes)))
	if err != nil {
		t.Fatalf("parse pub: %v", err)
	}

	// Round-trip: sign a message with priv, verify with pub.
	msg := []byte("genkey test")
	sig, err := lecrypto.Sign(priv, msg)
	if err != nil {
		t.Fatalf("sign: %v", err)
	}
	if !lecrypto.Verify(pub, msg, sig) {
		t.Fatal("priv/pub pair does not round-trip")
	}
}

func TestGenkey_PEMFlag(t *testing.T) {
	dir := t.TempDir()
	base := filepath.Join(dir, "signing")

	if err := run([]string{"-out", base, "-pem"}); err != nil {
		t.Fatalf("run: %v", err)
	}

	pemBytes, err := os.ReadFile(base + ".pub.pem")
	if err != nil {
		t.Fatalf("read pem: %v", err)
	}
	pub, err := lecrypto.ImportPublicKeyPEM(pemBytes)
	if err != nil {
		t.Fatalf("parse pem: %v", err)
	}

	pubFileBytes, err := os.ReadFile(base + ".pub")
	if err != nil {
		t.Fatalf("read pub file: %v", err)
	}
	pubFile, err := lecrypto.ImportPublicKeyBase64(strings.TrimSpace(string(pubFileBytes)))
	if err != nil {
		t.Fatalf("parse pub file: %v", err)
	}
	if !pub.Equal(pubFile) {
		t.Error("PEM and base64 public keys disagree")
	}
}

func TestGenkey_RefusesOverwrite(t *testing.T) {
	dir := t.TempDir()
	base := filepath.Join(dir, "signing")

	if err := run([]string{"-out", base}); err != nil {
		t.Fatalf("first run: %v", err)
	}
	if err := run([]string{"-out", base}); err == nil {
		t.Fatal("second run without -force should fail, got nil")
	}
	if err := run([]string{"-out", base, "-force"}); err != nil {
		t.Fatalf("second run with -force should succeed: %v", err)
	}
}

func TestGenkey_RejectsMissingOut(t *testing.T) {
	err := run([]string{})
	if err == nil {
		t.Fatal("run without -out should fail")
	}
	if _, ok := err.(*usageError); !ok {
		t.Errorf("want *usageError, got %T: %v", err, err)
	}
}
