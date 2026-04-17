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

func TestVerifyHandler_Forbidden_NonCloudflareSource(t *testing.T) {
	_, priv, _ := lecrypto.GenerateKeyPair()
	handler := NewVerifyHandler(priv)
	// Strict mode: insecure=false.
	cfv := NewCloudflareIPValidator(false, nil)
	handler.SetCFValidator(cfv)

	body := makeValidVerifyBody(t)
	req := httptest.NewRequest(http.MethodPost, "/verify", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	// Non-CF source IP.
	req.RemoteAddr = "8.8.8.8:443"
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusForbidden {
		t.Errorf("status = %d, want 403 for non-CF source in strict mode", rec.Code)
	}
	// Verify response body does not contain the source IP (NFR20).
	if strings.Contains(rec.Body.String(), "8.8.8.8") {
		t.Error("response body contains source IP — NFR20 violation")
	}
}

func TestVerifyHandler_Forbidden_CloudflareSourceMissingCFHeader(t *testing.T) {
	_, priv, _ := lecrypto.GenerateKeyPair()
	handler := NewVerifyHandler(priv)
	cfv := NewCloudflareIPValidator(false, nil)
	handler.SetCFValidator(cfv)

	body := makeValidVerifyBody(t)
	req := httptest.NewRequest(http.MethodPost, "/verify", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	// CF source IP (104.16.0.0/13 range), but no CF-Connecting-IP header.
	req.RemoteAddr = "104.16.1.1:443"
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusForbidden {
		t.Errorf("status = %d, want 403 for CF source without CF-Connecting-IP", rec.Code)
	}
	// Verify response body does not contain the source IP (NFR20).
	if strings.Contains(rec.Body.String(), "104.16.1.1") {
		t.Error("response body contains source IP — NFR20 violation")
	}
}

func TestVerifyHandler_Forbidden_InvalidCFHeader(t *testing.T) {
	_, priv, _ := lecrypto.GenerateKeyPair()
	handler := NewVerifyHandler(priv)
	cfv := NewCloudflareIPValidator(false, nil)
	handler.SetCFValidator(cfv)

	body := makeValidVerifyBody(t)
	req := httptest.NewRequest(http.MethodPost, "/verify", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.RemoteAddr = "104.16.1.1:443"
	req.Header.Set("CF-Connecting-IP", "not-an-ip")
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusForbidden {
		t.Errorf("status = %d, want 403 for invalid CF-Connecting-IP", rec.Code)
	}
	// Verify response body does not contain the source IP or invalid header (NFR20).
	respBody := rec.Body.String()
	if strings.Contains(respBody, "104.16.1.1") || strings.Contains(respBody, "not-an-ip") {
		t.Error("response body contains source IP or CF header value — NFR20 violation")
	}
}

func TestVerifyHandler_StrictMode_HappyPath(t *testing.T) {
	_, priv, _ := lecrypto.GenerateKeyPair()
	handler := NewVerifyHandler(priv)
	// Strict mode: insecure=false.
	cfv := NewCloudflareIPValidator(false, nil)
	handler.SetCFValidator(cfv)

	body := makeValidVerifyBody(t)
	req := httptest.NewRequest(http.MethodPost, "/verify", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	// CF source IP with valid CF-Connecting-IP.
	req.RemoteAddr = "104.16.1.1:443"
	req.Header.Set("CF-Connecting-IP", "203.0.113.42")
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 for valid CF source in strict mode", rec.Code)
	}

	bodyStr := rec.Body.String()

	var resp VerifyResponse
	if err := json.Unmarshal([]byte(bodyStr), &resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if resp.SessionToken == "" {
		t.Error("session_token empty — strict mode with valid CF source should issue token (AC1)")
	}
	if resp.Signature == "" {
		t.Error("signature empty")
	}
	// Verify no IP leaks in response body (NFR20).
	if strings.Contains(bodyStr, "203.0.113.42") || strings.Contains(bodyStr, "104.16.1.1") {
		t.Error("response body contains client or source IP — NFR20 violation")
	}
}

func TestVerifyHandler_InsecureMode_StillIssuesToken(t *testing.T) {
	_, priv, _ := lecrypto.GenerateKeyPair()
	handler := NewVerifyHandler(priv)
	// Insecure/dev mode: trust all sources.
	cfv := NewCloudflareIPValidator(true, nil)
	handler.SetCFValidator(cfv)

	body := makeValidVerifyBody(t)
	req := httptest.NewRequest(http.MethodPost, "/verify", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.RemoteAddr = "192.168.1.100:12345"
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 in insecure mode", rec.Code)
	}

	// Save body BEFORE decode — json.Decoder consumes the buffer.
	bodyStr := rec.Body.String()

	var resp VerifyResponse
	if err := json.Unmarshal([]byte(bodyStr), &resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if resp.SessionToken == "" {
		t.Error("session_token empty — insecure mode should still issue tokens (AC7)")
	}
	if resp.Signature == "" {
		t.Error("signature empty")
	}
	// Verify no IP leaks in response body.
	if strings.Contains(bodyStr, "192.168.1.100") {
		t.Error("response body contains RemoteAddr IP")
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
