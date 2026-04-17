package relay

import (
	"context"
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"
)

const (
	connectIdleTimeout = 120 * time.Second
	maxConnectBodySize = 1024 // max size for the JSON target body
)

// connectRequest is the JSON body for a CONNECT proxy request.
type connectRequest struct {
	Target string `json:"target"`
}

// ConnectHandler implements the relay-side forward proxy handler.
// Accepts POST requests with a target in the JSON body, authenticates via
// session token, and relays traffic bidirectionally to the destination.
type ConnectHandler struct {
	signingKey  ed25519.PublicKey // for verifying session tokens
	cfValidator *CloudflareIPValidator
	ipLimiter   *IPLimiter
	bwLimiter   *BandwidthLimiter
	logFunc     func(format string, args ...any)
}

// NewConnectHandler creates a new relay CONNECT handler.
func NewConnectHandler(pubKey ed25519.PublicKey, cfv *CloudflareIPValidator, ipLimiter *IPLimiter, bwLimiter *BandwidthLimiter, logFunc func(string, ...any)) *ConnectHandler {
	return &ConnectHandler{
		signingKey:  pubKey,
		cfValidator: cfv,
		ipLimiter:   ipLimiter,
		bwLimiter:   bwLimiter,
		logFunc:     logFunc,
	}
}

// ServeHTTP handles POST requests to /connect.
func (h *ConnectHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if h.logFunc != nil {
		h.logFunc("incoming %s", r.Method)
	}

	// Only POST allowed.
	if r.Method != http.MethodPost {
		http.Error(w, "Method Not Allowed", http.StatusMethodNotAllowed)
		return
	}

	// CF-Connecting-IP validation: drop non-CF sources silently.
	if h.cfValidator != nil && !h.cfValidator.IsTrustedSource(r.RemoteAddr) {
		// Silent TCP drop — no HTTP response.
		hj, ok := w.(http.Hijacker)
		if ok {
			conn, _, _ := hj.Hijack()
			if conn != nil {
				conn.Close()
			}
		}
		return
	}

	// Extract client IP.
	clientIP := ""
	if h.cfValidator != nil {
		var err error
		clientIP, err = h.cfValidator.ExtractClientIP(r)
		if err != nil {
			http.Error(w, "Unauthorized", http.StatusUnauthorized)
			return
		}
	}

	// Authenticate: extract and verify session token.
	token := extractBearerToken(r)
	if token == "" {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}
	// Sanitize: remove Authorization header to prevent accidental log/forward leakage.
	r.Header.Del("Authorization")

	payload, err := VerifySessionToken(h.signingKey, token)
	if err != nil {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}

	// Check expiration.
	if time.Now().Unix() > payload.Issued+payload.TTL {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}

	// Check IP hash match (defense-in-depth).
	if clientIP != "" {
		expectedHash := fmt.Sprintf("%x", sha256.Sum256([]byte(clientIP)))
		if payload.IPHash != expectedHash {
			http.Error(w, "Unauthorized", http.StatusUnauthorized)
			return
		}
	}

	// Per-IP rate limiting.
	if h.ipLimiter != nil && clientIP != "" {
		if !h.ipLimiter.Acquire(clientIP) {
			RejectedIPLimitTotal.Add(1)
			http.Error(w, "Too Many Requests", http.StatusTooManyRequests)
			return
		}
		defer h.ipLimiter.Release(clientIP)
	}

	// Per-IP daily bandwidth quota check — reject before dialing upstream.
	if h.bwLimiter != nil && clientIP != "" && !h.bwLimiter.CanOpenTunnel(clientIP) {
		RejectedDailyQuotaTotal.Add(1)
		http.Error(w, "Too Many Requests", http.StatusTooManyRequests)
		return
	}

	// Read target from JSON body using a streaming decoder.
	// IMPORTANT: Do NOT use io.ReadAll — the body is a multiplexed stream
	// (JSON target + upstream data). ReadAll would block waiting for the stream to end.
	decoder := json.NewDecoder(io.LimitReader(r.Body, maxConnectBodySize))
	var req connectRequest
	if err := decoder.Decode(&req); err != nil || req.Target == "" {
		http.Error(w, "Bad Request", http.StatusBadRequest)
		return
	}

	// Resolve and validate destination (TOCTOU-safe: use resolved IP for dial).
	targetAddr, err := resolveAndValidateConnect(req.Target)
	if err != nil {
		http.Error(w, "Forbidden", http.StatusForbidden)
		return
	}

	// Dial the destination using the resolved IP directly.
	destConn, err := net.DialTCP("tcp", nil, targetAddr)
	if err != nil {
		http.Error(w, "Bad Gateway", http.StatusBadGateway)
		return
	}
	defer destConn.Close()

	// Signal success — start streaming.
	w.WriteHeader(http.StatusOK)

	// Flush the 200 response immediately so the client can start relaying.
	if f, ok := w.(http.Flusher); ok {
		f.Flush()
	}

	// Bidirectional relay with idle timeout.
	// Pass remaining body (after JSON) as the client upstream reader.
	// Pass r.Body as closer so the relay can unblock clientReader.Read()
	// when the destination side times out or disconnects.
	clientReader := io.MultiReader(decoder.Buffered(), r.Body)
	ctx := r.Context()
	relay(ctx, clientReader, w, destConn, r.Body, clientIP, h.bwLimiter)
}

// relay copies data bidirectionally between the HTTP stream and the destination.
// Both directions run in goroutines; when either finishes, the other is unblocked
// via deadline manipulation and context cancellation.
// bodyCloser is closed to unblock clientReader.Read() when the relay ends —
// without this, reads on the HTTP/3 request body block indefinitely, leaking
// the goroutine and its IP limiter slot.
func relay(ctx context.Context, clientReader io.Reader, clientWriter io.Writer, dest net.Conn, bodyCloser io.Closer, clientIP string, bwLimiter *BandwidthLimiter) {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	var wg sync.WaitGroup
	wg.Add(2)

	// dest → client
	go func() {
		defer wg.Done()
		defer cancel()
		buf := make([]byte, 32*1024)
		for {
			dest.SetReadDeadline(time.Now().Add(connectIdleTimeout))
			n, err := dest.Read(buf)
			if n > 0 {
				if bwLimiter != nil && clientIP != "" {
					bwLimiter.AccountAndThrottle(ctx, clientIP, n)
				}
				if _, wErr := clientWriter.Write(buf[:n]); wErr != nil {
					return
				}
				if f, ok := clientWriter.(http.Flusher); ok {
					f.Flush()
				}
			}
			if err != nil {
				return
			}
		}
	}()

	// client → dest
	go func() {
		defer wg.Done()
		defer cancel()
		// When client stream ends, unblock dest.Read via a past deadline.
		defer dest.SetReadDeadline(time.Now())
		buf := make([]byte, 32*1024)
		for {
			n, err := clientReader.Read(buf)
			if n > 0 {
				dest.SetWriteDeadline(time.Now().Add(connectIdleTimeout))
				if _, wErr := dest.Write(buf[:n]); wErr != nil {
					return
				}
			}
			if err != nil {
				return
			}
		}
	}()

	// Wait for context cancellation (from either goroutine or parent).
	<-ctx.Done()
	// Force-unblock both directions.
	dest.SetDeadline(time.Now())
	// Close the request body to unblock clientReader.Read() — the HTTP/3
	// stream read has no deadline mechanism, so closing is the only way
	// to prevent the client→dest goroutine from blocking forever.
	if bodyCloser != nil {
		bodyCloser.Close()
	}
	wg.Wait()
}

// resolveAndValidateConnect resolves a host:port and validates against SSRF.
func resolveAndValidateConnect(target string) (*net.TCPAddr, error) {
	host, port, err := net.SplitHostPort(target)
	if err != nil {
		return nil, fmt.Errorf("connect: invalid target: %w", err)
	}
	if host == "" || port == "" {
		return nil, fmt.Errorf("connect: empty host or port")
	}

	// Resolve hostname to IP.
	var ip net.IP
	if parsed := net.ParseIP(host); parsed != nil {
		ip = parsed
	} else {
		ips, err := net.LookupIP(host)
		if err != nil || len(ips) == 0 {
			return nil, fmt.Errorf("connect: resolve %q: %w", host, err)
		}
		ip = ips[0]
	}

	// SSRF validation on resolved IP.
	if isBlockedIP(ip) {
		return nil, fmt.Errorf("connect: blocked destination %s", ip)
	}

	portNum, err := net.LookupPort("tcp", port)
	if err != nil {
		return nil, fmt.Errorf("connect: invalid port %q: %w", port, err)
	}

	return &net.TCPAddr{IP: ip, Port: portNum}, nil
}

// isBlockedIP returns true if the IP is private, loopback, or otherwise blocked.
// Covers SSRF vectors: loopback, private, link-local, multicast, unspecified,
// and IPv6-mapped IPv4 addresses (e.g. ::ffff:127.0.0.1).
func isBlockedIP(ip net.IP) bool {
	// Normalize IPv6-mapped IPv4 (e.g. ::ffff:127.0.0.1 → 127.0.0.1)
	// so that checks like IsLoopback work correctly.
	if mapped := ip.To4(); mapped != nil {
		ip = mapped
	}
	return ip.IsLoopback() ||
		ip.IsPrivate() ||
		ip.IsLinkLocalUnicast() ||
		ip.IsLinkLocalMulticast() ||
		ip.IsMulticast() ||
		ip.IsUnspecified() ||
		ip.Equal(net.IPv4bcast)
}

// extractBearerToken extracts the token from an Authorization: Bearer header.
// The Authorization header value is sanitized in logs.
func extractBearerToken(r *http.Request) string {
	auth := r.Header.Get("Authorization")
	if auth == "" {
		return ""
	}
	const prefix = "Bearer "
	if len(auth) < len(prefix) || !strings.EqualFold(auth[:len(prefix)], prefix) {
		return ""
	}
	return auth[len(prefix):]
}
