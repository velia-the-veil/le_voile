package relay

import "net/http"

// CFSourceMiddleware rejects requests originating outside the configured
// Cloudflare IP ranges. Returns HTTP 403 for untrusted sources.
//
// Pass-through cases: v == nil (no validator configured, e.g. test setups),
// or v.IsInsecure() == true (dev mode — IsTrustedSource already returns
// true for any source in that mode, but the explicit short-circuit avoids
// any future divergence).
//
// NFR20 compliance: rejection paths MUST NOT log client IPs. When non-nil,
// logFunc is invoked with the literal string "cf-reject" — no IP, no
// metadata that could identify the requester. Callers may aggregate this
// signal to count rejections, but cannot derive identity from it.
func CFSourceMiddleware(v *CloudflareIPValidator, logFunc func(string), next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if v == nil || v.IsInsecure() {
			next.ServeHTTP(w, r)
			return
		}
		if !v.IsTrustedSource(r.RemoteAddr) {
			if logFunc != nil {
				logFunc("cf-reject")
			}
			http.Error(w, "Forbidden", http.StatusForbidden)
			return
		}
		next.ServeHTTP(w, r)
	})
}
