package relay

import (
	"encoding/binary"
	"fmt"
	"io"
	"net"
	"net/http"
	"strconv"
	"time"
)

const (
	stunMessageContentType = "application/stun-message"
	maxSTUNBodySize        = 1500
	stunUpstreamTimeout    = 3 * time.Second
	stunHeaderSize         = 20
	stunMagicCookie        = 0x2112A442
)

// testSkipIPValidation bypasses IP validation in tests (set by tests only).
var testSkipIPValidation bool

// allowedSTUNPorts are the standard STUN/TURN ports that the relay will forward to.
var allowedSTUNPorts = map[int]bool{
	3478:  true, // STUN
	5349:  true, // STUN over DTLS
	19302: true, // Google STUN
	19305: true, // Google STUN alt
}

// STUNHandler implements http.Handler for relaying STUN packets.
// It receives a STUN packet via HTTP POST, sends it to the target STUN server
// via UDP, and returns the response.
type STUNHandler struct {
	timeout time.Duration
	// TestAllowPort adds an extra port to the allowed set (for testing only).
	TestAllowPort int
	// TestSkipIPCheck disables loopback/private IP validation (for testing only).
	TestSkipIPCheck bool
}

// NewSTUNHandler creates a handler that relays STUN requests to the target
// server specified in the X-Stun-Target header.
func NewSTUNHandler() *STUNHandler {
	return &STUNHandler{timeout: stunUpstreamTimeout}
}

// NewSTUNHandlerWithTimeout creates a STUNHandler with a custom timeout.
func NewSTUNHandlerWithTimeout(timeout time.Duration) *STUNHandler {
	return &STUNHandler{timeout: timeout}
}

// ServeHTTP handles incoming STUN relay requests.
func (h *STUNHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "", http.StatusMethodNotAllowed)
		return
	}

	if r.Header.Get("Content-Type") != stunMessageContentType {
		http.Error(w, "", http.StatusBadRequest)
		return
	}

	target := r.Header.Get("X-Stun-Target")
	if target == "" {
		http.Error(w, "", http.StatusBadRequest)
		return
	}

	if !h.isAllowedTarget(target) {
		http.Error(w, "", http.StatusForbidden)
		return
	}

	body, err := io.ReadAll(io.LimitReader(r.Body, maxSTUNBodySize+1))
	if err != nil {
		http.Error(w, "", http.StatusBadRequest)
		return
	}

	if len(body) == 0 {
		http.Error(w, "", http.StatusBadRequest)
		return
	}

	if len(body) > maxSTUNBodySize {
		http.Error(w, "", http.StatusBadRequest)
		return
	}

	if !isValidSTUNPacket(body) {
		http.Error(w, "", http.StatusBadRequest)
		return
	}

	resp, err := h.forwardToSTUN(body, target)
	if err != nil {
		http.Error(w, "", http.StatusBadGateway)
		return
	}

	w.Header().Set("Content-Type", stunMessageContentType)
	w.WriteHeader(http.StatusOK)
	w.Write(resp)
}

// isAllowedTarget validates that the target address uses an allowed STUN port
// and is not a private/loopback IP address.
// Hostnames are resolved immediately to prevent DNS rebinding and SSRF bypasses.
func (h *STUNHandler) isAllowedTarget(target string) bool {
	host, portStr, err := net.SplitHostPort(target)
	if err != nil {
		return false
	}

	port, err := strconv.Atoi(portStr)
	if err != nil {
		return false
	}

	if !allowedSTUNPorts[port] && port != h.TestAllowPort {
		return false
	}

	if host == "" {
		return false
	}

	// Block private/loopback IPs to prevent SSRF.
	skipIPCheck := testSkipIPValidation || h.TestSkipIPCheck
	if !skipIPCheck {
		// Resolve hostnames to IPs immediately to prevent DNS rebinding attacks.
		// If host is already an IP, ParseIP succeeds; otherwise resolve via DNS.
		ips := []net.IP{net.ParseIP(host)}
		if ips[0] == nil {
			resolved, err := net.LookupIP(host)
			if err != nil || len(resolved) == 0 {
				return false
			}
			ips = resolved
		}
		for _, ip := range ips {
			if ip.IsLoopback() || ip.IsPrivate() || ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() || ip.IsUnspecified() {
				return false
			}
		}
	}

	return true
}

// isValidSTUNPacket performs minimal STUN validation: checks size, first 2 bits,
// and magic cookie at bytes 4-7 per RFC 5389.
func isValidSTUNPacket(packet []byte) bool {
	if len(packet) < stunHeaderSize {
		return false
	}
	// RFC 5764: first 2 bits must be 0b00 for STUN.
	if packet[0]&0xC0 != 0 {
		return false
	}
	cookie := binary.BigEndian.Uint32(packet[4:8])
	return cookie == stunMagicCookie
}

func (h *STUNHandler) forwardToSTUN(packet []byte, target string) ([]byte, error) {
	addr, err := net.ResolveUDPAddr("udp", target)
	if err != nil {
		return nil, fmt.Errorf("relay: stun: resolve: %w", err)
	}

	conn, err := net.DialUDP("udp", nil, addr)
	if err != nil {
		return nil, fmt.Errorf("relay: stun: dial: %w", err)
	}
	defer conn.Close()

	conn.SetDeadline(time.Now().Add(h.timeout))

	if _, err := conn.Write(packet); err != nil {
		return nil, fmt.Errorf("relay: stun: write: %w", err)
	}

	buf := make([]byte, maxSTUNBodySize)
	n, err := conn.Read(buf)
	if err != nil {
		return nil, fmt.Errorf("relay: stun: read: %w", err)
	}

	return buf[:n], nil
}
