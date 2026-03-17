package relay

import (
	"net"
	"net/http"
)

// IPHandler returns the client's visible IP address.
// Used by the desktop client to display the exit IP after tunnel connection.
type IPHandler struct {
	cfValidator *CloudflareIPValidator
}

// NewIPHandler creates an IP handler with optional Cloudflare IP validation.
func NewIPHandler(cfv *CloudflareIPValidator) *IPHandler {
	return &IPHandler{cfValidator: cfv}
}

func (h *IPHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	var clientIP string
	if h.cfValidator != nil {
		ip, err := h.cfValidator.ExtractClientIP(r)
		if err == nil {
			clientIP = ip
		}
	}
	if clientIP == "" {
		// Fallback: extract from RemoteAddr directly.
		host, _, err := net.SplitHostPort(r.RemoteAddr)
		if err != nil {
			host = r.RemoteAddr
		}
		clientIP = host
	}
	w.Header().Set("Content-Type", "text/plain")
	w.Write([]byte(clientIP))
}
