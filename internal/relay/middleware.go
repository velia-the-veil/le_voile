package relay

import (
	"net/http"
	"strings"
)

// LimitMiddleware wraps an http.Handler with connection limiting.
// Returns HTTP 503 Service Unavailable when the limiter is saturated.
func LimitMiddleware(limiter *Limiter, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !limiter.Acquire() {
			http.Error(w, "Service Unavailable", http.StatusServiceUnavailable)
			return
		}
		defer limiter.Release()
		next.ServeHTTP(w, r)
	})
}

// IPLimitMiddleware enforces per-client-IP concurrency limits. It fronts the
// global LimitMiddleware for endpoints where a single abusive client should
// not be able to exhaust the global pool — most notably /verify, where each
// request triggers an Ed25519 sign that is cheap but non-trivial under load.
//
// The client IP is taken from CF-Connecting-IP when present (Cloudflare
// front) and falls back to r.RemoteAddr. Returns HTTP 429 when saturated,
// distinct from the global LimitMiddleware's 503 so monitoring can tell
// "one noisy client" apart from "relay overall overloaded".
func IPLimitMiddleware(limiter *IPLimiter, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ip := clientIPForLimiter(r)
		if ip == "" {
			// No usable source IP (e.g. tests): skip the per-IP limiter rather
			// than fail-closed. The global limiter still protects the relay.
			next.ServeHTTP(w, r)
			return
		}
		if !limiter.Acquire(ip) {
			http.Error(w, "Too Many Requests", http.StatusTooManyRequests)
			return
		}
		defer limiter.Release(ip)
		next.ServeHTTP(w, r)
	})
}

// clientIPForLimiter returns the best-available client IP for rate-limiting.
// Prefers CF-Connecting-IP (trusted only after CFSourceMiddleware has gated
// the request), then X-Forwarded-For's first hop, then RemoteAddr minus port.
func clientIPForLimiter(r *http.Request) string {
	if v := strings.TrimSpace(r.Header.Get("CF-Connecting-IP")); v != "" {
		return v
	}
	if v := strings.TrimSpace(r.Header.Get("X-Forwarded-For")); v != "" {
		if comma := strings.IndexByte(v, ','); comma >= 0 {
			v = v[:comma]
		}
		return strings.TrimSpace(v)
	}
	if r.RemoteAddr == "" {
		return ""
	}
	// net.SplitHostPort fails on IPs without port; keep it simple.
	if colon := strings.LastIndexByte(r.RemoteAddr, ':'); colon >= 0 {
		return r.RemoteAddr[:colon]
	}
	return r.RemoteAddr
}
