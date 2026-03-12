package crypto

import (
	"crypto/ed25519"
	"errors"
	"testing"
)

func TestEd25519_GenerateKeyPair(t *testing.T) {
	pub, priv, err := GenerateKeyPair()
	if err != nil {
		t.Fatalf("GenerateKeyPair() error: %v", err)
	}
	if len(pub) != ed25519.PublicKeySize {
		t.Errorf("public key size = %d, want %d", len(pub), ed25519.PublicKeySize)
	}
	if len(priv) != ed25519.PrivateKeySize {
		t.Errorf("private key size = %d, want %d", len(priv), ed25519.PrivateKeySize)
	}
}

func TestEd25519_ExportImportPublicKey(t *testing.T) {
	pub, _, err := GenerateKeyPair()
	if err != nil {
		t.Fatalf("GenerateKeyPair() error: %v", err)
	}

	encoded := ExportPublicKeyBase64(pub)
	if encoded == "" {
		t.Fatal("ExportPublicKeyBase64() returned empty string")
	}

	decoded, err := ImportPublicKeyBase64(encoded)
	if err != nil {
		t.Fatalf("ImportPublicKeyBase64() error: %v", err)
	}

	if !pub.Equal(decoded) {
		t.Error("round-trip public key does not match original")
	}
}

func TestEd25519_SignAndVerify(t *testing.T) {
	pub, priv, err := GenerateKeyPair()
	if err != nil {
		t.Fatalf("GenerateKeyPair() error: %v", err)
	}

	message := []byte("le voile de velia")
	sig, err := Sign(priv, message)
	if err != nil {
		t.Fatalf("Sign() error: %v", err)
	}

	if !Verify(pub, message, sig) {
		t.Error("Verify() returned false for valid signature")
	}
}

func TestEd25519_VerifyWrongKey(t *testing.T) {
	_, priv, err := GenerateKeyPair()
	if err != nil {
		t.Fatalf("GenerateKeyPair() error: %v", err)
	}

	otherPub, _, err := GenerateKeyPair()
	if err != nil {
		t.Fatalf("GenerateKeyPair() error: %v", err)
	}

	message := []byte("le voile de velia")
	sig, err := Sign(priv, message)
	if err != nil {
		t.Fatalf("Sign() error: %v", err)
	}

	if Verify(otherPub, message, sig) {
		t.Error("Verify() returned true with wrong public key")
	}
}

func TestEd25519_VerifyTamperedMessage(t *testing.T) {
	pub, priv, err := GenerateKeyPair()
	if err != nil {
		t.Fatalf("GenerateKeyPair() error: %v", err)
	}

	message := []byte("le voile de velia")
	sig, err := Sign(priv, message)
	if err != nil {
		t.Fatalf("Sign() error: %v", err)
	}

	tampered := []byte("le voile de velia modifie")
	if Verify(pub, tampered, sig) {
		t.Error("Verify() returned true for tampered message")
	}
}

func TestEd25519_ImportInvalidBase64(t *testing.T) {
	tests := []struct {
		name      string
		input     string
		wantErr   bool
		checkSent bool // whether to check for ErrInvalidKey sentinel
	}{
		{
			name:      "invalid base64 characters",
			input:     "!!!not-valid-base64!!!",
			wantErr:   true,
			checkSent: false,
		},
		{
			name:      "valid base64 but wrong size",
			input:     "AQID",
			wantErr:   true,
			checkSent: true,
		},
		{
			name:      "empty string",
			input:     "",
			wantErr:   true,
			checkSent: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := ImportPublicKeyBase64(tt.input)
			if (err != nil) != tt.wantErr {
				t.Errorf("ImportPublicKeyBase64(%q) error = %v, wantErr %v", tt.input, err, tt.wantErr)
			}
			if tt.checkSent && !errors.Is(err, ErrInvalidKey) {
				t.Errorf("ImportPublicKeyBase64(%q) error = %v, want ErrInvalidKey", tt.input, err)
			}
		})
	}
}

func TestEd25519_SignNilKey(t *testing.T) {
	_, err := Sign(nil, []byte("test"))
	if err == nil {
		t.Error("Sign(nil, msg) should return error, got nil")
	}
}

func TestEd25519_VerifyNilKey(t *testing.T) {
	if Verify(nil, []byte("test"), []byte("fakesig")) {
		t.Error("Verify(nil, msg, sig) should return false")
	}
}

func TestEd25519_SignAndVerifyEmptyMessage(t *testing.T) {
	pub, priv, err := GenerateKeyPair()
	if err != nil {
		t.Fatalf("GenerateKeyPair() error: %v", err)
	}

	sig, err := Sign(priv, []byte{})
	if err != nil {
		t.Fatalf("Sign() error: %v", err)
	}

	if !Verify(pub, []byte{}, sig) {
		t.Error("Verify() returned false for valid signature of empty message")
	}
}
