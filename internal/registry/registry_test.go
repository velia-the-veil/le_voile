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
		msg := append([]byte(SignaturePrefix), relayPub...)
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

// TestVerifyAllWithLogger_MixedValidity covers AC3 of Story 4.1: each rejected
// entry triggers exactly one logger callback with id/domain/reason, and valid
// entries are returned. Logger never receives binary content.
func TestVerifyAllWithLogger_MixedValidity(t *testing.T) {
	masterPub, _, entries := testRegistry(t, 2)

	badSigEntry := RelayEntry{
		ID:        "bad-sig",
		Domain:    "bad-sig.example.com",
		PublicKey: entries[0].PublicKey,
		Signature: base64.StdEncoding.EncodeToString(make([]byte, 64)),
	}
	badPubKeyEntry := RelayEntry{
		ID:        "bad-pub",
		Domain:    "bad-pub.example.com",
		PublicKey: "not-valid-base64!!!",
		Signature: entries[0].Signature,
	}
	badSigB64Entry := RelayEntry{
		ID:        "bad-sig-b64",
		Domain:    "bad-sig-b64.example.com",
		PublicKey: entries[0].PublicKey,
		Signature: "not-valid-base64!!!",
	}

	reg := &Registry{
		Version:         1,
		MasterPublicKey: base64.StdEncoding.EncodeToString(masterPub),
		Relays:          append(entries, badSigEntry, badPubKeyEntry, badSigB64Entry),
	}

	type rejection struct {
		id, domain, reason string
	}
	var got []rejection
	logger := func(id, domain, reason string) {
		got = append(got, rejection{id, domain, reason})
	}

	verified, err := reg.VerifyAllWithLogger(logger)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(verified) != 2 {
		t.Errorf("verified: got %d, want 2", len(verified))
	}

	want := map[string]string{
		"bad-sig":     RejectReasonInvalidSignature,
		"bad-pub":     RejectReasonDecodePublicKey,
		"bad-sig-b64": RejectReasonDecodeSignature,
	}
	if len(got) != len(want) {
		t.Fatalf("logger callbacks: got %d, want %d (%+v)", len(got), len(want), got)
	}
	for _, r := range got {
		expectedReason, ok := want[r.id]
		if !ok {
			t.Errorf("unexpected logger callback for id=%q", r.id)
			continue
		}
		if r.reason != expectedReason {
			t.Errorf("id=%q: reason got %q, want %q", r.id, r.reason, expectedReason)
		}
		if r.domain == "" {
			t.Errorf("id=%q: domain must be populated", r.id)
		}
	}
}

// TestVerifyAllWithLogger_NilLoggerMatchesVerifyAll documents that passing nil
// is equivalent to calling VerifyAll — needed for AC3 backward compatibility.
func TestVerifyAllWithLogger_NilLoggerMatchesVerifyAll(t *testing.T) {
	masterPub, _, entries := testRegistry(t, 2)
	badEntry := RelayEntry{
		ID:        "bad",
		Domain:    "bad.example.com",
		PublicKey: entries[0].PublicKey,
		Signature: base64.StdEncoding.EncodeToString(make([]byte, 64)),
	}
	reg := &Registry{
		Version:         1,
		MasterPublicKey: base64.StdEncoding.EncodeToString(masterPub),
		Relays:          append(entries, badEntry),
	}

	a, err1 := reg.VerifyAll()
	b, err2 := reg.VerifyAllWithLogger(nil)
	if err1 != err2 {
		t.Errorf("errors differ: VerifyAll=%v VerifyAllWithLogger(nil)=%v", err1, err2)
	}
	if len(a) != len(b) {
		t.Errorf("verified count differs: %d vs %d", len(a), len(b))
	}
}

// TestVerifyAllWithLogger_AllInvalid — AC3 edge case: if all entries are
// invalid, each triggers a callback and ErrNoValidRelays is returned.
func TestVerifyAllWithLogger_AllInvalid(t *testing.T) {
	masterPub, _, _ := testRegistry(t, 0)
	badEntry := func(id string) RelayEntry {
		return RelayEntry{
			ID:        id,
			Domain:    id + ".example.com",
			PublicKey: base64.StdEncoding.EncodeToString(make([]byte, ed25519.PublicKeySize)),
			Signature: base64.StdEncoding.EncodeToString(make([]byte, 64)),
		}
	}
	reg := &Registry{
		Version:         1,
		MasterPublicKey: base64.StdEncoding.EncodeToString(masterPub),
		Relays:          []RelayEntry{badEntry("bad-1"), badEntry("bad-2"), badEntry("bad-3")},
	}

	var calls int
	_, err := reg.VerifyAllWithLogger(func(id, domain, reason string) {
		calls++
		if reason != RejectReasonInvalidSignature {
			t.Errorf("reason: got %q, want %q", reason, RejectReasonInvalidSignature)
		}
	})
	if !errors.Is(err, ErrNoValidRelays) {
		t.Errorf("expected ErrNoValidRelays, got %v", err)
	}
	if calls != 3 {
		t.Errorf("logger calls: got %d, want 3", calls)
	}
}
