package relay

import (
	"bytes"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	lecrypto "github.com/velia-the-veil/le_voile/internal/crypto"
)

func TestVerifyHandler_InvalidJSON(t *testing.T) {
	_, priv, _ := lecrypto.GenerateKeyPair()
	handler := NewVerifyHandler(priv)

	tests := []struct {
		name string
		body string
	}{
		{"truncated_json", `{"nonce": "abc`},
		{"not_json", `this is not json`},
		{"trailing_comma", `{"nonce": "abc",}`},
		{"bare_string", `"just a string"`},
		{"array_instead_of_object", `["abc"]`},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodPost, "/verify", strings.NewReader(tt.body))
			req.Header.Set("Content-Type", "application/json")
			rec := httptest.NewRecorder()

			handler.ServeHTTP(rec, req)

			if rec.Code != http.StatusBadRequest {
				t.Errorf("status = %d, want 400 for malformed JSON %q", rec.Code, tt.body)
			}
		})
	}
}

func TestVerifyHandler_MissingNonce(t *testing.T) {
	_, priv, _ := lecrypto.GenerateKeyPair()
	handler := NewVerifyHandler(priv)

	tests := []struct {
		name string
		body string
	}{
		{"empty_nonce_field", `{"nonce": ""}`},
		{"no_nonce_field", `{"other": "value"}`},
		{"nonce_null", `{"nonce": null}`},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodPost, "/verify", strings.NewReader(tt.body))
			req.Header.Set("Content-Type", "application/json")
			rec := httptest.NewRecorder()

			handler.ServeHTTP(rec, req)

			if rec.Code != http.StatusBadRequest {
				t.Errorf("status = %d, want 400 for body %q", rec.Code, tt.body)
			}
		})
	}
}

func TestVerifyHandler_BodyTooLarge(t *testing.T) {
	_, priv, _ := lecrypto.GenerateKeyPair()
	handler := NewVerifyHandler(priv)

	// Create a JSON body with an extremely large nonce value (1MB of base64 data).
	// The handler should still reject it because the decoded nonce won't be 32 bytes.
	largePayload := make([]byte, 1<<20) // 1MB
	for i := range largePayload {
		largePayload[i] = 'A'
	}
	body, _ := json.Marshal(VerifyRequest{Nonce: string(largePayload)})

	req := httptest.NewRequest(http.MethodPost, "/verify", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400 for oversized body", rec.Code)
	}
}

// makeValidVerifyBody creates a valid /verify request body with a random 32-byte nonce.
func makeValidVerifyBody(t *testing.T) []byte {
	t.Helper()
	nonce := make([]byte, 32)
	if _, err := rand.Read(nonce); err != nil {
		t.Fatalf("generate nonce: %v", err)
	}
	body, _ := json.Marshal(VerifyRequest{Nonce: base64.StdEncoding.EncodeToString(nonce)})
	return body
}

func TestVerifyHandler_IssuesTokenBoundToRemoteAddr(t *testing.T) {
	_, priv, _ := lecrypto.GenerateKeyPair()
	handler := NewVerifyHandler(priv)

	body := makeValidVerifyBody(t)
	req := httptest.NewRequest(http.MethodPost, "/verify", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.RemoteAddr = "203.0.113.42:12345"
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}

	// Save body BEFORE decode — json.Decoder consumes the buffer.
	bodyStr := rec.Body.String()

	var resp VerifyResponse
	if err := json.Unmarshal([]byte(bodyStr), &resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if resp.SessionToken == "" {
		t.Error("session_token empty — direct-origin relay should always issue tokens bound to RemoteAddr")
	}
	if resp.Signature == "" {
		t.Error("signature empty")
	}
	// Verify no IP leaks in response body (NFR20).
	if strings.Contains(bodyStr, "203.0.113.42") {
		t.Error("response body contains client IP — NFR20 violation")
	}
}

func TestVerifyHandler_WrongContentType(t *testing.T) {
	_, priv, _ := lecrypto.GenerateKeyPair()
	handler := NewVerifyHandler(priv)

	nonce := make([]byte, 32)
	body, _ := json.Marshal(VerifyRequest{Nonce: base64.StdEncoding.EncodeToString(nonce)})

	contentTypes := []struct {
		name        string
		contentType string
	}{
		{"text_plain", "text/plain"},
		{"text_html", "text/html"},
		{"multipart_form", "multipart/form-data"},
		{"empty", ""},
		{"xml", "application/xml"},
		{"form_urlencoded", "application/x-www-form-urlencoded"},
	}

	for _, tt := range contentTypes {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodPost, "/verify", bytes.NewReader(body))
			if tt.contentType != "" {
				req.Header.Set("Content-Type", tt.contentType)
			}
			rec := httptest.NewRecorder()

			handler.ServeHTTP(rec, req)

			if rec.Code != http.StatusBadRequest {
				t.Errorf("status = %d, want 400 for content-type %q", rec.Code, tt.contentType)
			}
		})
	}
}
