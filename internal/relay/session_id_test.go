package relay

import (
	"testing"
	"time"
)

// TestNewSessionID_Uniqueness sanity-checks fix H5: two back-to-back calls
// produce distinct IDs with ~128 bits of entropy. Collision probability in
// this loop is negligible — a failure here means crypto/rand is not
// producing random data, which is catastrophic for every other crypto path.
func TestNewSessionID_Uniqueness(t *testing.T) {
	seen := make(map[string]struct{}, 1024)
	for i := 0; i < 1024; i++ {
		id, err := newSessionID()
		if err != nil {
			t.Fatalf("newSessionID[%d]: %v", i, err)
		}
		if len(id) != 32 {
			t.Fatalf("newSessionID[%d] len = %d, want 32", i, len(id))
		}
		if _, dup := seen[id]; dup {
			t.Fatalf("newSessionID collision at iteration %d: %s", i, id)
		}
		seen[id] = struct{}{}
	}
}

// TestSessionKey_PrefersID checks that sessionKey uses the cryptographic ID
// when present, instead of the legacy predictable IPHash@UnixNano format.
// Without this, H5 remains open: an attacker with a stopwatch and the
// client's IP hash could still guess other sessions.
func TestSessionKey_PrefersID(t *testing.T) {
	s := TunnelSession{
		ID:           "deadbeefcafebabe0011223344556677",
		ClientIPHash: "aabbcc",
		OpenedAt:     time.Unix(1700000000, 0),
	}
	got := sessionKey(s)
	if got != "deadbeefcafebabe0011223344556677" {
		t.Errorf("sessionKey = %q, want the random ID", got)
	}

	// Legacy fallback still works for test literals without ID populated.
	s2 := TunnelSession{ClientIPHash: "aabbcc", OpenedAt: time.Unix(1700000000, 0)}
	legacy := sessionKey(s2)
	if legacy == "" {
		t.Error("sessionKey on ID-less session must still produce a non-empty legacy key")
	}
	if legacy == "deadbeefcafebabe0011223344556677" {
		t.Error("legacy fallback must not collide with the ID path")
	}
}
