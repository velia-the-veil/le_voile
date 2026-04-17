package registry

import (
	"bytes"
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"time"

	"golang.org/x/net/dns/dnsmessage"
)

// Default DoH upstream endpoints (RFC 8484, POST application/dns-message).
const (
	CloudflareDoH = "https://cloudflare-dns.com/dns-query"
	Quad9DoH      = "https://dns.quad9.net/dns-query"
)

// dohBootstrapIPs maps DoH upstream hostnames to hardcoded bootstrap IPs.
// Used when the system resolver cannot be trusted even for resolving the DoH
// endpoint itself. The first IP is tried; SNI is preserved as the original
// hostname so TLS certificate verification still works.
var dohBootstrapIPs = map[string][]string{
	"cloudflare-dns.com": {"1.1.1.1", "1.0.0.1"},
	"dns.quad9.net":      {"9.9.9.9", "149.112.112.112"},
}

// Sentinel errors for the DoH resolver.
var (
	ErrDoHUpstreamUnreachable = errors.New("registry: doh: upstream unreachable")
	ErrAllDoHUpstreamsDown    = errors.New("registry: doh: all upstreams down")
	ErrDoHInvalidResponse     = errors.New("registry: doh: invalid response")
	ErrDoHNoAddressRecord     = errors.New("registry: doh: no A/AAAA record in response")
	ErrDoHPrivateAddress      = errors.New("registry: doh: upstream returned private/loopback address")
	ErrDoHNXDOMAIN            = errors.New("registry: doh: upstream returned NXDOMAIN")
	ErrDoHServerFailure       = errors.New("registry: doh: upstream returned SERVFAIL")
)

// DoHResolver resolves hostnames over HTTPS (RFC 8484) with upstream failover.
// On resolution failure of every upstream, it does NOT fall back to the system
// resolver — the caller receives ErrAllDoHUpstreamsDown and must decide.
type DoHResolver struct {
	upstreams    []string
	timeout      time.Duration
	httpClient   *http.Client
	logger       func(format string, args ...any)
	allowPrivate bool // tests only — permit loopback/private addrs
}

// DoHOption configures a DoHResolver.
type DoHOption func(*DoHResolver)

// WithDoHUpstreams overrides the default upstream list. Must be HTTPS URLs.
// Upstreams are tried in order; the first success wins.
func WithDoHUpstreams(upstreams []string) DoHOption {
	return func(r *DoHResolver) {
		r.upstreams = append([]string(nil), upstreams...)
	}
}

// WithDoHTimeout sets the per-upstream request timeout.
func WithDoHTimeout(d time.Duration) DoHOption {
	return func(r *DoHResolver) {
		r.timeout = d
	}
}

// WithDoHHTTPClient injects a custom HTTP client (primarily for tests).
// When provided, the resolver does NOT build its own transport.
func WithDoHHTTPClient(c *http.Client) DoHOption {
	return func(r *DoHResolver) {
		r.httpClient = c
	}
}

// WithDoHLogger sets a log callback invoked for failures. The callback MUST
// NOT receive client IP data — only (domain, upstream, error type). Default
// is a no-op.
func WithDoHLogger(fn func(format string, args ...any)) DoHOption {
	return func(r *DoHResolver) {
		r.logger = fn
	}
}

// withDoHAllowPrivate permits loopback/private/link-local addresses in
// resolver responses. Test-only — production MUST reject them.
var withDoHAllowPrivate = func(r *DoHResolver) {
	r.allowPrivate = true
}

// NewDoHResolver creates a resolver with Cloudflare + Quad9 as defaults.
// Returns an error if any upstream URL is not a valid HTTPS endpoint.
func NewDoHResolver(opts ...DoHOption) (*DoHResolver, error) {
	r := &DoHResolver{
		upstreams: []string{CloudflareDoH, Quad9DoH},
		timeout:   5 * time.Second,
		logger:    func(string, ...any) {},
	}
	for _, opt := range opts {
		opt(r)
	}
	if err := validateUpstreams(r.upstreams); err != nil {
		return nil, err
	}
	if len(r.upstreams) == 0 {
		return nil, fmt.Errorf("registry: doh: at least one upstream required")
	}
	if r.httpClient == nil {
		r.httpClient = &http.Client{
			Transport: newDoHTransport(r.timeout),
			Timeout:   r.timeout,
		}
	}
	return r, nil
}

// MustNewDoHResolver is like NewDoHResolver but panics on invalid upstreams.
// Reserved for trusted-defaults call sites (service bootstrap) where failure
// is a configuration bug, not a runtime condition.
func MustNewDoHResolver(opts ...DoHOption) *DoHResolver {
	r, err := NewDoHResolver(opts...)
	if err != nil {
		panic(err)
	}
	return r
}

// UpstreamTimeout returns the per-upstream request timeout (for callers
// that need to compute an overall budget, e.g. the registry HTTP client).
func (r *DoHResolver) UpstreamTimeout() time.Duration { return r.timeout }

// UpstreamCount returns the number of configured upstreams, so callers
// can size their enclosing timeout budget.
func (r *DoHResolver) UpstreamCount() int { return len(r.upstreams) }

// Resolve returns the first A (IPv4) address for host, falling back to AAAA
// (IPv6) if no A record is present. Upstreams are tried in order until one
// returns a valid non-private address.
//
// On failure, the returned error joins ErrAllDoHUpstreamsDown with the
// last upstream's error so callers can use errors.Is for any of the
// sentinel error types (ErrDoHPrivateAddress, ErrDoHNoAddressRecord,
// ErrDoHNXDOMAIN, ErrDoHServerFailure, ErrDoHUpstreamUnreachable).
func (r *DoHResolver) Resolve(ctx context.Context, host string) (netip.Addr, error) {
	addrs, err := r.ResolveAll(ctx, host)
	if err != nil {
		return netip.Addr{}, err
	}
	return addrs[0], nil
}

// ResolveAll returns every A/AAAA address a successful upstream provides,
// in the order the resolver received them. Callers can randomize for load
// distribution. If a DNS round-robin maps one hostname to many relays, this
// exposes all of them.
func (r *DoHResolver) ResolveAll(ctx context.Context, host string) ([]netip.Addr, error) {
	if host == "" {
		return nil, fmt.Errorf("registry: doh: empty host")
	}
	if addr, err := netip.ParseAddr(host); err == nil {
		return []netip.Addr{addr}, nil
	}

	var lastErr error
	for _, upstream := range r.upstreams {
		addrs, err := r.queryUpstream(ctx, upstream, host)
		if err == nil && len(addrs) > 0 {
			return addrs, nil
		}
		r.logger("registry: doh: upstream %q failed for %q: %v", upstream, host, err)
		lastErr = err
		// NXDOMAIN is authoritative — no point trying other upstreams.
		if errors.Is(err, ErrDoHNXDOMAIN) {
			return nil, errors.Join(ErrAllDoHUpstreamsDown, err)
		}
	}
	if lastErr == nil {
		return nil, ErrAllDoHUpstreamsDown
	}
	return nil, errors.Join(ErrAllDoHUpstreamsDown, lastErr)
}

// queryUpstream issues DoH queries for host on upstream, returning all
// valid A/AAAA addresses. It first tries A; if none are present, it tries
// AAAA. Private/loopback/link-local addresses are filtered out.
func (r *DoHResolver) queryUpstream(ctx context.Context, upstream, host string) ([]netip.Addr, error) {
	addrs, err := r.queryType(ctx, upstream, host, dnsmessage.TypeA)
	if err == nil && len(addrs) > 0 {
		return addrs, nil
	}
	// On NXDOMAIN the zone has no records at all — don't retry AAAA.
	if errors.Is(err, ErrDoHNXDOMAIN) || errors.Is(err, ErrDoHServerFailure) {
		return nil, err
	}
	if err != nil && !errors.Is(err, ErrDoHNoAddressRecord) {
		return nil, err
	}
	addrs6, err6 := r.queryType(ctx, upstream, host, dnsmessage.TypeAAAA)
	if err6 == nil && len(addrs6) > 0 {
		return addrs6, nil
	}
	if err != nil {
		return nil, err
	}
	return nil, err6
}

func (r *DoHResolver) queryType(ctx context.Context, upstream, host string, qtype dnsmessage.Type) ([]netip.Addr, error) {
	wire, err := buildDNSQuery(host, qtype)
	if err != nil {
		return nil, fmt.Errorf("registry: doh: build query: %w", err)
	}

	reqCtx, cancel := context.WithTimeout(ctx, r.timeout)
	defer cancel()

	req, err := http.NewRequestWithContext(reqCtx, http.MethodPost, upstream, bytes.NewReader(wire))
	if err != nil {
		return nil, fmt.Errorf("registry: doh: new request: %w", err)
	}
	req.Header.Set("Content-Type", "application/dns-message")
	req.Header.Set("Accept", "application/dns-message")

	resp, err := r.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("%w: %w", ErrDoHUpstreamUnreachable, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("%w: http %d", ErrDoHUpstreamUnreachable, resp.StatusCode)
	}

	// Cap at 4 KiB — DoH responses are small (A/AAAA query).
	body, err := io.ReadAll(io.LimitReader(resp.Body, 4096))
	if err != nil {
		return nil, fmt.Errorf("%w: %w", ErrDoHInvalidResponse, err)
	}

	addrs, err := parseDNSAnswer(body, qtype)
	if err != nil {
		return nil, err
	}

	// Defense in depth: reject private/loopback/link-local answers —
	// a compromised DoH upstream could redirect the bootstrap to localhost.
	filtered := make([]netip.Addr, 0, len(addrs))
	for _, a := range addrs {
		if !r.allowPrivate && isRejectableAddr(a) {
			continue
		}
		filtered = append(filtered, a)
	}
	if len(filtered) == 0 {
		if len(addrs) > 0 {
			return nil, ErrDoHPrivateAddress
		}
		return nil, ErrDoHNoAddressRecord
	}
	return filtered, nil
}

// buildDNSQuery encodes a DNS query for host/qtype as wireformat.
func buildDNSQuery(host string, qtype dnsmessage.Type) ([]byte, error) {
	name, err := dnsmessage.NewName(dnsToFQDN(host))
	if err != nil {
		return nil, err
	}
	msg := dnsmessage.Message{
		Header: dnsmessage.Header{
			ID:               0, // Per RFC 8484 §4.1, ID SHOULD be 0 for DoH.
			RecursionDesired: true,
		},
		Questions: []dnsmessage.Question{{
			Name:  name,
			Type:  qtype,
			Class: dnsmessage.ClassINET,
		}},
	}
	return msg.Pack()
}

// parseDNSAnswer extracts every A or AAAA address matching qtype from a DoH
// response. It honors the DNS RCODE: NXDOMAIN and SERVFAIL short-circuit
// as distinct sentinel errors so callers can avoid pointless retries.
func parseDNSAnswer(body []byte, qtype dnsmessage.Type) ([]netip.Addr, error) {
	var parser dnsmessage.Parser
	hdr, err := parser.Start(body)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrDoHInvalidResponse, err)
	}
	// RFC 1035 §4.1.1 — honor the response code.
	switch hdr.RCode {
	case dnsmessage.RCodeSuccess:
		// continue
	case dnsmessage.RCodeNameError:
		return nil, ErrDoHNXDOMAIN
	case dnsmessage.RCodeServerFailure:
		return nil, ErrDoHServerFailure
	default:
		return nil, fmt.Errorf("%w: rcode %d", ErrDoHInvalidResponse, hdr.RCode)
	}
	if err := parser.SkipAllQuestions(); err != nil {
		return nil, fmt.Errorf("%w: %v", ErrDoHInvalidResponse, err)
	}
	var addrs []netip.Addr
	for {
		ahdr, err := parser.AnswerHeader()
		if errors.Is(err, dnsmessage.ErrSectionDone) {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("%w: %v", ErrDoHInvalidResponse, err)
		}
		switch {
		case ahdr.Type == dnsmessage.TypeA && qtype == dnsmessage.TypeA:
			a, err := parser.AResource()
			if err != nil {
				return nil, fmt.Errorf("%w: %v", ErrDoHInvalidResponse, err)
			}
			addrs = append(addrs, netip.AddrFrom4(a.A))
		case ahdr.Type == dnsmessage.TypeAAAA && qtype == dnsmessage.TypeAAAA:
			aaaa, err := parser.AAAAResource()
			if err != nil {
				return nil, fmt.Errorf("%w: %v", ErrDoHInvalidResponse, err)
			}
			addrs = append(addrs, netip.AddrFrom16(aaaa.AAAA))
		default:
			if err := parser.SkipAnswer(); err != nil {
				return nil, fmt.Errorf("%w: %v", ErrDoHInvalidResponse, err)
			}
		}
	}
	if len(addrs) == 0 {
		return nil, ErrDoHNoAddressRecord
	}
	return addrs, nil
}

// newDoHTransport builds an http.Transport that dials DoH endpoints using
// hardcoded bootstrap IPs when the hostname is a well-known DoH provider,
// bypassing the system resolver entirely. SNI is preserved via tlsConfig
// ServerName so certificate validation still targets the real hostname.
func newDoHTransport(timeout time.Duration) *http.Transport {
	dialer := &net.Dialer{Timeout: timeout, KeepAlive: 30 * time.Second}
	return &http.Transport{
		ForceAttemptHTTP2: true,
		TLSClientConfig: &tls.Config{
			MinVersion: tls.VersionTLS13,
		},
		DialContext: func(ctx context.Context, network, addr string) (net.Conn, error) {
			host, port, err := net.SplitHostPort(addr)
			if err != nil {
				return nil, err
			}
			// If already an IP literal, dial direct.
			if _, err := netip.ParseAddr(host); err == nil {
				return dialer.DialContext(ctx, network, addr)
			}
			// Hardcoded bootstrap IPs for well-known DoH providers.
			if bootstraps, ok := dohBootstrapIPs[host]; ok {
				var lastErr error
				for _, ip := range bootstraps {
					conn, derr := dialer.DialContext(ctx, network, net.JoinHostPort(ip, port))
					if derr == nil {
						return conn, nil
					}
					lastErr = derr
				}
				// Fall through to system resolver if all hardcoded IPs fail.
				conn, derr := dialer.DialContext(ctx, network, addr)
				if derr != nil && lastErr != nil {
					return nil, fmt.Errorf("%v (bootstrap fail: %v)", derr, lastErr)
				}
				return conn, derr
			}
			return dialer.DialContext(ctx, network, addr)
		},
	}
}

// dnsToFQDN ensures the host ends with a trailing dot, as required by
// dnsmessage.NewName (which wants a fully qualified DNS name).
func dnsToFQDN(host string) string {
	if len(host) > 0 && host[len(host)-1] == '.' {
		return host
	}
	return host + "."
}

// isRejectableAddr returns true for addresses that MUST NOT be treated as a
// legitimate public relay endpoint. Defends against a hostile DoH upstream
// redirecting the bootstrap to a private/loopback target.
func isRejectableAddr(addr netip.Addr) bool {
	if !addr.IsValid() {
		return true
	}
	if addr.IsLoopback() || addr.IsUnspecified() || addr.IsMulticast() {
		return true
	}
	if addr.IsLinkLocalUnicast() || addr.IsLinkLocalMulticast() {
		return true
	}
	if addr.IsPrivate() {
		return true
	}
	return false
}

// validateUpstreams verifies every upstream URL is a valid HTTPS endpoint.
// Returns the first offending upstream or nil.
func validateUpstreams(upstreams []string) error {
	for _, u := range upstreams {
		parsed, err := url.Parse(u)
		if err != nil || parsed.Scheme != "https" || parsed.Host == "" {
			return fmt.Errorf("registry: doh: invalid upstream %q", u)
		}
	}
	return nil
}

