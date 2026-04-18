// Package crypto provides Ed25519 cryptographic operations for relay authentication.
package crypto

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"
)

// ErrInvalidKey is returned when a public key has an invalid format or size.
var ErrInvalidKey = errors.New("crypto: invalid public key")

// GenerateKeyPair generates a new Ed25519 key pair using a cryptographically
// secure random number generator.
func GenerateKeyPair() (ed25519.PublicKey, ed25519.PrivateKey, error) {
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		return nil, nil, fmt.Errorf("crypto: generate key pair: %w", err)
	}
	return pub, priv, nil
}

// ExportPublicKeyBase64 encodes an Ed25519 public key to a base64 standard string.
func ExportPublicKeyBase64(pub ed25519.PublicKey) string {
	return base64.StdEncoding.EncodeToString(pub)
}

// ImportPublicKeyBase64 decodes a base64 standard string into an Ed25519 public key.
// Returns ErrInvalidKey if the decoded data is not exactly 32 bytes.
func ImportPublicKeyBase64(encoded string) (ed25519.PublicKey, error) {
	decoded, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil {
		return nil, fmt.Errorf("crypto: import key: %w", err)
	}
	if len(decoded) != ed25519.PublicKeySize {
		return nil, ErrInvalidKey
	}
	return ed25519.PublicKey(decoded), nil
}

// ExportPrivateKeyBase64 encodes an Ed25519 private key (64 bytes) to a
// base64 standard string. Used by cmd/genkey to persist the release master
// key (story 7.4) and by cmd/signpkg/cmd/genregistry to load it back.
func ExportPrivateKeyBase64(priv ed25519.PrivateKey) string {
	return base64.StdEncoding.EncodeToString(priv)
}

// ImportPrivateKeyBase64 decodes a base64 standard string into an Ed25519
// private key. Returns ErrInvalidKey if the decoded data is not exactly
// ed25519.PrivateKeySize bytes (64).
func ImportPrivateKeyBase64(encoded string) (ed25519.PrivateKey, error) {
	decoded, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil {
		return nil, fmt.Errorf("crypto: import private key: %w", err)
	}
	if len(decoded) != ed25519.PrivateKeySize {
		return nil, ErrInvalidKey
	}
	return ed25519.PrivateKey(decoded), nil
}

// Sign signs a message with the given Ed25519 private key.
// Returns an error if the private key is nil or has an invalid size.
func Sign(priv ed25519.PrivateKey, message []byte) ([]byte, error) {
	if len(priv) != ed25519.PrivateKeySize {
		return nil, fmt.Errorf("crypto: sign: %w", ErrInvalidKey)
	}
	return ed25519.Sign(priv, message), nil
}

// Verify reports whether sig is a valid Ed25519 signature of message by pub.
// Returns false if the public key is nil or has an invalid size.
func Verify(pub ed25519.PublicKey, message []byte, sig []byte) bool {
	if len(pub) != ed25519.PublicKeySize {
		return false
	}
	return ed25519.Verify(pub, message, sig)
}
