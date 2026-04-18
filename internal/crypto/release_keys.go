package crypto

import (
	"crypto/ed25519"
	"errors"
	"fmt"
)

// ReleasePublicKeyCurrentBase64 is the Ed25519 public key used to verify
// Le Voile release artifacts (story 7.4) and auto-update bundles (story 8.2).
//
// The matching private key lives exclusively on the maintainer's offline
// machine (per NFR22g). Rotated every 24 months per NFR22h via the dual-key
// chain below.
//
// Generated 2026-04-18 via `go run ./cmd/genkey -out "$HOME/.levoile/signing" -pem`.
// The PEM is also published in docs/keys/levoile-release.pub.pem for consumers
// that want to verify via `openssl pkeyutl -verify -rawin`.
const ReleasePublicKeyCurrentBase64 = "h94H7SXEBMr0/OTqxLmepAxax60vhgbbezU0Jt+hbQM="

// ReleasePublicKeyNextBase64 is the upcoming rotation key. Empty when no
// rotation is in flight. During the 6-month dual-signature transition window
// (NFR22h), releases are double-signed and verifiers try Current first,
// then Next on failure.
const ReleasePublicKeyNextBase64 = ""

// ReleasePublicKeyCurrent parses and returns the current release public key.
// Returns ErrInvalidKey if the embedded constant is a placeholder or malformed.
func ReleasePublicKeyCurrent() (ed25519.PublicKey, error) {
	if ReleasePublicKeyCurrentBase64 == "" || ReleasePublicKeyCurrentBase64 == "REPLACE_ME_WITH_MASTER_PUBLIC_KEY" {
		return nil, fmt.Errorf("crypto: release key not provisioned: %w", ErrInvalidKey)
	}
	pub, err := ImportPublicKeyBase64(ReleasePublicKeyCurrentBase64)
	if err != nil {
		return nil, fmt.Errorf("crypto: release current key: %w", err)
	}
	return pub, nil
}

// ReleasePublicKeyNext parses and returns the rotation public key if one is
// configured. Returns (nil, false, nil) when no rotation is active.
// Returns (nil, true, err) when a rotation key is declared but malformed.
func ReleasePublicKeyNext() (ed25519.PublicKey, bool, error) {
	if ReleasePublicKeyNextBase64 == "" {
		return nil, false, nil
	}
	pub, err := ImportPublicKeyBase64(ReleasePublicKeyNextBase64)
	if err != nil {
		return nil, true, fmt.Errorf("crypto: release next key: %w", err)
	}
	return pub, true, nil
}

// VerifyReleaseSignature verifies sig over message using the current release
// public key, then falls back to the rotation key if one is configured. This
// is the canonical verification path for auto-update bundles (Epic 8 story
// 8.2). It always tries Next on Current failure — different from the
// user-facing cmd/verifypkg which defaults to Current only (opt-in -try-next
// for extra safety, since an interactive user can re-run with the flag).
// Auto-update has no user to ask, so it always tries both trust anchors
// during the dual-signature window (NFR22h).
//
// Returns nil on success. Returns a wrapped error indicating neither key
// accepted the signature (or that neither key is provisioned).
func VerifyReleaseSignature(message, sig []byte) error {
	current, err := ReleasePublicKeyCurrent()
	if err == nil && Verify(current, message, sig) {
		return nil
	}
	next, hasNext, nextErr := ReleasePublicKeyNext()
	if hasNext && nextErr == nil && Verify(next, message, sig) {
		return nil
	}
	if err != nil && !hasNext {
		return err
	}
	return errors.New("crypto: release signature does not match any trusted key")
}
