package relay

import (
	"net"
	"net/http"
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
// Returns HTTP 429 when saturated, distinct from the global LimitMiddleware's
// 503 so monitoring can tell "one noisy client" apart from "relay overall
// overloaded".
func IPLimitMiddleware(limiter *IPLimiter, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ip := clientIP(r)
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

// clientIP returns the client IP from the request's RemoteAddr (host portion,
// port stripped). The relay is reached directly by clients (DNS-only origin,
// no CDN fronting), so spoofable hop headers like X-Forwarded-For and
// CF-Connecting-IP are NOT consulted — trusting them on a direct-to-origin
// path would let any client impersonate another and hijack its session token.
func clientIP(r *http.Request) string {
	if r.RemoteAddr == "" {
		return ""
	}
	if h, _, err := net.SplitHostPort(r.RemoteAddr); err == nil {
		return h
	}
	return r.RemoteAddr
}
