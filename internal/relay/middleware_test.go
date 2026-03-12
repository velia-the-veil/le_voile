package relay

import (
	"net/http"
	"net/http/httptest"
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

func TestLimitMiddleware_HealthNotLimited(t *testing.T) {
	limiter := NewLimiter(1)
	limiter.Acquire() // saturate

	// Health handler is NOT wrapped by middleware — verify it stays accessible
	healthHandler := NewHealthHandler(limiter, time.Now())
	req := httptest.NewRequest(http.MethodGet, "/health", nil)
	rec := httptest.NewRecorder()

	healthHandler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("expected 200 for /health even when saturated, got %d", rec.Code)
	}
	limiter.Release()
}
