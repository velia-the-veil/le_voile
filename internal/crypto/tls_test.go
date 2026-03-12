package crypto

import (
	"crypto/ed25519"
	"crypto/tls"
	"crypto/x509"
	"encoding/pem"
	"testing"
)

func TestGenerateSelfSignedTLSCert_Valid(t *testing.T) {
	_, priv, err := GenerateKeyPair()
	if err != nil {
		t.Fatalf("generate key pair: %v", err)
	}

	certPEM, keyPEM, err := GenerateSelfSignedTLSCert(priv)
	if err != nil {
		t.Fatalf("generate cert: %v", err)
	}

	if len(certPEM) == 0 {
		t.Fatal("certPEM is empty")
	}
	if len(keyPEM) == 0 {
		t.Fatal("keyPEM is empty")
	}

	// Verify round-trip: load as tls.Certificate
	tlsCert, err := tls.X509KeyPair(certPEM, keyPEM)
	if err != nil {
		t.Fatalf("X509KeyPair: %v", err)
	}

	// Verify the certificate uses Ed25519
	block, _ := pem.Decode(certPEM)
	if block == nil {
		t.Fatal("failed to decode cert PEM")
	}

	cert, err := x509.ParseCertificate(block.Bytes)
	if err != nil {
		t.Fatalf("parse certificate: %v", err)
	}

	if cert.PublicKeyAlgorithm != x509.Ed25519 {
		t.Errorf("expected Ed25519 algorithm, got %v", cert.PublicKeyAlgorithm)
	}

	if cert.Subject.CommonName != "levoile.dev" {
		t.Errorf("expected CN levoile.dev, got %q", cert.Subject.CommonName)
	}

	// Verify private key type
	if _, ok := tlsCert.PrivateKey.(ed25519.PrivateKey); !ok {
		t.Errorf("expected ed25519.PrivateKey, got %T", tlsCert.PrivateKey)
	}
}

func TestGenerateSelfSignedTLSCert_NilKey(t *testing.T) {
	_, _, err := GenerateSelfSignedTLSCert(nil)
	if err == nil {
		t.Fatal("expected error for nil key")
	}
}

func TestGenerateSelfSignedTLSCert_InvalidKey(t *testing.T) {
	_, _, err := GenerateSelfSignedTLSCert(ed25519.PrivateKey([]byte("short")))
	if err == nil {
		t.Fatal("expected error for invalid key")
	}
}

func TestGenerateSelfSignedTLSCert_DNSNames(t *testing.T) {
	_, priv, err := GenerateKeyPair()
	if err != nil {
		t.Fatalf("generate key pair: %v", err)
	}

	certPEM, _, err := GenerateSelfSignedTLSCert(priv)
	if err != nil {
		t.Fatalf("generate cert: %v", err)
	}

	block, _ := pem.Decode(certPEM)
	cert, err := x509.ParseCertificate(block.Bytes)
	if err != nil {
		t.Fatalf("parse certificate: %v", err)
	}

	expectedDNS := map[string]bool{"levoile.dev": false, "localhost": false}
	for _, name := range cert.DNSNames {
		if _, ok := expectedDNS[name]; ok {
			expectedDNS[name] = true
		}
	}

	for name, found := range expectedDNS {
		if !found {
			t.Errorf("expected DNS name %q not found in certificate", name)
		}
	}
}
