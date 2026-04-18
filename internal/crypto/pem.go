package crypto

import (
	"crypto/ed25519"
	"crypto/x509"
	"encoding/pem"
	"fmt"
)

// ExportPublicKeyPEM encodes an Ed25519 public key as a PKIX-wrapped PEM
// "PUBLIC KEY" block. The output is interoperable with OpenSSL:
//
//	openssl pkeyutl -verify -rawin -pubin -inkey <this.pem> -sigfile s -in f
//
// Used by story 7.4 to expose the release public key in a format the
// PKGBUILD AUR verify() hook can consume (inlined via heredoc) and that
// any user can use from their shell without extra Go tooling.
func ExportPublicKeyPEM(pub ed25519.PublicKey) ([]byte, error) {
	if len(pub) != ed25519.PublicKeySize {
		return nil, fmt.Errorf("crypto: export pem: %w", ErrInvalidKey)
	}
	der, err := x509.MarshalPKIXPublicKey(pub)
	if err != nil {
		return nil, fmt.Errorf("crypto: export pem: marshal pkix: %w", err)
	}
	return pem.EncodeToMemory(&pem.Block{
		Type:  "PUBLIC KEY",
		Bytes: der,
	}), nil
}

// ImportPublicKeyPEM parses a PKIX-wrapped PEM "PUBLIC KEY" block and
// returns the Ed25519 public key. Returns ErrInvalidKey if the PEM is
// not Ed25519.
func ImportPublicKeyPEM(pemBytes []byte) (ed25519.PublicKey, error) {
	block, _ := pem.Decode(pemBytes)
	if block == nil || block.Type != "PUBLIC KEY" {
		return nil, fmt.Errorf("crypto: import pem: %w", ErrInvalidKey)
	}
	pub, err := x509.ParsePKIXPublicKey(block.Bytes)
	if err != nil {
		return nil, fmt.Errorf("crypto: import pem: parse pkix: %w", err)
	}
	edPub, ok := pub.(ed25519.PublicKey)
	if !ok {
		return nil, fmt.Errorf("crypto: import pem: not ed25519: %w", ErrInvalidKey)
	}
	return edPub, nil
}
