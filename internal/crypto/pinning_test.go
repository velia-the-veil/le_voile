package crypto

import (
	"crypto/ecdsa"
	"crypto/ed25519"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"math/big"
	"testing"
	"time"
)

// generateTestEd25519Cert creates a self-signed Ed25519 certificate for testing.
func generateTestEd25519Cert(t *testing.T, pubKey ed25519.PublicKey, privKey ed25519.PrivateKey) *x509.Certificate {
	t.Helper()
	serial, err := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 128))
	if err != nil {
		t.Fatalf("generate serial: %v", err)
	}
	template := &x509.Certificate{
		SerialNumber: serial,
		Subject:      pkix.Name{CommonName: "test"},
		NotBefore:    time.Now().Add(-time.Minute),
		NotAfter:     time.Now().Add(time.Hour),
	}
	certDER, err := x509.CreateCertificate(rand.Reader, template, template, pubKey, privKey)
	if err != nil {
		t.Fatalf("create cert: %v", err)
	}
	cert, err := x509.ParseCertificate(certDER)
	if err != nil {
		t.Fatalf("parse cert: %v", err)
	}
	return cert
}

func TestVerifyEd25519CertPin_Valid(t *testing.T) {
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}
	cert := generateTestEd25519Cert(t, pub, priv)

	if err := VerifyEd25519CertPin(cert, pub); err != nil {
		t.Errorf("expected nil, got %v", err)
	}
}

func TestVerifyEd25519CertPin_WrongKey(t *testing.T) {
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("generate key pair: %v", err)
	}
	wrongPub, _, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("generate wrong key: %v", err)
	}
	cert := generateTestEd25519Cert(t, pub, priv)

	err = VerifyEd25519CertPin(cert, wrongPub)
	if err == nil {
		t.Fatal("expected error for wrong key, got nil")
	}
	if err != ErrPinningFailed {
		t.Errorf("expected ErrPinningFailed, got %v", err)
	}
}

// TestVerifyEd25519CertPin_KeyDifferingOnlyInLastByte ensures the comparison
// does not short-circuit on a matching prefix. This is a regression guard: if
// someone replaces ed25519.PublicKey.Equal with bytes.Equal the behavior is
// still correct, but if they replace it with a naïve early-exit loop the test
// will still pass — what this test really guards is that keys differing only
// in their last byte are rejected. Timing-attack resistance itself is provided
// by crypto/subtle.ConstantTimeCompare inside ed25519.PublicKey.Equal (NFR9c);
// see pinning.go godoc.
func TestVerifyEd25519CertPin_KeyDifferingOnlyInLastByte(t *testing.T) {
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}
	cert := generateTestEd25519Cert(t, pub, priv)

	// Flip only the last byte of the pinned key.
	tampered := make(ed25519.PublicKey, len(pub))
	copy(tampered, pub)
	tampered[len(tampered)-1] ^= 0x01

	if err := VerifyEd25519CertPin(cert, tampered); err != ErrPinningFailed {
		t.Errorf("expected ErrPinningFailed for last-byte diff, got %v", err)
	}
}

func TestVerifyEd25519CertPin_NonEd25519Cert(t *testing.T) {
	ecKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("generate EC key: %v", err)
	}
	serial, err := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 128))
	if err != nil {
		t.Fatalf("generate serial: %v", err)
	}
	template := &x509.Certificate{
		SerialNumber: serial,
		Subject:      pkix.Name{CommonName: "test-ecdsa"},
		NotBefore:    time.Now().Add(-time.Minute),
		NotAfter:     time.Now().Add(time.Hour),
	}
	certDER, err := x509.CreateCertificate(rand.Reader, template, template, ecKey.Public(), ecKey)
	if err != nil {
		t.Fatalf("create ECDSA cert: %v", err)
	}
	cert, err := x509.ParseCertificate(certDER)
	if err != nil {
		t.Fatalf("parse cert: %v", err)
	}

	// Any Ed25519 pinned key should fail since the cert is ECDSA
	anyPub, _, _ := ed25519.GenerateKey(rand.Reader)
	err = VerifyEd25519CertPin(cert, anyPub)
	if err == nil {
		t.Fatal("expected error for non-Ed25519 cert, got nil")
	}
	if err == ErrPinningFailed {
		t.Error("expected non-ErrPinningFailed error for non-Ed25519 cert (wrong error type)")
	}
}
