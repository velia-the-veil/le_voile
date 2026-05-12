package relay

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

// TestIPLimitMiddleware_RejectsSameIPSpam covers fix H10: a single client IP
// hitting /verify-like endpoints above the per-IP cap gets 429, leaving
// global capacity for other clients. The cap here mirrors the default
// IPLimiterMaxPerIP so the arithmetic is explicit.
func TestIPLimitMiddleware_RejectsSameIPSpam(t *testing.T) {
	limiter := NewIPLimiter(3)

	handlerEntered := make(chan struct{}, 16)
	handlerExit := make(chan struct{})
	inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		handlerEntered <- struct{}{}
		<-handlerExit
		w.WriteHeader(http.StatusOK)
	})
	wrapped := IPLimitMiddleware(limiter, inner)

	// Kick off 3 concurrent requests from the same IP — all allowed.
	done := make(chan int, 4)
	for i := 0; i < 3; i++ {
		go func() {
			req := httptest.NewRequest(http.MethodPost, "http://relay/verify", nil)
			req.RemoteAddr = "203.0.113.7:12345"
			rr := httptest.NewRecorder()
			wrapped.ServeHTTP(rr, req)
			done <- rr.Code
		}()
	}
	// Wait for all 3 handlers to be in flight before launching the 4th.
	for i := 0; i < 3; i++ {
		<-handlerEntered
	}

	// Fourth request from same IP — expect 429 without entering inner.
	req := httptest.NewRequest(http.MethodPost, "http://relay/verify", nil)
	req.RemoteAddr = "203.0.113.7:12345"
	rr := httptest.NewRecorder()
	wrapped.ServeHTTP(rr, req)
	if rr.Code != http.StatusTooManyRequests {
		t.Errorf("4th concurrent request from same IP: got %d, want 429", rr.Code)
	}

	// Release the in-flight handlers; the 4 parked goroutines should drain.
	close(handlerExit)
	for i := 0; i < 3; i++ {
		code := <-done
		if code != http.StatusOK {
			t.Errorf("in-flight request returned %d, want 200", code)
		}
	}
}

// TestIPLimitMiddleware_DifferentIPsIsolated : one abusive client cannot
// shut out another. Critical property since the relay is multi-tenant.
func TestIPLimitMiddleware_DifferentIPsIsolated(t *testing.T) {
	limiter := NewIPLimiter(1)

	blocking := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	wrapped := IPLimitMiddleware(limiter, blocking)

	req1 := httptest.NewRequest(http.MethodPost, "http://relay/verify", nil)
	req1.RemoteAddr = "198.51.100.1:12345"
	rr1 := httptest.NewRecorder()
	wrapped.ServeHTTP(rr1, req1)
	if rr1.Code != http.StatusOK {
		t.Fatalf("IP A request 1 got %d, want 200", rr1.Code)
	}

	// Different IP — must succeed independently of IP A's history.
	req2 := httptest.NewRequest(http.MethodPost, "http://relay/verify", nil)
	req2.RemoteAddr = "198.51.100.2:12345"
	rr2 := httptest.NewRecorder()
	wrapped.ServeHTTP(rr2, req2)
	if rr2.Code != http.StatusOK {
		t.Errorf("IP B got %d, want 200 (IPs must be isolated)", rr2.Code)
	}
}
