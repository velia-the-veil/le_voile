package relay

import (
	"context"
	"crypto/ed25519"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/binary"
	"fmt"
	"io"
	"net/http"
	"time"
)

// TunnelMaxFrameSize is the maximum IP packet size in a tunnel frame.
// MTU 1420 fits inside QUIC without fragmentation (architecture.md#L285).
const TunnelMaxFrameSize = 1420

// TunnelFrameHeaderSize is the length-prefix size (2 bytes big-endian).
const TunnelFrameHeaderSize = 2

// tunnelIdleTimeout is the maximum duration without a frame before the
// stream is closed. Aligned with quic.Config.MaxIdleTimeout in server.go.
const tunnelIdleTimeout = 90 * time.Second

// TunnelSession holds per-stream metadata. No plaintext IP is stored (NFR20).
type TunnelSession struct {
	// ID is a cryptographically random 32-hex identifier minted per tunnel
	// stream. Acts as the authoritative session key for NAT lookups and
	// the reverse-channel dispatcher: two clients cannot collide, and an
	// attacker cannot guess another client's ID to hijack their forward
	// path (fix H5 audit sécurité — before, sessionKey = IPHash@UnixNano
	// with ~30 bits of timestamp entropy was brute-forceable within a
	// second of the victim's connection).
	ID           string
	ClientIPHash string
	OpenedAt     time.Time
}

// PacketForwarder is the interface between the tunnel handler and the
// NAT/DNS pipeline (story 3.4+). Implementations MUST NOT retain the
// pkt slice passed to Forward beyond the call.
type PacketForwarder interface {
	// OpenSession is called when a tunnel stream is authenticated.
	// Returns a channel for outbound packets and an idempotent cleanup func.
	OpenSession(ctx context.Context, session TunnelSession) (<-chan []byte, func())
	// Forward delivers an inbound IP packet from the client.
	// Returning an error closes the stream.
	Forward(ctx context.Context, session TunnelSession, pkt []byte) error
}

// TunnelHandler serves POST /tunnel — a bidirectional IP-packet stream
// authenticated by Ed25519 session tokens with IP-hash binding.
type TunnelHandler struct {
	signingKey  ed25519.PublicKey
	cfValidator *CloudflareIPValidator
	ipLimiter   *IPLimiter
	bwLimiter   *BandwidthLimiter
	forwarder   PacketForwarder
	logFunc     func(string, ...any)
}

// NewTunnelHandler creates a TunnelHandler. Panics if pubKey or forwarder is nil.
func NewTunnelHandler(pubKey ed25519.PublicKey, cfv *CloudflareIPValidator, ipLimiter *IPLimiter, forwarder PacketForwarder, logFunc func(string, ...any)) *TunnelHandler {
	if pubKey == nil {
		panic("tunnel: nil public key")
	}
	if forwarder == nil {
		panic("tunnel: nil forwarder")
	}
	return &TunnelHandler{
		signingKey:  pubKey,
		cfValidator: cfv,
		ipLimiter:   ipLimiter,
		forwarder:   forwarder,
		logFunc:     logFunc,
	}
}

// SetBWLimiter sets the bandwidth limiter for per-IP daily/hourly quota enforcement.
func (h *TunnelHandler) SetBWLimiter(bl *BandwidthLimiter) {
	h.bwLimiter = bl
}

// ServeHTTP handles POST /tunnel requests.
func (h *TunnelHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	// AC5: only POST allowed.
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", "POST")
		http.Error(w, "Method Not Allowed", http.StatusMethodNotAllowed)
		return
	}

	// Extract client IP via CF validator.
	// cfValidator nil → reject (misconfiguration: IP-hash can't be verified).
	if h.cfValidator == nil {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}
	clientIP := ""
	if !h.cfValidator.IsInsecure() {
		ip, err := h.cfValidator.ExtractClientIP(r)
		if err != nil {
			http.Error(w, "Unauthorized", http.StatusUnauthorized)
			return
		}
		clientIP = ip
	} else {
		// Insecure/dev mode: best-effort extraction.
		ip, _ := h.cfValidator.ExtractClientIP(r)
		clientIP = ip
	}

	// Extract Bearer token.
	token := extractBearerToken(r)
	if token == "" {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}
	r.Header.Del("Authorization")

	// Verify session token signature.
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

	// IP-hash match via constant-time compare (NFR9c/NFR9d).
	if clientIP != "" {
		expected := fmt.Sprintf("%x", sha256.Sum256([]byte(clientIP)))
		if subtle.ConstantTimeCompare([]byte(expected), []byte(payload.IPHash)) != 1 {
			http.Error(w, "Unauthorized", http.StatusUnauthorized)
			return
		}
	}

	// Per-IP rate limiting (AC6 per-IP). Global limit handled by LimitMiddleware.
	if h.ipLimiter != nil && clientIP != "" {
		if !h.ipLimiter.Acquire(clientIP) {
			RejectedIPLimitTotal.Add(1)
			http.Error(w, "Too Many Requests", http.StatusTooManyRequests)
			return
		}
		defer h.ipLimiter.Release(clientIP)
	}

	// Per-IP daily bandwidth quota check — reject before starting tunnel.
	if h.bwLimiter != nil && clientIP != "" && !h.bwLimiter.CanOpenTunnel(clientIP) {
		RejectedDailyQuotaTotal.Add(1)
		http.Error(w, "Too Many Requests", http.StatusTooManyRequests)
		return
	}

	// Auth passed — start bidirectional stream (AC1).
	h.serveTunnel(w, r, payload, clientIP)
}

// serveTunnel runs the bidirectional frame pump after auth succeeds.
func (h *TunnelHandler) serveTunnel(w http.ResponseWriter, r *http.Request, payload *SessionTokenPayload, clientIP string) {
	// Capture body reference BEFORE WriteHeader — the Go HTTP/1.1 server
	// may close r.Body after the response headers are flushed if it detects
	// the body hasn't been fully consumed. Holding a direct reference to
	// the underlying reader prevents the "read on closed body" error.
	body := r.Body

	// Send 200 and flush to unblock client.
	w.WriteHeader(http.StatusOK)
	if f, ok := w.(http.Flusher); ok {
		f.Flush()
	}

	sid, sidErr := newSessionID()
	if sidErr != nil {
		// crypto/rand failure is effectively impossible; refuse to open
		// a session with a weak ID rather than fall back to a predictable
		// format. Client will see the POST close and retry.
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
		return
	}
	session := TunnelSession{
		ID:           sid,
		ClientIPHash: payload.IPHash,
		OpenedAt:     time.Now(),
	}

	sessionCtx, cancel := context.WithCancel(r.Context())
	defer cancel()

	outCh, cleanup := h.forwarder.OpenSession(sessionCtx, session)
	defer cleanup()

	readerDone := make(chan struct{})
	writerDone := make(chan struct{})

	// Goroutine: client → forwarder (read frames from request body).
	// Does NOT cancel the context — allows the writer to drain remaining
	// outbound packets after the reader finishes (EOF or error).
	go func() {
		defer close(readerDone)
		hdr := make([]byte, TunnelFrameHeaderSize)
		buf := make([]byte, TunnelMaxFrameSize)
		idleTimer := time.NewTimer(tunnelIdleTimeout)
		defer idleTimer.Stop()
		// Monitor idle timeout in a separate goroutine.
		go func() {
			select {
			case <-idleTimer.C:
				cancel()
			case <-sessionCtx.Done():
			}
		}()
		for {
			if _, err := io.ReadFull(body, hdr); err != nil {
				return
			}
			idleTimer.Reset(tunnelIdleTimeout)
			n := binary.BigEndian.Uint16(hdr)
			if n == 0 || n > TunnelMaxFrameSize {
				if h.logFunc != nil {
					h.logFunc("tunnel: frame size out of range: %d", n)
				}
				return
			}
			if _, err := io.ReadFull(body, buf[:n]); err != nil {
				return
			}
			if err := h.forwarder.Forward(sessionCtx, session, buf[:n]); err != nil {
				if h.logFunc != nil {
					h.logFunc("tunnel: forwarder error")
				}
				return
			}
		}
	}()

	// Goroutine: forwarder → client (write frames to response).
	// Drains remaining channel items after context cancellation.
	go func() {
		defer close(writerDone)
		hdr := make([]byte, TunnelFrameHeaderSize)
		writeFrame := func(pkt []byte) bool {
			if len(pkt) > TunnelMaxFrameSize {
				if h.logFunc != nil {
					h.logFunc("tunnel: outbound frame too large, dropping")
				}
				return true
			}
			// Bandwidth accounting on outbound data (same as connect_handler relay).
			if h.bwLimiter != nil && clientIP != "" {
				h.bwLimiter.AccountAndThrottle(sessionCtx, clientIP, len(pkt))
			}
			binary.BigEndian.PutUint16(hdr, uint16(len(pkt)))
			if _, err := w.Write(hdr); err != nil {
				return false
			}
			if _, err := w.Write(pkt); err != nil {
				return false
			}
			if f, ok := w.(http.Flusher); ok {
				f.Flush()
			}
			return true
		}
		for {
			select {
			case <-sessionCtx.Done():
				// Drain any remaining buffered packets.
				for {
					select {
					case pkt, ok := <-outCh:
						if !ok {
							return
						}
						writeFrame(pkt)
					default:
						return
					}
				}
			case pkt, ok := <-outCh:
				if !ok {
					return
				}
				if !writeFrame(pkt) {
					cancel()
					return
				}
			}
		}
	}()

	// Wait for reader to finish (EOF, error, or context cancelled).
	<-readerDone
	// Close request body to unblock reader if still blocked (HTTP/3 streams
	// have no SetReadDeadline — closing is the only way).
	body.Close()
	// Cancel the context to signal forwarder and writer to stop.
	cancel()
	// Wait for writer to finish draining.
	<-writerDone
}
