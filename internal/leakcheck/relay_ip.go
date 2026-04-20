package leakcheck

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/netip"
	"sync"
	"time"
)

// defaultRelayIPCacheTTL is the TTL for the cached relay IP. The relay
// domain's DNS resolves directly to the VPS origin IP (DNS-only, no CDN
// fronting); a 5-minute cache balances reactivity to failover with DoH
// traffic economy.
const defaultRelayIPCacheTTL = 5 * time.Minute

// DoHResolver abstracts the minimum surface needed from
// internal/registry.DoHResolver so the leakcheck package stays test-isolated
// and doesn't take a direct package dependency on registry.
//
// The concrete implementation in internal/registry returns netip.Addr; this
// interface mirrors that signature so no adapter is required.
type DoHResolver interface {
	Resolve(ctx context.Context, host string) (netip.Addr, error)
}

// ErrRelayDomainEmpty is returned when the resolver is constructed without a
// domain function, or when the function returns an empty string at resolve
// time (the active relay domain hasn't been set yet, or was cleared).
var ErrRelayDomainEmpty = errors.New("leakcheck: relay domain is empty")

// RelayIPResolver resolves the active relay's public IP via DoH and caches
// the result for a short TTL so each leak-check cycle doesn't hit the DoH
// upstream. Callers inject a DoHResolver to keep the leakcheck package
// free of a hard dependency on the registry package.
//
// The domain is read through a callback on every ExpectedIP() call so the
// resolver automatically tracks country switches and inter-country failovers
// (the Client.RelayDomain method value is the typical source). The cache is
// keyed by domain — a domain change auto-invalidates the cached IP, closing
// the false-LEAK_DETECTED window that a figé domain would leave open.
type RelayIPResolver struct {
	domainFn func() string
	resolver DoHResolver
	ttl      time.Duration

	mu           sync.Mutex
	cachedIP     net.IP
	cachedDomain string
	expiresAt    time.Time
	// nowFn is injectable so tests can advance time without sleeping.
	nowFn func() time.Time
}

// NewRelayIPResolver creates a resolver backed by domainFn (evaluated on
// every ExpectedIP call) using the supplied DoH resolver. Returns
// ErrRelayDomainEmpty if domainFn is nil. A nil DoH resolver is accepted
// at construction but every ExpectedIP call will return an error — the
// service is expected to provide a working resolver or disable the leak
// scheduler entirely.
func NewRelayIPResolver(domainFn func() string, resolver DoHResolver) (*RelayIPResolver, error) {
	if domainFn == nil {
		return nil, ErrRelayDomainEmpty
	}
	return &RelayIPResolver{
		domainFn: domainFn,
		resolver: resolver,
		ttl:      defaultRelayIPCacheTTL,
		nowFn:    time.Now,
	}, nil
}

// WithTTL overrides the default cache TTL (primarily for tests).
func (r *RelayIPResolver) WithTTL(ttl time.Duration) *RelayIPResolver {
	r.ttl = ttl
	return r
}

// WithNowFunc overrides the time source (tests only).
func (r *RelayIPResolver) WithNowFunc(fn func() time.Time) *RelayIPResolver {
	if fn != nil {
		r.nowFn = fn
	}
	return r
}

// ExpectedIP returns the relay's public IP. It serves a cached value until
// TTL expires OR the domain changes, then re-queries DoH. On DoH failure
// the previous cache is NOT reused — the caller receives an error so a
// stale value cannot turn a real leak into a false OK.
func (r *RelayIPResolver) ExpectedIP(ctx context.Context) (net.IP, error) {
	domain := r.domainFn()
	if domain == "" {
		return nil, ErrRelayDomainEmpty
	}

	r.mu.Lock()
	if r.cachedIP != nil && r.cachedDomain == domain && r.nowFn().Before(r.expiresAt) {
		ip := cloneIP(r.cachedIP)
		r.mu.Unlock()
		return ip, nil
	}
	r.mu.Unlock()

	if r.resolver == nil {
		return nil, fmt.Errorf("leakcheck: relay ip resolver: no DoH resolver configured")
	}

	addr, err := r.resolver.Resolve(ctx, domain)
	if err != nil {
		return nil, fmt.Errorf("leakcheck: relay ip resolver: %w", err)
	}
	if !addr.IsValid() {
		return nil, fmt.Errorf("leakcheck: relay ip resolver: upstream returned no ip for %s", domain)
	}

	ip := net.IP(addr.AsSlice())
	if ip == nil {
		return nil, fmt.Errorf("leakcheck: relay ip resolver: upstream returned no ip for %s", domain)
	}

	r.mu.Lock()
	r.cachedIP = ip
	r.cachedDomain = domain
	r.expiresAt = r.nowFn().Add(r.ttl)
	r.mu.Unlock()

	return cloneIP(ip), nil
}

// Invalidate drops the cached IP so the next ExpectedIP call re-queries
// DoH. Redundant for domain-change scenarios (ExpectedIP auto-invalidates
// when domainFn returns a different value) but kept for cases where the
// caller wants a fresh lookup even for the same domain (e.g. intra-country
// failover where the relay changed but the domain happens to match, or a
// defensive refresh after a TUN recovery).
func (r *RelayIPResolver) Invalidate() {
	r.mu.Lock()
	r.cachedIP = nil
	r.cachedDomain = ""
	r.expiresAt = time.Time{}
	r.mu.Unlock()
}

func cloneIP(ip net.IP) net.IP {
	out := make(net.IP, len(ip))
	copy(out, ip)
	return out
}
