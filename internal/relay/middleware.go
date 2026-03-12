package relay

import "net/http"

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
