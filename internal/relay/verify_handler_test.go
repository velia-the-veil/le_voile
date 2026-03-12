package relay

import (
	"bytes"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	lecrypto "github.com/velia-the-veil/le_voile/internal/crypto"
)

func TestVerifyHandler_ValidNonce(t *testing.T) {
	pub, priv, err := lecrypto.GenerateKeyPair()
	if err != nil {
		t.Fatalf("generate key pair: %v", err)
	}

	handler := NewVerifyHandler(priv)

	nonce := make([]byte, 32)
	if _, err := rand.Read(nonce); err != nil {
		t.Fatalf("generate nonce: %v", err)
	}

	body, _ := json.Marshal(VerifyRequest{Nonce: base64.StdEncoding.EncodeToString(nonce)})
	req := httptest.NewRequest(http.MethodPost, "/verify", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}

	var resp VerifyResponse
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}

	sig, err := base64.StdEncoding.DecodeString(resp.Signature)
	if err != nil {
		t.Fatalf("decode signature: %v", err)
	}

	if !lecrypto.Verify(pub, nonce, sig) {
		t.Error("signature verification failed")
	}
}

func TestVerifyHandler_InvalidMethod(t *testing.T) {
	_, priv, _ := lecrypto.GenerateKeyPair()
	handler := NewVerifyHandler(priv)

	req := httptest.NewRequest(http.MethodGet, "/verify", nil)
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusMethodNotAllowed {
		t.Errorf("status = %d, want 405", rec.Code)
	}
}

func TestVerifyHandler_InvalidNonce(t *testing.T) {
	_, priv, _ := lecrypto.GenerateKeyPair()
	handler := NewVerifyHandler(priv)

	// Nonce too short (16 bytes instead of 32)
	shortNonce := make([]byte, 16)
	body, _ := json.Marshal(VerifyRequest{Nonce: base64.StdEncoding.EncodeToString(shortNonce)})
	req := httptest.NewRequest(http.MethodPost, "/verify", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400", rec.Code)
	}
}

func TestVerifyHandler_EmptyBody(t *testing.T) {
	_, priv, _ := lecrypto.GenerateKeyPair()
	handler := NewVerifyHandler(priv)

	req := httptest.NewRequest(http.MethodPost, "/verify", bytes.NewReader([]byte{}))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400", rec.Code)
	}
}
