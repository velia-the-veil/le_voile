package dns

import (
	"context"
	"fmt"
	"net"
	"strings"
	"sync"
)

const (
	// maxUDPSize is the maximum DNS UDP payload size (EDNS0).
	maxUDPSize = 4096
	// maxConcurrent is the maximum number of concurrent query goroutines.
	maxConcurrent = 100
	// minDNSSize is the minimum valid DNS message size (header only).
	minDNSSize = 12
)

// BlocklistChecker is implemented by any component that can report whether
// a domain is blocked. Defined here to avoid circular imports between dns and blocklist.
type BlocklistChecker interface {
	IsBlocked(domain string) bool
	IsReady() bool
}

// QueryFunc sends a DNS wire-format payload through the tunnel and returns
// the wire-format response. It maps to tunnel.Client.SendDoHQuery.
type QueryFunc func(ctx context.Context, payload []byte) ([]byte, error)

// Proxy is a local UDP DNS proxy that forwards queries via a DoH tunnel.
type Proxy struct {
	listenAddr string
	queryFunc  QueryFunc
	connMu     sync.RWMutex
	conn       *net.UDPConn
	ready      chan struct{}
	blMu       sync.RWMutex
	blocklist  BlocklistChecker
}

// NewProxy creates a DNS proxy that listens on listenAddr and forwards
// queries using queryFunc.
func NewProxy(listenAddr string, queryFunc QueryFunc) *Proxy {
	return &Proxy{
		listenAddr: listenAddr,
		queryFunc:  queryFunc,
		ready:      make(chan struct{}),
	}
}

// SetBlocklist sets (or clears) the blocklist used for local NXDOMAIN filtering.
// Pass nil to disable filtering. Thread-safe.
func (p *Proxy) SetBlocklist(bl BlocklistChecker) {
	p.blMu.Lock()
	p.blocklist = bl
	p.blMu.Unlock()
}

// Ready returns a channel that is closed when the proxy has bound its socket
// and is ready to accept queries.
func (p *Proxy) Ready() <-chan struct{} {
	return p.ready
}

// Start begins listening for DNS queries and blocks until ctx is cancelled.
// Returns nil on graceful shutdown.
func (p *Proxy) Start(ctx context.Context) error {
	addr, err := net.ResolveUDPAddr("udp", p.listenAddr)
	if err != nil {
		return fmt.Errorf("dns: proxy resolve addr: %w", err)
	}

	conn, err := listenUDPReuseAddr("udp", addr)
	if err != nil {
		return fmt.Errorf("dns: proxy listen: %w", err)
	}
	p.connMu.Lock()
	p.conn = conn
	p.connMu.Unlock()

	// Signal readiness — socket is now bound and accepting packets.
	close(p.ready)

	// Close the connection when context is cancelled.
	go func() {
		<-ctx.Done()
		conn.Close()
	}()

	sem := make(chan struct{}, maxConcurrent)

	buf := make([]byte, maxUDPSize)
	for {
		n, clientAddr, err := conn.ReadFromUDP(buf)
		if err != nil {
			// Check if shutdown was requested.
			select {
			case <-ctx.Done():
				return nil
			default:
				return fmt.Errorf("dns: proxy read: %w", err)
			}
		}

		if n < minDNSSize {
			continue
		}

		// Copy payload for the goroutine.
		payload := make([]byte, n)
		copy(payload, buf[:n])

		sem <- struct{}{}
		go func(payload []byte, addr *net.UDPAddr) {
			defer func() { <-sem }()
			p.handleQuery(ctx, payload, addr)
		}(payload, clientAddr)
	}
}

// handleQuery forwards a single DNS query and writes the response back.
// If a blocklist is active and the queried domain is blocked, it replies
// immediately with NXDOMAIN without forwarding to the tunnel.
func (p *Proxy) handleQuery(ctx context.Context, payload []byte, clientAddr *net.UDPAddr) {
	p.blMu.RLock()
	bl := p.blocklist
	p.blMu.RUnlock()

	p.connMu.RLock()
	conn := p.conn
	p.connMu.RUnlock()

	if conn == nil {
		return
	}

	if bl != nil && bl.IsReady() {
		if domain := extractDomain(payload); domain != "" {
			if bl.IsBlocked(strings.ToLower(domain)) {
				if resp := buildNXDOMAINResponse(payload); resp != nil {
					conn.WriteToUDP(resp, clientAddr)
				}
				return
			}
		}
	}

	resp, err := p.queryFunc(ctx, payload)
	if err != nil {
		// Silently drop — no logging per architecture constraint.
		return
	}

	conn.WriteToUDP(resp, clientAddr)
}

// extractDomain parses the QNAME from a DNS wire-format query payload.
// The QNAME starts at byte offset 12 (after the fixed 12-byte header).
// Returns the domain name (e.g., "example.com") or "" on any parse error
// or if a compression pointer is encountered.
func extractDomain(payload []byte) string {
	if len(payload) <= minDNSSize {
		return ""
	}

	pos := minDNSSize
	var labels []string

	for pos < len(payload) {
		length := int(payload[pos])

		// Compression pointer: top two bits set (0xC0).
		if length&0xC0 == 0xC0 {
			return ""
		}

		// Root label — end of QNAME.
		if length == 0 {
			break
		}

		// Sanity check: label too long (max 63 per RFC 1035).
		if length > 63 {
			return ""
		}

		pos++
		end := pos + length
		if end > len(payload) {
			return ""
		}

		labels = append(labels, string(payload[pos:end]))
		pos = end
	}

	if len(labels) == 0 {
		return ""
	}
	return strings.Join(labels, ".")
}

// buildNXDOMAINResponse constructs a DNS NXDOMAIN response from the given query.
// Returns nil if the query is too short to be a valid DNS message.
func buildNXDOMAINResponse(query []byte) []byte {
	if len(query) < minDNSSize {
		return nil
	}

	// Find the end of the question section to truncate additional data.
	// Header (12 bytes) + QNAME + QTYPE (2) + QCLASS (2).
	pos := minDNSSize
	for pos < len(query) {
		length := int(query[pos])
		if length == 0 {
			pos++ // skip root label
			break
		}
		if length&0xC0 == 0xC0 {
			pos += 2 // skip compression pointer
			break
		}
		pos += 1 + length
	}
	pos += 4 // QTYPE + QCLASS
	if pos > len(query) {
		pos = len(query)
	}

	// Only copy header + question section, no stale additional data.
	resp := make([]byte, pos)
	copy(resp, query[:pos])

	// Byte 2: QR=1, AA=1 — preserve Opcode (bits 6-3) and RD (bit 0).
	resp[2] = (query[2] & 0x79) | 0x84

	// Byte 3: RCODE=3 (NXDOMAIN), all other flags (RA, Z, AD, CD) cleared.
	resp[3] = 0x03

	// Bytes 6-11: ANCOUNT=0, NSCOUNT=0, ARCOUNT=0.
	resp[6] = 0
	resp[7] = 0
	resp[8] = 0
	resp[9] = 0
	resp[10] = 0
	resp[11] = 0

	return resp
}
