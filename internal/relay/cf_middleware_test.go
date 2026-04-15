package relay

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func cfMWTestOK() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("ok"))
	})
}

func TestCFSourceMiddleware_NilValidator_PassesThrough(t *testing.T) {
	rec := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/health", nil)
	req.RemoteAddr = "203.0.113.1:1234" // arbitrary public IP

	CFSourceMiddleware(nil, nil, cfMWTestOK()).ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 with nil validator, got %d", rec.Code)
	}
}

func TestCFSourceMiddleware_InsecureMode_PassesThrough(t *testing.T) {
	v := NewCloudflareIPValidator(true, nil)
	rec := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/health", nil)
	req.RemoteAddr = "203.0.113.1:1234"

	CFSourceMiddleware(v, nil, cfMWTestOK()).ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 in insecure mode, got %d", rec.Code)
	}
}

func TestCFSourceMiddleware_NonCFSource_Returns403(t *testing.T) {
	v := NewCloudflareIPValidator(false, nil)
	rec := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/health", nil)
	req.RemoteAddr = "203.0.113.1:1234" // not a CF range

	CFSourceMiddleware(v, nil, cfMWTestOK()).ServeHTTP(rec, req)

	if rec.Code != http.StatusForbidden {
		t.Fatalf("expected 403 for non-CF source, got %d", rec.Code)
	}
}

func TestCFSourceMiddleware_CFSource_PassesThrough(t *testing.T) {
	v := NewCloudflareIPValidator(false, nil)
	rec := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/health", nil)
	// 173.245.48.0/20 is in defaultCFIPv4Ranges
	req.RemoteAddr = "173.245.48.10:443"

	CFSourceMiddleware(v, nil, cfMWTestOK()).ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 for CF source, got %d", rec.Code)
	}
}

func TestCFSourceMiddleware_LogFunc_NoIPLeak(t *testing.T) {
	v := NewCloudflareIPValidator(false, nil)
	var captured []string
	logFn := func(reason string) {
		captured = append(captured, reason)
	}
	rec := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/health", nil)
	clientIP := "198.51.100.42"
	req.RemoteAddr = clientIP + ":9999"

	CFSourceMiddleware(v, logFn, cfMWTestOK()).ServeHTTP(rec, req)

	if rec.Code != http.StatusForbidden {
		t.Fatalf("expected 403, got %d", rec.Code)
	}
	if len(captured) != 1 {
		t.Fatalf("expected 1 log entry, got %d", len(captured))
	}
	for _, msg := range captured {
		if strings.Contains(msg, clientIP) {
			t.Errorf("NFR20 violation: log message %q contains client IP %q", msg, clientIP)
		}
		if strings.Contains(msg, "198.51.100") {
			t.Errorf("NFR20 violation: log message %q contains client IP prefix", msg)
		}
	}
}

func TestCFSourceMiddleware_NoLogFunc_NoSurprises(t *testing.T) {
	v := NewCloudflareIPValidator(false, nil)
	rec := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/health", nil)
	req.RemoteAddr = "198.51.100.42:9999"

	// Must not panic when logFunc is nil and request is rejected.
	CFSourceMiddleware(v, nil, cfMWTestOK()).ServeHTTP(rec, req)

	if rec.Code != http.StatusForbidden {
		t.Fatalf("expected 403, got %d", rec.Code)
	}
}

func TestCFSourceMiddleware_MalformedRemoteAddr_Returns403(t *testing.T) {
	v := NewCloudflareIPValidator(false, nil)
	rec := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/health", nil)
	req.RemoteAddr = "not-an-address"

	CFSourceMiddleware(v, nil, cfMWTestOK()).ServeHTTP(rec, req)

	if rec.Code != http.StatusForbidden {
		t.Fatalf("expected 403 for malformed RemoteAddr, got %d", rec.Code)
	}
}
