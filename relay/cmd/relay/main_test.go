package main

import (
	"crypto/ed25519"
	"encoding/base64"
	"os"
	"path/filepath"
	"testing"
)

func TestLoadSigningKey_ValidKey(t *testing.T) {
	// Generate a real Ed25519 key pair for testing.
	_, priv, err := ed25519.GenerateKey(nil)
	if err != nil {
		t.Fatal(err)
	}

	encoded := base64.StdEncoding.EncodeToString(priv)

	tmpDir := t.TempDir()
	keyFile := filepath.Join(tmpDir, "key.b64")
	if err := os.WriteFile(keyFile, []byte(encoded), 0o600); err != nil {
		t.Fatal(err)
	}

	got, err := loadSigningKey(keyFile)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(got) != ed25519.PrivateKeySize {
		t.Errorf("key size = %d, want %d", len(got), ed25519.PrivateKeySize)
	}

	if !got.Equal(priv) {
		t.Error("loaded key does not match original")
	}
}

func TestLoadSigningKey_ValidKeyWithTrailingNewline(t *testing.T) {
	_, priv, _ := ed25519.GenerateKey(nil)
	encoded := base64.StdEncoding.EncodeToString(priv) + "\n"

	tmpDir := t.TempDir()
	keyFile := filepath.Join(tmpDir, "key.b64")
	if err := os.WriteFile(keyFile, []byte(encoded), 0o600); err != nil {
		t.Fatal(err)
	}

	got, err := loadSigningKey(keyFile)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if !got.Equal(priv) {
		t.Error("loaded key does not match original (with trailing newline)")
	}
}

func TestLoadSigningKey_FileNotFound(t *testing.T) {
	_, err := loadSigningKey("/nonexistent/path/key.b64")
	if err == nil {
		t.Error("expected error for missing file")
	}
}

func TestLoadSigningKey_InvalidBase64(t *testing.T) {
	tmpDir := t.TempDir()
	keyFile := filepath.Join(tmpDir, "bad.b64")
	if err := os.WriteFile(keyFile, []byte("not-valid-base64!!!"), 0o600); err != nil {
		t.Fatal(err)
	}

	_, err := loadSigningKey(keyFile)
	if err == nil {
		t.Error("expected error for invalid base64")
	}
}

func TestLoadSigningKey_WrongKeySize(t *testing.T) {
	// 32 bytes instead of 64 (ed25519.PrivateKeySize).
	shortKey := make([]byte, 32)
	encoded := base64.StdEncoding.EncodeToString(shortKey)

	tmpDir := t.TempDir()
	keyFile := filepath.Join(tmpDir, "short.b64")
	if err := os.WriteFile(keyFile, []byte(encoded), 0o600); err != nil {
		t.Fatal(err)
	}

	_, err := loadSigningKey(keyFile)
	if err == nil {
		t.Error("expected error for wrong key size")
	}
}

func TestLoadSigningKey_EmptyFile(t *testing.T) {
	tmpDir := t.TempDir()
	keyFile := filepath.Join(tmpDir, "empty.b64")
	if err := os.WriteFile(keyFile, []byte(""), 0o600); err != nil {
		t.Fatal(err)
	}

	_, err := loadSigningKey(keyFile)
	if err == nil {
		t.Error("expected error for empty file")
	}
}
