package relay

import (
	"bytes"
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	lecrypto "github.com/velia-the-veil/le_voile/internal/crypto"
)

// testKeys generates a fresh Ed25519 key pair for testing.
func testKeys(t *testing.T) (ed25519.PublicKey, ed25519.PrivateKey) {
	t.Helper()
	pub, priv, err := lecrypto.GenerateKeyPair()
	if err != nil {
		t.Fatalf("generate key pair: %v", err)
	}
	return pub, priv
}

// testToken creates a valid session token for the given clientIP using the private key.
func testToken(t *testing.T, priv ed25519.PrivateKey, clientIP string) string {
	t.Helper()
	token, err := CreateSessionToken(priv, clientIP)
	if err != nil {
		t.Fatalf("create session token: %v", err)
	}
	return token
}

// expiredToken creates a session token with an issue time far in the past so it is expired.
func expiredToken(t *testing.T, priv ed25519.PrivateKey, clientIP string) string {
	t.Helper()
	ipHash := fmt.Sprintf("%x", sha256.Sum256([]byte(clientIP)))
	payload := SessionTokenPayload{
		IPHash: ipHash,
		Issued: time.Now().Add(-5 * time.Hour).Unix(), // issued 5h ago, TTL is 4h
		TTL:    SessionTokenTTL,
	}
	payloadJSON, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("marshal payload: %v", err)
	}
	payloadB64 := base64.RawURLEncoding.EncodeToString(payloadJSON)
	sig, err := lecrypto.Sign(priv, payloadJSON)
	if err != nil {
		t.Fatalf("sign payload: %v", err)
	}
	sigB64 := base64.RawURLEncoding.EncodeToString(sig)
	return payloadB64 + "." + sigB64
}

// newConnectRequest builds a POST request to /connect with Authorization header and JSON body.
func newConnectRequest(t *testing.T, token string, target string) *http.Request {
	t.Helper()
	body, err := json.Marshal(connectRequest{Target: target})
	if err != nil {
		t.Fatalf("marshal body: %v", err)
	}
	req := httptest.NewRequest(http.MethodPost, "/connect", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	return req
}

// newHandler creates a ConnectHandler with insecure CF validator for testing.
func newHandler(t *testing.T, pub ed25519.PublicKey, limiter *IPLimiter) *ConnectHandler {
	t.Helper()
	cfv := NewCloudflareIPValidator(true, nil)
	return NewConnectHandler(pub, cfv, limiter, func(string, ...any) {})
}

func TestConnectHandler_NonPOST(t *testing.T) {
	pub, _ := testKeys(t)
	h := newHandler(t, pub, nil)

	methods := []string{http.MethodGet, http.MethodPut, http.MethodDelete, http.MethodPatch}
	for _, m := range methods {
		t.Run(m, func(t *testing.T) {
			req := httptest.NewRequest(m, "/connect", nil)
			rec := httptest.NewRecorder()
			h.ServeHTTP(rec, req)
			if rec.Code != http.StatusMethodNotAllowed {
				t.Errorf("method %s: status = %d, want 405", m, rec.Code)
			}
		})
	}
}

func TestConnectHandler_MissingBearerToken(t *testing.T) {
	pub, _ := testKeys(t)
	h := newHandler(t, pub, nil)

	body, _ := json.Marshal(connectRequest{Target: "example.com:443"})
	req := httptest.NewRequest(http.MethodPost, "/connect", bytes.NewReader(body))
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Errorf("status = %d, want 401", rec.Code)
	}
}

func TestConnectHandler_InvalidToken(t *testing.T) {
	pub, _ := testKeys(t)
	h := newHandler(t, pub, nil)

	// Token signed by a different key.
	_, otherPriv := testKeys(t)
	token := testToken(t, otherPriv, "1.2.3.4")

	req := newConnectRequest(t, token, "example.com:443")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Errorf("status = %d, want 401", rec.Code)
	}
}

func TestConnectHandler_ExpiredToken(t *testing.T) {
	pub, priv := testKeys(t)
	h := newHandler(t, pub, nil)

	token := expiredToken(t, priv, "127.0.0.1")
	req := newConnectRequest(t, token, "example.com:443")
	// Set RemoteAddr so ExtractClientIP returns the same IP we used in the token.
	req.RemoteAddr = "127.0.0.1:12345"
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Errorf("status = %d, want 401", rec.Code)
	}
}

func TestConnectHandler_TokenIPMismatch(t *testing.T) {
	pub, priv := testKeys(t)
	h := newHandler(t, pub, nil)

	// Token minted for 1.2.3.4 but request comes from 5.6.7.8.
	token := testToken(t, priv, "1.2.3.4")
	req := newConnectRequest(t, token, "example.com:443")
	req.RemoteAddr = "5.6.7.8:12345"
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Errorf("status = %d, want 401", rec.Code)
	}
}

func TestConnectHandler_SSRFBlocked(t *testing.T) {
	pub, priv := testKeys(t)
	h := newHandler(t, pub, nil)

	tests := []struct {
		name   string
		target string
	}{
		{"loopback", "127.0.0.1:80"},
		{"private_10", "10.0.0.1:80"},
		{"private_192", "192.168.1.1:80"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			clientIP := "203.0.113.1"
			token := testToken(t, priv, clientIP)
			req := newConnectRequest(t, token, tt.target)
			req.RemoteAddr = clientIP + ":12345"
			rec := httptest.NewRecorder()
			h.ServeHTTP(rec, req)

			if rec.Code != http.StatusForbidden {
				t.Errorf("target %s: status = %d, want 403", tt.target, rec.Code)
			}
		})
	}
}

func TestConnectHandler_RateLimitExceeded(t *testing.T) {
	pub, priv := testKeys(t)
	// Limiter that allows only 1 concurrent connection per IP.
	limiter := NewIPLimiter(1)
	h := newHandler(t, pub, limiter)

	clientIP := "203.0.113.50"
	token := testToken(t, priv, clientIP)

	// Exhaust the single slot.
	if !limiter.Acquire(clientIP) {
		t.Fatal("failed to acquire initial slot")
	}
	defer limiter.Release(clientIP)

	req := newConnectRequest(t, token, "example.com:443")
	req.RemoteAddr = clientIP + ":12345"
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusTooManyRequests {
		t.Errorf("status = %d, want 429", rec.Code)
	}
}

func TestConnectHandler_ValidRequestPassesAuth(t *testing.T) {
	// This test verifies the full auth pipeline succeeds and the handler
	// proceeds to the SSRF / dial stage. We target a loopback address which
	// will be blocked by SSRF validation (403). Getting 403 (not 401/405/429)
	// proves that authentication, token verification, IP hash check, and rate
	// limiting all passed.
	pub, priv := testKeys(t)
	limiter := NewIPLimiter(IPLimiterMaxPerIP)
	h := newHandler(t, pub, limiter)

	clientIP := "203.0.113.10"
	token := testToken(t, priv, clientIP)

	req := newConnectRequest(t, token, "127.0.0.1:80")
	req.RemoteAddr = clientIP + ":12345"
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusForbidden {
		t.Errorf("status = %d, want 403 (SSRF block proves auth passed)", rec.Code)
	}
}

func TestConnectHandler_ValidRequestE2E(t *testing.T) {
	// Full end-to-end test: start a local TCP echo server on a routable
	// loopback address. Since isBlockedIP blocks 127.x.x.x, we use a real
	// httptest server and verify the handler reaches the dial stage by
	// targeting a public IP (8.8.8.8:53). If the dial succeeds we get 200;
	// if the environment blocks outbound we get 502. Either confirms auth passed.
	pub, priv := testKeys(t)
	cfv := NewCloudflareIPValidator(true, nil)
	h := NewConnectHandler(pub, cfv, nil, func(string, ...any) {})

	srv := httptest.NewServer(h)
	defer srv.Close()

	// The httptest server's client IP will be 127.0.0.1 — create token for that.
	clientIP := "127.0.0.1"
	token := testToken(t, priv, clientIP)

	body, _ := json.Marshal(connectRequest{Target: "8.8.8.8:53"})
	httpReq, err := http.NewRequest(http.MethodPost, srv.URL+"/connect", bytes.NewReader(body))
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Authorization", "Bearer "+token)

	client := &http.Client{Timeout: 5 * time.Second}
	resp, err := client.Do(httpReq)
	if err != nil {
		t.Fatalf("do request: %v", err)
	}
	defer resp.Body.Close()

	// 200 = dial succeeded and relay started; 502 = dial failed (firewall).
	// Both confirm auth passed. Anything else is unexpected.
	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusBadGateway {
		respBody, _ := io.ReadAll(resp.Body)
		t.Errorf("status = %d, want 200 or 502; body: %s", resp.StatusCode, string(respBody))
	}
}

// --- Tests for unexported helpers ---

func TestExtractBearerToken(t *testing.T) {
	tests := []struct {
		name  string
		auth  string
		want  string
	}{
		{"empty", "", ""},
		{"no_prefix", "Token abc123", ""},
		{"basic_auth", "Basic dXNlcjpwYXNz", ""},
		{"bearer_lowercase", "bearer mytoken", "mytoken"},
		{"bearer_mixed_case", "BEARER mytoken", "mytoken"},
		{"bearer_valid", "Bearer mytoken", "mytoken"},
		{"bearer_with_dots", "Bearer abc.def.ghi", "abc.def.ghi"},
		{"just_bearer", "Bearer ", ""},
		{"bearer_no_space", "Bearertoken", ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodPost, "/", nil)
			if tt.auth != "" {
				req.Header.Set("Authorization", tt.auth)
			}
			got := extractBearerToken(req)
			if got != tt.want {
				t.Errorf("extractBearerToken(%q) = %q, want %q", tt.auth, got, tt.want)
			}
		})
	}
}

func TestIsBlockedIP(t *testing.T) {
	tests := []struct {
		name    string
		ip      string
		blocked bool
	}{
		{"loopback_v4", "127.0.0.1", true},
		{"loopback_v6", "::1", true},
		{"private_10", "10.0.0.1", true},
		{"private_172", "172.16.0.1", true},
		{"private_192", "192.168.1.1", true},
		{"link_local_v4", "169.254.1.1", true},
		{"link_local_v6", "fe80::1", true},
		{"unspecified_v4", "0.0.0.0", true},
		{"unspecified_v6", "::", true},
		{"public_v4", "8.8.8.8", false},
		{"public_v4_2", "203.0.113.1", false},
		{"public_v6", "2001:4860:4860::8888", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ip := net.ParseIP(tt.ip)
			if ip == nil {
				t.Fatalf("failed to parse IP %q", tt.ip)
			}
			got := isBlockedIP(ip)
			if got != tt.blocked {
				t.Errorf("isBlockedIP(%s) = %v, want %v", tt.ip, got, tt.blocked)
			}
		})
	}
}

func TestResolveAndValidateConnect(t *testing.T) {
	tests := []struct {
		name    string
		target  string
		wantErr bool
	}{
		{"loopback", "127.0.0.1:80", true},
		{"private_10", "10.0.0.1:80", true},
		{"private_192", "192.168.1.1:80", true},
		{"private_172", "172.16.0.1:443", true},
		{"link_local", "169.254.1.1:80", true},
		{"unspecified", "0.0.0.0:80", true},
		{"empty_host", ":80", true},
		{"empty_port", "example.com:", true},
		{"no_port", "example.com", true},
		// A public IP should succeed (if reachable for resolution).
		{"public_ip", "8.8.8.8:53", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := resolveAndValidateConnect(tt.target)
			if tt.wantErr && err == nil {
				t.Errorf("resolveAndValidateConnect(%q) = nil error, want error", tt.target)
			}
			if !tt.wantErr && err != nil {
				t.Errorf("resolveAndValidateConnect(%q) = %v, want nil error", tt.target, err)
			}
		})
	}
}

func TestConnectHandler_MalformedTokenFormat(t *testing.T) {
	pub, _ := testKeys(t)
	h := newHandler(t, pub, nil)

	tests := []struct {
		name  string
		token string
	}{
		{"garbage", "not-a-valid-token"},
		{"no_dot", base64.RawURLEncoding.EncodeToString([]byte("payload"))},
		{"empty_sig", base64.RawURLEncoding.EncodeToString([]byte("payload")) + "."},
		{"empty_payload", "." + base64.RawURLEncoding.EncodeToString([]byte("sig"))},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := newConnectRequest(t, tt.token, "example.com:443")
			req.RemoteAddr = "203.0.113.1:12345"
			rec := httptest.NewRecorder()
			h.ServeHTTP(rec, req)

			if rec.Code != http.StatusUnauthorized {
				t.Errorf("status = %d, want 401", rec.Code)
			}
		})
	}
}
