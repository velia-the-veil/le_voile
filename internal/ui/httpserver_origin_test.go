package ui

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

// TestOriginGuard_SameOriginAllowed covers the normal webview flow: Origin
// header points back at our loopback listener → request passes.
func TestOriginGuard_SameOriginAllowed(t *testing.T) {
	inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusTeapot)
	})
	h := originGuard(inner)

	cases := []struct {
		origin string
	}{
		{"http://127.0.0.1:54321"},
		{"http://[::1]:54321"},
		{"http://localhost:54321"},
		{""},            // no Origin — webview direct fetch
		{"null"},        // file:// context — null Origin, rejected
	}

	for _, tc := range cases {
		req := httptest.NewRequest(http.MethodGet, "http://127.0.0.1:54321/api/status", nil)
		if tc.origin != "" {
			req.Header.Set("Origin", tc.origin)
		}
		rr := httptest.NewRecorder()
		h.ServeHTTP(rr, req)

		// "null" Origin is not a loopback prefix → 403. All others 418.
		expected := http.StatusTeapot
		if tc.origin == "null" {
			expected = http.StatusForbidden
		}
		if rr.Code != expected {
			t.Errorf("origin=%q: got status %d, want %d", tc.origin, rr.Code, expected)
		}
	}
}

// TestOriginGuard_CrossOriginRejected is the core defense: a malicious page
// at evil.example does fetch('http://127.0.0.1:PORT/api/connect') and the
// browser dutifully attaches Origin: https://evil.example. Must 403.
func TestOriginGuard_CrossOriginRejected(t *testing.T) {
	inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Errorf("handler reached with cross-origin request")
	})
	h := originGuard(inner)

	hostile := []string{
		"https://evil.example",
		"http://attacker.localhost",     // suffix trap
		"http://127.0.0.1.evil.com",     // prefix trap
		"http://127.0.0.2",              // different loopback
		"http://example.com:127.0.0.1",  // port-host confusion
	}

	for _, o := range hostile {
		req := httptest.NewRequest(http.MethodPost, "http://127.0.0.1:54321/api/connect", nil)
		req.Header.Set("Origin", o)
		rr := httptest.NewRecorder()
		h.ServeHTTP(rr, req)
		if rr.Code != http.StatusForbidden {
			t.Errorf("origin=%q: got status %d, want 403", o, rr.Code)
		}
	}
}

// TestOriginGuard_DNSRebindingRejected covers the case where the attacker
// gets the user's browser to send Host: evil.example (resolving to
// 127.0.0.1 via rebinding) without Origin. The Host check blocks it.
func TestOriginGuard_DNSRebindingRejected(t *testing.T) {
	inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Errorf("handler reached on DNS-rebinding request")
	})
	h := originGuard(inner)

	req := httptest.NewRequest(http.MethodGet, "http://127.0.0.1:54321/api/status", nil)
	req.Host = "evil.example:54321"
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)
	if rr.Code != http.StatusForbidden {
		t.Errorf("DNS rebinding got status %d, want 403", rr.Code)
	}
}

// TestOriginGuard_RefererFallback confirms that when Origin is absent but
// Referer points outside loopback, the request is still rejected. Browsers
// strip Origin on some redirects; Referer is the backup signal.
func TestOriginGuard_RefererFallback(t *testing.T) {
	inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Errorf("handler reached with evil referer")
	})
	h := originGuard(inner)

	req := httptest.NewRequest(http.MethodGet, "http://127.0.0.1:54321/api/status", nil)
	req.Header.Set("Referer", "https://evil.example/phish")
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)
	if rr.Code != http.StatusForbidden {
		t.Errorf("evil Referer got status %d, want 403", rr.Code)
	}
}
