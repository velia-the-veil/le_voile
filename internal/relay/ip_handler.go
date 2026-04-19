package relay

import (
	"net/http"
)

// IPHandler returns the client's visible IP address.
// Used by the desktop client to display the exit IP after tunnel connection.
type IPHandler struct{}

// NewIPHandler creates an IP handler that reflects the caller's remote address.
func NewIPHandler() *IPHandler {
	return &IPHandler{}
}

func (h *IPHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/plain")
	w.Write([]byte(clientIP(r)))
}
