package relay

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestLimitMiddleware_PassThrough(t *testing.T) {
	limiter := NewLimiter(10)
	inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	handler := LimitMiddleware(limiter, inner)
	req := httptest.NewRequest(http.MethodGet, "/dns-query", nil)
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", rec.Code)
	}
}

func TestLimitMiddleware_Returns503WhenSaturated(t *testing.T) {
	limiter := NewLimiter(1)
	limiter.Acquire() // saturate

	inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Error("inner handler should not be called when saturated")
	})

	handler := LimitMiddleware(limiter, inner)
	req := httptest.NewRequest(http.MethodGet, "/dns-query", nil)
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusServiceUnavailable {
		t.Errorf("expected 503, got %d", rec.Code)
	}
	limiter.Release()
}

func TestLimitMiddleware_Returns503WhenTunnelLimiterSaturated(t *testing.T) {
	limiter := NewLimiter(MaxTunnels)
	// Saturate to MaxTunnels (150)
	for i := int64(0); i < MaxTunnels; i++ {
		if !limiter.Acquire() {
			t.Fatalf("Acquire failed at %d", i)
		}
	}
	defer func() {
		for i := int64(0); i < MaxTunnels; i++ {
			limiter.Release()
		}
	}()

	inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Error("inner handler should not be called when tunnel limiter saturated")
	})

	handler := LimitMiddleware(limiter, inner)
	req := httptest.NewRequest(http.MethodPost, "/tunnel", nil)
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusServiceUnavailable {
		t.Errorf("expected 503, got %d", rec.Code)
	}
	if body := rec.Body.String(); !strings.Contains(body, "Service Unavailable") {
		t.Errorf("expected body to contain 'Service Unavailable', got %q", body)
	}
	if limiter.Current() != MaxTunnels {
		t.Errorf("expected current %d (no increment), got %d", MaxTunnels, limiter.Current())
	}
}

func TestLimitMiddleware_HealthNotLimited(t *testing.T) {
	limiter := NewLimiter(1)
	limiter.Acquire() // saturate

	// Health handler is NOT wrapped by middleware — verify it stays accessible
	healthHandler := NewHealthHandler(limiter, nil, time.Now())
	req := httptest.NewRequest(http.MethodGet, "/health", nil)
	rec := httptest.NewRecorder()

	healthHandler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("expected 200 for /health even when saturated, got %d", rec.Code)
	}
	limiter.Release()
}
