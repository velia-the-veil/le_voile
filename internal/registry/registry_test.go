package registry

import (
	"crypto/ed25519"
	"encoding/base64"
	"encoding/json"
	"errors"
	"testing"
	"time"
)

// testRegistry creates a signed registry for testing.
func testRegistry(t *testing.T, relayCount int) (masterPub ed25519.PublicKey, masterPriv ed25519.PrivateKey, entries []RelayEntry) {
	t.Helper()
	masterPub, masterPriv, err := ed25519.GenerateKey(nil)
	if err != nil {
		t.Fatal(err)
	}

	for i := 0; i < relayCount; i++ {
		relayPub, _, err := ed25519.GenerateKey(nil)
		if err != nil {
			t.Fatal(err)
		}
		relayPubB64 := base64.StdEncoding.EncodeToString(relayPub)
		msg := append([]byte(signaturePrefix), relayPub...)
		sig := ed25519.Sign(masterPriv, msg)
		sigB64 := base64.StdEncoding.EncodeToString(sig)

		entries = append(entries, RelayEntry{
			ID:        "relay-" + string(rune('a'+i)),
			Domain:    "relay" + string(rune('a'+i)) + ".example.com",
			PublicKey: relayPubB64,
			Signature: sigB64,
			Added:     time.Now(),
		})
	}
	return
}

func makeRegistryJSON(t *testing.T, version int, masterPub ed25519.PublicKey, relays []RelayEntry) []byte {
	t.Helper()
	reg := struct {
		Version         int          `json:"version"`
		MasterPublicKey string       `json:"master_public_key"`
		Relays          []RelayEntry `json:"relays"`
		Updated         time.Time    `json:"updated"`
	}{
		Version:         version,
		MasterPublicKey: base64.StdEncoding.EncodeToString(masterPub),
		Relays:          relays,
		Updated:         time.Now(),
	}
	data, err := json.Marshal(reg)
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func TestParse_ValidJSON(t *testing.T) {
	masterPub, _, entries := testRegistry(t, 2)
	data := makeRegistryJSON(t, 1, masterPub, entries)

	reg, err := Parse(data)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if reg.Version != CurrentVersion {
		t.Errorf("version: got %d, want %d", reg.Version, CurrentVersion)
	}
	if len(reg.Relays) != 2 {
		t.Errorf("relays: got %d, want 2", len(reg.Relays))
	}
}

func TestParse_InvalidJSON(t *testing.T) {
	_, err := Parse([]byte("{{not json"))
	if err == nil {
		t.Error("expected error for invalid JSON")
	}
}

func TestParse_EmptyRelays(t *testing.T) {
	masterPub, _, _ := testRegistry(t, 0)
	data := makeRegistryJSON(t, 1, masterPub, nil)

	_, err := Parse(data)
	if !errors.Is(err, ErrRegistryEmpty) {
		t.Errorf("expected ErrRegistryEmpty, got %v", err)
	}
}

func TestParse_WrongVersion(t *testing.T) {
	masterPub, _, entries := testRegistry(t, 1)
	data := makeRegistryJSON(t, 2, masterPub, entries)

	_, err := Parse(data)
	if err == nil {
		t.Error("expected error for wrong version")
	}
}

func TestVerifyRelaySignature_Valid(t *testing.T) {
	masterPub, _, entries := testRegistry(t, 1)
	if err := VerifyRelaySignature(masterPub, entries[0]); err != nil {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestVerifyRelaySignature_InvalidSignature(t *testing.T) {
	masterPub, _, entries := testRegistry(t, 1)
	// Corrupt the signature.
	entries[0].Signature = base64.StdEncoding.EncodeToString(make([]byte, 64))

	err := VerifyRelaySignature(masterPub, entries[0])
	if !errors.Is(err, ErrInvalidSignature) {
		t.Errorf("expected ErrInvalidSignature, got %v", err)
	}
}

func TestVerifyRelaySignature_WrongMasterKey(t *testing.T) {
	_, _, entries := testRegistry(t, 1)
	// Generate a different master key.
	otherPub, _, _ := ed25519.GenerateKey(nil)

	err := VerifyRelaySignature(otherPub, entries[0])
	if !errors.Is(err, ErrInvalidSignature) {
		t.Errorf("expected ErrInvalidSignature, got %v", err)
	}
}

func TestVerifyAll_MixedValidity(t *testing.T) {
	masterPub, _, entries := testRegistry(t, 2)

	// Add an invalid entry.
	invalidEntry := RelayEntry{
		ID:        "bad-relay",
		Domain:    "bad.example.com",
		PublicKey: entries[0].PublicKey,
		Signature: base64.StdEncoding.EncodeToString(make([]byte, 64)), // bad signature
	}
	allEntries := append(entries, invalidEntry)

	reg := &Registry{
		Version:         1,
		MasterPublicKey: base64.StdEncoding.EncodeToString(masterPub),
		Relays:          allEntries,
	}

	verified, err := reg.VerifyAll()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(verified) != 2 {
		t.Errorf("verified relays: got %d, want 2", len(verified))
	}
}

func TestVerifyAll_NoneValid(t *testing.T) {
	masterPub, _, _ := testRegistry(t, 0)
	// Create entries with bad signatures.
	badEntry := RelayEntry{
		ID:        "bad",
		Domain:    "bad.example.com",
		PublicKey: base64.StdEncoding.EncodeToString(make([]byte, ed25519.PublicKeySize)),
		Signature: base64.StdEncoding.EncodeToString(make([]byte, 64)),
	}

	reg := &Registry{
		Version:         1,
		MasterPublicKey: base64.StdEncoding.EncodeToString(masterPub),
		Relays:          []RelayEntry{badEntry},
	}

	_, err := reg.VerifyAll()
	if !errors.Is(err, ErrNoValidRelays) {
		t.Errorf("expected ErrNoValidRelays, got %v", err)
	}
}
