package relay

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestIPHandler_ReturnsRemoteAddr(t *testing.T) {
	handler := NewIPHandler(nil)
	req := httptest.NewRequest(http.MethodGet, "/ip", nil)
	req.RemoteAddr = "93.184.216.34:12345"
	w := httptest.NewRecorder()

	handler.ServeHTTP(w, req)

	body := strings.TrimSpace(w.Body.String())
	if body != "93.184.216.34" {
		t.Errorf("ip = %q, want %q", body, "93.184.216.34")
	}
	if w.Header().Get("Content-Type") != "text/plain" {
		t.Errorf("content-type = %q, want %q", w.Header().Get("Content-Type"), "text/plain")
	}
}

func TestIPHandler_IPv6(t *testing.T) {
	handler := NewIPHandler(nil)
	req := httptest.NewRequest(http.MethodGet, "/ip", nil)
	req.RemoteAddr = "[2001:db8::1]:12345"
	w := httptest.NewRecorder()

	handler.ServeHTTP(w, req)

	body := strings.TrimSpace(w.Body.String())
	if body != "2001:db8::1" {
		t.Errorf("ip = %q, want %q", body, "2001:db8::1")
	}
}
