package crypto

import (
	"crypto/ed25519"
	"crypto/x509"
	"errors"
	"fmt"
)

// ErrPinningFailed is returned when the certificate's public key does not
// match the pinned Ed25519 key.
var ErrPinningFailed = errors.New("crypto: certificate pinning failed: key mismatch")

// VerifyEd25519CertPin checks that the leaf certificate presented by the server
// contains an Ed25519 public key that matches pinnedPubKey exactly.
// Returns ErrPinningFailed if the key does not match.
// Returns a non-ErrPinningFailed error if the certificate does not use Ed25519.
func VerifyEd25519CertPin(cert *x509.Certificate, pinnedPubKey ed25519.PublicKey) error {
	certPubKey, ok := cert.PublicKey.(ed25519.PublicKey)
	if !ok {
		return fmt.Errorf("crypto: pinning: certificate does not use Ed25519 key")
	}
	if !pinnedPubKey.Equal(certPubKey) {
		return ErrPinningFailed
	}
	return nil
}
