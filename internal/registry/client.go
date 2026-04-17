package registry

import (
	"context"
	"crypto/ed25519"
	"crypto/tls"
	"encoding/base64"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"sort"
	"time"
)

// maxRegistrySize is the maximum allowed response body size (1 MB).
const maxRegistrySize = 1 << 20

// Client fetches and verifies the relay registry from a remote endpoint.
type Client struct {
	registryURL        string
	masterPubKey       ed25519.PublicKey
	masterPubKeyBase64 string
	httpClient         *http.Client
	httpClientSet      bool // tracks WithHTTPClient explicit override
	resolver           *DoHResolver
	refreshInterval    time.Duration
	rejectLogger       RejectLogger
	allowHTTP          bool // test-only: bypass HTTPS enforcement
}

// ClientOption configures the Client.
type ClientOption func(*Client)

// WithHTTPClient sets a custom HTTP client. Incompatible with WithResolver:
// NewClient returns an error if both are specified.
func WithHTTPClient(c *http.Client) ClientOption {
	return func(cl *Client) {
		cl.httpClient = c
		cl.httpClientSet = true
	}
}

// WithRefreshInterval sets the periodic refresh interval.
func WithRefreshInterval(d time.Duration) ClientOption {
	return func(cl *Client) {
		cl.refreshInterval = d
	}
}

// WithRejectLogger installs a callback invoked for each registry entry that
// fails verification. Useful for surfacing silent data corruption after a
// master-key rotation (NFR22h transient window). The logger receives only
// id/domain/reason — never binary content (NFR20/NFR22a).
func WithRejectLogger(logger RejectLogger) ClientOption {
	return func(cl *Client) {
		cl.rejectLogger = logger
	}
}

// WithResolver makes the Client dial the registry endpoint using IPs returned
// by the provided DoHResolver instead of the system DNS resolver. This is the
// bootstrap path guarded by NFR9i: the first resolution of the registry host
// happens over HTTPS (Cloudflare/Quad9) before the tunnel is established, so
// a compromised system resolver cannot redirect the client to a hostile relay.
//
// The TLS SNI remains the original hostname so certificate verification still
// targets the legitimate relay. If resolver is nil the option is a no-op.
// Incompatible with WithHTTPClient: NewClient errors if both are specified.
func WithResolver(resolver *DoHResolver) ClientOption {
	return func(cl *Client) {
		cl.resolver = resolver
	}
}

// dohDialContext returns a DialContext that resolves the host via resolver
// and dials the resulting IP. IP literals are dialed directly. Multi-record
// DNS round-robin: if resolver returns N addresses, dial is attempted against
// all of them until one succeeds.
func dohDialContext(resolver *DoHResolver, timeout time.Duration) func(ctx context.Context, network, addr string) (net.Conn, error) {
	dialer := &net.Dialer{Timeout: timeout, KeepAlive: 30 * time.Second}
	return func(ctx context.Context, network, addr string) (net.Conn, error) {
		host, port, err := net.SplitHostPort(addr)
		if err != nil {
			return nil, err
		}
		if _, err := netip.ParseAddr(host); err == nil {
			return dialer.DialContext(ctx, network, addr)
		}
		ips, err := resolver.ResolveAll(ctx, host)
		if err != nil {
			return nil, fmt.Errorf("registry: dial via doh: %w", err)
		}
		var lastErr error
		for _, ip := range ips {
			conn, derr := dialer.DialContext(ctx, network, net.JoinHostPort(ip.String(), port))
			if derr == nil {
				return conn, nil
			}
			lastErr = derr
		}
		return nil, fmt.Errorf("registry: dial via doh: all resolved addresses failed: %w", lastErr)
	}
}

// withAllowHTTP is a test-only option that bypasses the HTTPS enforcement.
// Exported only within the package for test use.
var withAllowHTTP = func(cl *Client) {
	cl.allowHTTP = true
}

// NewClient creates a registry client that fetches from the given URL and
// verifies relay signatures against the master public key.
func NewClient(registryURL string, masterPubKeyBase64 string, opts ...ClientOption) (*Client, error) {
	parsedURL, err := url.Parse(registryURL)
	if err != nil || parsedURL.Host == "" {
		return nil, fmt.Errorf("registry: client: invalid registry URL %q", registryURL)
	}

	keyBytes, err := base64.StdEncoding.DecodeString(masterPubKeyBase64)
	if err != nil {
		return nil, fmt.Errorf("registry: client: decode master key: %w", err)
	}
	if len(keyBytes) != ed25519.PublicKeySize {
		return nil, fmt.Errorf("registry: client: %w: invalid key length %d", ErrInvalidMasterKey, len(keyBytes))
	}

	c := &Client{
		registryURL:        registryURL,
		masterPubKey:       ed25519.PublicKey(keyBytes),
		masterPubKeyBase64: masterPubKeyBase64,
		httpClient: &http.Client{
			Timeout: 10 * time.Second,
		},
		// 6h refresh cadence (AC Story 4.3) — the relay list is re-ordered
		// every 6 hours; country changes trigger an immediate background
		// re-discover without waiting for the next tick.
		refreshInterval: 6 * time.Hour,
	}
	for _, opt := range opts {
		opt(c)
	}

	// Enforce HTTPS in production. Tests can bypass via withAllowHTTP.
	if parsedURL.Scheme != "https" && !c.allowHTTP {
		return nil, fmt.Errorf("registry: client: registry URL must use HTTPS, got %q", parsedURL.Scheme)
	}

	// WithResolver and WithHTTPClient are mutually exclusive: combining them
	// would silently drop one of the two configurations. Fail loudly.
	if c.resolver != nil && c.httpClientSet {
		return nil, fmt.Errorf("registry: client: WithResolver and WithHTTPClient are mutually exclusive")
	}

	// If a DoH resolver was supplied, compose the HTTP transport around it
	// AFTER all options have run. Budget = per-upstream timeout × upstream
	// count + 10 s for the actual registry HTTP round-trip, so the client
	// never times out mid-failover (M1).
	if c.resolver != nil {
		budget := c.resolver.UpstreamTimeout()*time.Duration(c.resolver.UpstreamCount()) + 10*time.Second
		c.httpClient = &http.Client{
			Transport: &http.Transport{
				ForceAttemptHTTP2: true,
				TLSClientConfig: &tls.Config{
					MinVersion: tls.VersionTLS13,
				},
				DialContext: dohDialContext(c.resolver, 10*time.Second),
			},
			Timeout: budget,
		}
	}

	return c, nil
}

// RefreshInterval returns the configured refresh interval.
func (c *Client) RefreshInterval() time.Duration {
	return c.refreshInterval
}

// MasterPublicKeyBase64 returns the master public key in base64 encoding.
func (c *Client) MasterPublicKeyBase64() string {
	return c.masterPubKeyBase64
}

// Fetch retrieves the registry, parses it, verifies all relay signatures,
// and returns verified relays sorted by Added (most recent first).
func (c *Client) Fetch(ctx context.Context) ([]RelayEntry, error) {
	url := c.registryURL + EndpointPath

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, fmt.Errorf("registry: fetch: %w", err)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("registry: fetch: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("registry: fetch: unexpected status %d", resp.StatusCode)
	}

	body, err := io.ReadAll(io.LimitReader(resp.Body, maxRegistrySize))
	if err != nil {
		return nil, fmt.Errorf("registry: fetch: %w", err)
	}

	reg, err := Parse(body)
	if err != nil {
		return nil, err
	}

	// Use the client's trusted master key, NOT the registry response key.
	// This prevents an attacker who controls the registry endpoint from
	// supplying their own master key and signing malicious relays.
	reg.MasterPublicKey = c.masterPubKeyBase64

	verified, err := reg.VerifyAllWithLogger(c.rejectLogger)
	if err != nil {
		return nil, err
	}

	// Sort by Added descending (most recent first).
	sort.Slice(verified, func(i, j int) bool {
		return verified[i].Added.After(verified[j].Added)
	})

	return verified, nil
}
