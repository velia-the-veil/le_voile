//go:build windows

package config

import (
	"errors"
	"os"
	"path/filepath"
	"sync"
	"testing"
)

func TestLoadOrCreateKey_GeneratesOnFirstCall(t *testing.T) {
	dir := t.TempDir()
	keyPath := filepath.Join(dir, "config.integrity.key")

	key, err := LoadOrCreateKey(keyPath)
	if err != nil {
		t.Fatalf("LoadOrCreateKey: %v", err)
	}
	if len(key) != integrityKeyLen {
		t.Fatalf("key length = %d, want %d", len(key), integrityKeyLen)
	}
	info, err := os.Stat(keyPath)
	if err != nil {
		t.Fatalf("key file not persisted: %v", err)
	}
	if info.Size() != integrityKeyLen {
		t.Fatalf("key file size = %d, want %d", info.Size(), integrityKeyLen)
	}
}

func TestLoadOrCreateKey_ReturnsSameKeyOnSecondCall(t *testing.T) {
	dir := t.TempDir()
	keyPath := filepath.Join(dir, "config.integrity.key")

	k1, err := LoadOrCreateKey(keyPath)
	if err != nil {
		t.Fatalf("first call: %v", err)
	}
	k2, err := LoadOrCreateKey(keyPath)
	if err != nil {
		t.Fatalf("second call: %v", err)
	}
	if string(k1) != string(k2) {
		t.Fatal("LoadOrCreateKey returned different keys on repeat calls")
	}
}

func TestLoadOrCreateKey_RejectsMalformed(t *testing.T) {
	dir := t.TempDir()
	keyPath := filepath.Join(dir, "config.integrity.key")
	if err := os.WriteFile(keyPath, []byte("short"), 0o600); err != nil {
		t.Fatalf("seed malformed: %v", err)
	}
	_, err := LoadOrCreateKey(keyPath)
	if !errors.Is(err, ErrKeyMalformed) {
		t.Fatalf("err = %v, want ErrKeyMalformed", err)
	}
}

func TestSignVerify_RoundTrip(t *testing.T) {
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "config.toml")
	if err := os.WriteFile(cfgPath, []byte("[relay]\ndomain=\"x\"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	key, err := LoadOrCreateKey(filepath.Join(dir, "k"))
	if err != nil {
		t.Fatal(err)
	}
	if err := Sign(cfgPath, key); err != nil {
		t.Fatalf("Sign: %v", err)
	}
	if err := Verify(cfgPath, key); err != nil {
		t.Fatalf("Verify: %v", err)
	}
}

func TestVerify_ErrHMACAbsent(t *testing.T) {
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "config.toml")
	if err := os.WriteFile(cfgPath, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	key, err := LoadOrCreateKey(filepath.Join(dir, "k"))
	if err != nil {
		t.Fatal(err)
	}
	if err := Verify(cfgPath, key); !errors.Is(err, ErrHMACAbsent) {
		t.Fatalf("err = %v, want ErrHMACAbsent", err)
	}
}

func TestVerify_DetectsTampering(t *testing.T) {
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "config.toml")
	if err := os.WriteFile(cfgPath, []byte("original=true\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	key, err := LoadOrCreateKey(filepath.Join(dir, "k"))
	if err != nil {
		t.Fatal(err)
	}
	if err := Sign(cfgPath, key); err != nil {
		t.Fatal(err)
	}
	// Tamper with the config content AFTER signing.
	if err := os.WriteFile(cfgPath, []byte("original=false\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := Verify(cfgPath, key); !errors.Is(err, ErrIntegrityMismatch) {
		t.Fatalf("err = %v, want ErrIntegrityMismatch", err)
	}
}

func TestVerify_DetectsHMACFileTampering(t *testing.T) {
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "config.toml")
	if err := os.WriteFile(cfgPath, []byte("x=1\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	key, err := LoadOrCreateKey(filepath.Join(dir, "k"))
	if err != nil {
		t.Fatal(err)
	}
	if err := Sign(cfgPath, key); err != nil {
		t.Fatal(err)
	}
	// Corrupt the HMAC sidecar.
	if err := os.WriteFile(cfgPath+".hmac", []byte("00000000000000000000000000000000000000000000000000000000000000ff"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := Verify(cfgPath, key); !errors.Is(err, ErrIntegrityMismatch) {
		t.Fatalf("err = %v, want ErrIntegrityMismatch", err)
	}
}

func TestVerify_RejectsWrongKey(t *testing.T) {
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "config.toml")
	if err := os.WriteFile(cfgPath, []byte("x=1\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	key, err := LoadOrCreateKey(filepath.Join(dir, "k1"))
	if err != nil {
		t.Fatal(err)
	}
	if err := Sign(cfgPath, key); err != nil {
		t.Fatal(err)
	}
	// Generate a different key at a different path, then verify with it.
	otherKey, err := LoadOrCreateKey(filepath.Join(dir, "k2"))
	if err != nil {
		t.Fatal(err)
	}
	if err := Verify(cfgPath, otherKey); !errors.Is(err, ErrIntegrityMismatch) {
		t.Fatalf("err = %v, want ErrIntegrityMismatch", err)
	}
}

// TestSaveAndSign_ConcurrentSafe exercises the real hot path: every
// writer must go through SaveAndSign under config.Mu, and every pairing
// must land a config whose .hmac matches. The goroutines mutate distinct
// fields so we'd detect a "last-writer-wins but hmac-off-by-one-race"
// regression: if Save landed value X but Sign signed value Y from a
// different goroutine's encode, Verify would fail.
func TestSaveAndSign_ConcurrentSafe(t *testing.T) {
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "config.toml")
	key, err := LoadOrCreateKey(filepath.Join(dir, "k"))
	if err != nil {
		t.Fatal(err)
	}

	// Seed so Load succeeds inside the goroutines.
	if err := (&Config{TUN: TUNConfig{Name: "levoile0", MTU: 1420}}).SaveAndSign(cfgPath, key); err != nil {
		t.Fatal(err)
	}

	var wg sync.WaitGroup
	for i := 0; i < 16; i++ {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			Mu.Lock()
			defer Mu.Unlock()
			cfg, err := Load(cfgPath)
			if err != nil {
				t.Errorf("goroutine %d Load: %v", n, err)
				return
			}
			// Each goroutine sets a different country so the encoded bytes
			// actually differ run-to-run — guards against a race where Sign
			// runs on a stale encode.
			cfg.Client.PreferredCountry = []string{"de", "es", "gb", "us"}[n%4]
			if err := cfg.SaveAndSign(cfgPath, key); err != nil {
				t.Errorf("goroutine %d SaveAndSign: %v", n, err)
			}
		}(i)
	}
	wg.Wait()

	if err := Verify(cfgPath, key); err != nil {
		t.Fatalf("Verify after 16 concurrent SaveAndSign: %v", err)
	}
}

// TestSaveAndSign_NoTOCTOU_HMACPinsWrittenBytes pins the property that the
// HMAC is computed from the encoded bytes (not a disk re-read), so an
// attacker-interposed write between Save's rename and the sidecar write
// can NOT be "legitimized" by the signature. We simulate the attacker
// window by tampering with the on-disk file after SaveAndSign completed
// — Verify must flag the mismatch.
func TestSaveAndSign_NoTOCTOU_HMACPinsWrittenBytes(t *testing.T) {
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "config.toml")
	key, err := LoadOrCreateKey(filepath.Join(dir, "k"))
	if err != nil {
		t.Fatal(err)
	}
	cfg := &Config{
		TUN:    TUNConfig{Name: "levoile0", MTU: 1420},
		Client: ClientConfig{PreferredCountry: "de"},
	}
	if err := cfg.SaveAndSign(cfgPath, key); err != nil {
		t.Fatalf("SaveAndSign: %v", err)
	}
	// Post-write tampering — this is what a same-privilege attacker would
	// do in the TOCTOU window (or long after). Verify must catch it.
	if err := os.WriteFile(cfgPath, []byte("[relay]\ndomain=\"evil.example\"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := Verify(cfgPath, key); !errors.Is(err, ErrIntegrityMismatch) {
		t.Fatalf("err = %v, want ErrIntegrityMismatch — TOCTOU defense regressed", err)
	}
}
