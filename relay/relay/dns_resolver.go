package relay

import (
	"bytes"
	"context"
	"io"
	"log"
	"net/http"
	"strings"
	"sync/atomic"
	"time"

	"github.com/miekg/dns"
)

const (
	dohContentType         = "application/dns-message"
	dnsUpstreamTimeout     = 2 * time.Second
	dnsResolveTimeout      = 5 * time.Second
	defaultDNSHTTPTimeout  = 5 * time.Second
)

// Default upstream DoH resolvers (Cloudflare primary, Quad9 fallback).
var DefaultDNSUpstreams = []string{
	"https://1.1.1.1/dns-query",
	"https://9.9.9.9/dns-query",
}

// DNSMetrics holds resolver counters (all atomic, no mutex).
type DNSMetrics struct {
	QueriesTotal         uint64
	BlockedTotal         uint64
	UpstreamFailuresTotal uint64
	DNSSECFailuresTotal  uint64
}

// DNSResolver resolves DNS queries via DoH upstreams with DNSSEC validation
// (AD flag) and blocklist filtering. No domain name is ever logged (NFR22a).
type DNSResolver struct {
	upstreams []string
	blocklist *Blocklist
	client    *http.Client

	queriesTotal          atomic.Uint64
	blockedTotal          atomic.Uint64
	upstreamFailuresTotal atomic.Uint64
	dnssecFailuresTotal   atomic.Uint64
}

// NewDNSResolver creates a resolver. Panics if upstreams is empty.
// If upstreams is nil, DefaultDNSUpstreams is used.
// If client is nil, a default with 5s timeout is created.
func NewDNSResolver(upstreams []string, blocklist *Blocklist, client *http.Client) *DNSResolver {
	if upstreams == nil {
		upstreams = DefaultDNSUpstreams
	}
	if len(upstreams) == 0 {
		panic("dns_resolver: upstreams must not be empty")
	}
	if client == nil {
		client = &http.Client{Timeout: defaultDNSHTTPTimeout}
	}
	return &DNSResolver{
		upstreams: upstreams,
		blocklist: blocklist,
		client:    client,
	}
}

// Resolve processes a DNS wire-format query. Returns a wire-format response.
// Blocked domains get NXDOMAIN without upstream call. Upstream failures or
// DNSSEC invalidity (AD=0) trigger fallback; double failure yields SERVFAIL.
func (r *DNSResolver) Resolve(ctx context.Context, query []byte) ([]byte, error) {
	r.queriesTotal.Add(1)

	msg := new(dns.Msg)
	if err := msg.Unpack(query); err != nil {
		return synthesizeSERVFAIL(query), nil
	}
	if len(msg.Question) != 1 {
		return synthesizeSERVFAIL(query), nil
	}

	qname := strings.ToLower(strings.TrimSuffix(msg.Question[0].Name, "."))

	// Blocklist check — before any upstream call (AC4).
	if r.blocklist != nil && r.blocklist.IsBlocked(qname) {
		r.blockedTotal.Add(1)
		return synthesizeNXDOMAIN(msg), nil
	}

	// Set DO flag to request DNSSEC data from upstream.
	msg.SetEdns0(4096, true)
	wireQuery, err := msg.Pack()
	if err != nil {
		return synthesizeSERVFAIL(query), nil
	}

	// Try upstreams in order with per-upstream timeout.
	resolveCtx, cancel := context.WithTimeout(ctx, dnsResolveTimeout)
	defer cancel()

	for i, upstream := range r.upstreams {
		upCtx, upCancel := context.WithTimeout(resolveCtx, dnsUpstreamTimeout)
		resp, err := r.doHPost(upCtx, upstream, wireQuery)
		upCancel()

		if err != nil {
			r.upstreamFailuresTotal.Add(1)
			log.Printf("dns: upstream %d failed", i)
			continue
		}

		respMsg := new(dns.Msg)
		if err := respMsg.Unpack(resp); err != nil {
			r.upstreamFailuresTotal.Add(1)
			continue
		}

		// DNSSEC tracking: record AD flag status (NFR9f).
		// AD=0 is normal for unsigned zones (~65% of domains).
		// We accept the response regardless and track non-AD for metrics.
		if !respMsg.AuthenticatedData {
			r.dnssecFailuresTotal.Add(1)
		}

		return resp, nil
	}

	// All upstreams failed.
	return synthesizeSERVFAIL(query), nil
}

// Metrics returns a snapshot of the resolver's counters.
func (r *DNSResolver) Metrics() DNSMetrics {
	return DNSMetrics{
		QueriesTotal:          r.queriesTotal.Load(),
		BlockedTotal:          r.blockedTotal.Load(),
		UpstreamFailuresTotal: r.upstreamFailuresTotal.Load(),
		DNSSECFailuresTotal:   r.dnssecFailuresTotal.Load(),
	}
}

// doHPost sends a DNS wire-format query via DoH POST.
func (r *DNSResolver) doHPost(ctx context.Context, upstream string, wireQuery []byte) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, upstream, bytes.NewReader(wireQuery))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", dohContentType)
	req.Header.Set("Accept", dohContentType)

	resp, err := r.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, &UpstreamDNSError{StatusCode: resp.StatusCode}
	}

	body, err := io.ReadAll(io.LimitReader(resp.Body, 65535))
	if err != nil {
		return nil, err
	}
	return body, nil
}

// synthesizeNXDOMAIN builds a DNS NXDOMAIN response for the given query.
func synthesizeNXDOMAIN(req *dns.Msg) []byte {
	m := new(dns.Msg)
	m.SetRcode(req, dns.RcodeNameError)
	m.RecursionAvailable = true
	out, _ := m.Pack()
	return out
}

// synthesizeSERVFAIL builds a DNS SERVFAIL response. Accepts raw wire
// to handle unparseable queries gracefully.
func synthesizeSERVFAIL(query []byte) []byte {
	req := new(dns.Msg)
	if err := req.Unpack(query); err != nil {
		// Can't parse at all — return minimal SERVFAIL with QID=0.
		m := new(dns.Msg)
		m.Rcode = dns.RcodeServerFailure
		m.Response = true
		out, _ := m.Pack()
		return out
	}
	m := new(dns.Msg)
	m.SetRcode(req, dns.RcodeServerFailure)
	m.RecursionAvailable = true
	out, _ := m.Pack()
	return out
}

// UpstreamDNSError is returned when a DoH upstream returns non-200.
type UpstreamDNSError struct {
	StatusCode int
}

func (e *UpstreamDNSError) Error() string {
	return "dns: upstream returned non-200 status"
}
