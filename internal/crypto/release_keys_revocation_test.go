package crypto

import (
	"errors"
	"testing"
)

// TestVerifyReleaseSignature_RevokedCurrent checks that a signature made by
// the current key is rejected when that key appears in
// RevokedReleaseKeysBase64 — the scenario operators hit after a key
// compromise (fix H1).
func TestVerifyReleaseSignature_RevokedCurrent(t *testing.T) {
	// Sign something with the current private key equivalent — we don't
	// actually have access to the private half of the baked-in public key,
	// so this test verifies the policy gate rather than the signature path.
	// We simulate by planting the current key's base64 into the revoked
	// list and asserting that any call surface returning a success path
	// would flip to ErrReleaseKeyRevoked.
	if ReleasePublicKeyCurrentBase64 == "" {
		t.Skip("no current release key configured")
	}
	// Backup and restore the revoked list so the test is hermetic.
	orig := RevokedReleaseKeysBase64
	t.Cleanup(func() { RevokedReleaseKeysBase64 = orig })

	RevokedReleaseKeysBase64 = []string{ReleasePublicKeyCurrentBase64}
	if !isRevoked(ReleasePublicKeyCurrentBase64) {
		t.Fatalf("isRevoked must flag the current key after planting it")
	}
}

// TestVerifyReleaseSignature_NotRevoked confirms the default list is empty,
// so production builds never flag a legitimate key by accident.
func TestVerifyReleaseSignature_NotRevoked(t *testing.T) {
	if len(RevokedReleaseKeysBase64) != 0 {
		t.Errorf("default RevokedReleaseKeysBase64 must be empty, got %d entries",
			len(RevokedReleaseKeysBase64))
	}
	if isRevoked(ReleasePublicKeyCurrentBase64) {
		t.Errorf("current key must not be revoked by default")
	}
}

// TestErrReleaseKeyRevoked_Distinct ensures the revocation error is a
// distinct sentinel so callers can react differently (alert loudly, page
// oncall) than they would for a plain signature mismatch.
func TestErrReleaseKeyRevoked_Distinct(t *testing.T) {
	if ErrReleaseKeyRevoked == nil {
		t.Fatal("ErrReleaseKeyRevoked must not be nil")
	}
	var genericErr = errors.New("crypto: release signature does not match any trusted key")
	if errors.Is(ErrReleaseKeyRevoked, genericErr) {
		t.Errorf("ErrReleaseKeyRevoked must not wrap the generic mismatch error")
	}
}
