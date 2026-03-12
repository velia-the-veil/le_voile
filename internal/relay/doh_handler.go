package relay

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net/http"
	"sync"
	"time"
)

const (
	dnsMessageContentType   = "application/dns-message"
	maxDNSBodySize          = 65535
	upstreamTimeout         = 5 * time.Second
	fallbackTimeout         = 2 * time.Second
	defaultRecoveryInterval = 30 * time.Second
)

// Sentinel errors for the DoH handler.
var (
	ErrUpstreamUnavailable = errors.New("relay: upstream unavailable")
	ErrBodyTooLarge        = errors.New("relay: body too large")
	ErrEmptyBody           = errors.New("relay: empty body")
)

// DoHHandler implements http.Handler for DNS-over-HTTPS (RFC 8484).
// It forwards DNS wire-format queries to an ordered list of upstream resolvers,
// automatically falling back to the next resolver when the active one fails.
type DoHHandler struct {
	upstreams        []string // upstreams[0] = primary, [1:] = fallbacks
	mu               sync.RWMutex
	activeIdx        int
	client           *http.Client
	recoveryInterval time.Duration
	onRecovery       func() // called after activeIdx resets to 0; for testing only
}

// NewDoHHandler creates a handler that forwards DoH requests to the given
// upstreams in order. upstreams[0] is the primary resolver; subsequent entries
// are fallbacks. If client is nil, a default http.Client with 5s timeout is used.
// Panics if upstreams is empty.
func NewDoHHandler(upstreams []string, client *http.Client) *DoHHandler {
	if len(upstreams) == 0 {
		panic("relay: NewDoHHandler: upstreams must not be empty")
	}
	if client == nil {
		client = &http.Client{Timeout: upstreamTimeout}
	}
	return &DoHHandler{
		upstreams:        upstreams,
		client:           client,
		recoveryInterval: defaultRecoveryInterval,
	}
}

// Start launches the recovery goroutine that periodically probes the primary
// upstream and resets activeIdx to 0 when it becomes reachable again.
// The goroutine stops when ctx is cancelled. If only one upstream is configured,
// this is a no-op.
func (h *DoHHandler) Start(ctx context.Context) {
	if len(h.upstreams) <= 1 {
		return
	}
	go func() {
		ticker := time.NewTicker(h.recoveryInterval)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				h.mu.RLock()
				active := h.activeIdx
				h.mu.RUnlock()
				if active == 0 {
					continue
				}
				probeCtx, cancel := context.WithTimeout(ctx, fallbackTimeout)
				reachable := h.isPrimaryReachable(probeCtx)
				cancel()
				if reachable {
					h.mu.Lock()
					h.activeIdx = 0
					h.mu.Unlock()
					if h.onRecovery != nil {
						h.onRecovery()
					}
				}
			}
		}
	}()
}

// ServeHTTP handles incoming DoH requests per RFC 8484.
func (h *DoHHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "", http.StatusMethodNotAllowed)
		return
	}

	if r.Header.Get("Content-Type") != dnsMessageContentType {
		http.Error(w, "", http.StatusBadRequest)
		return
	}

	body, err := io.ReadAll(io.LimitReader(r.Body, maxDNSBodySize+1))
	if err != nil {
		http.Error(w, "", http.StatusBadRequest)
		return
	}

	if len(body) == 0 {
		http.Error(w, "", http.StatusBadRequest)
		return
	}

	if len(body) > maxDNSBodySize {
		http.Error(w, "", http.StatusBadRequest)
		return
	}

	respBody, statusCode, err := h.forwardToUpstream(r.Context(), body)
	if err != nil {
		http.Error(w, "", http.StatusBadGateway)
		return
	}

	w.Header().Set("Content-Type", dnsMessageContentType)
	w.WriteHeader(statusCode)
	_, _ = w.Write(respBody)
}

// forwardToUpstream tries upstreams starting at activeIdx, falling back to the
// next in the list on error. Updates activeIdx when a fallback succeeds.
// Returns the buffered response body, its HTTP status code, or an error.
func (h *DoHHandler) forwardToUpstream(ctx context.Context, dnsQuery []byte) ([]byte, int, error) {
	// Cap total fallback time to upstreamTimeout regardless of upstream count.
	ctx, cancel := context.WithTimeout(ctx, upstreamTimeout)
	defer cancel()

	h.mu.RLock()
	startIdx := h.activeIdx
	h.mu.RUnlock()

	n := len(h.upstreams)
	for i := 0; i < n; i++ {
		idx := (startIdx + i) % n
		probeCtx, probeCancel := context.WithTimeout(ctx, fallbackTimeout)
		body, status, err := h.doRequest(probeCtx, h.upstreams[idx], dnsQuery)
		probeCancel()
		if err == nil {
			if idx != startIdx {
				h.mu.Lock()
				h.activeIdx = idx
				h.mu.Unlock()
			}
			return body, status, nil
		}
	}
	return nil, 0, ErrUpstreamUnavailable
}

// doRequest sends a single DNS-over-HTTPS POST request to the given upstream URL.
// It buffers the full response body while the context is still valid and returns
// the body bytes and HTTP status code directly, avoiding intermediate allocations.
func (h *DoHHandler) doRequest(ctx context.Context, upstream string, dnsQuery []byte) ([]byte, int, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, upstream, bytes.NewReader(dnsQuery))
	if err != nil {
		return nil, 0, ErrUpstreamUnavailable
	}
	req.Header.Set("Content-Type", dnsMessageContentType)
	resp, err := h.client.Do(req)
	if err != nil {
		return nil, 0, ErrUpstreamUnavailable
	}
	defer resp.Body.Close()
	bodyBytes, err := io.ReadAll(io.LimitReader(resp.Body, maxDNSBodySize+1))
	if err != nil {
		return nil, 0, ErrUpstreamUnavailable
	}
	if len(bodyBytes) > maxDNSBodySize {
		return nil, 0, ErrUpstreamUnavailable
	}
	if resp.StatusCode >= 500 {
		return nil, 0, ErrUpstreamUnavailable
	}
	return bodyBytes, resp.StatusCode, nil
}

// dnsProbeQuery is a minimal DNS wire-format query for "." type A,
// used to probe upstream reachability with a valid payload.
// Read-only; must not be modified.
var dnsProbeQuery = []byte{
	0x00, 0x00, // ID
	0x01, 0x00, // Flags: standard query, RD
	0x00, 0x01, // QDCOUNT: 1
	0x00, 0x00, // ANCOUNT
	0x00, 0x00, // NSCOUNT
	0x00, 0x00, // ARCOUNT
	0x00,       // Root label (.)
	0x00, 0x01, // QTYPE: A
	0x00, 0x01, // QCLASS: IN
}

// isPrimaryReachable probes upstreams[0] with a minimal DNS query and returns
// true if it responds with a non-5xx HTTP status; only network errors and server
// errors return false.
func (h *DoHHandler) isPrimaryReachable(ctx context.Context) bool {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, h.upstreams[0], bytes.NewReader(dnsProbeQuery))
	if err != nil {
		return false
	}
	req.Header.Set("Content-Type", dnsMessageContentType)
	resp, err := h.client.Do(req)
	if err != nil {
		return false
	}
	_, _ = io.Copy(io.Discard, resp.Body)
	resp.Body.Close()
	return resp.StatusCode < 500
}
