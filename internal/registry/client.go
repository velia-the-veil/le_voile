package registry

import (
	"context"
	"crypto/ed25519"
	"encoding/base64"
	"fmt"
	"io"
	"net/http"
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
	refreshInterval    time.Duration
	allowHTTP          bool // test-only: bypass HTTPS enforcement
}

// ClientOption configures the Client.
type ClientOption func(*Client)

// WithHTTPClient sets a custom HTTP client.
func WithHTTPClient(c *http.Client) ClientOption {
	return func(cl *Client) {
		cl.httpClient = c
	}
}

// WithRefreshInterval sets the periodic refresh interval.
func WithRefreshInterval(d time.Duration) ClientOption {
	return func(cl *Client) {
		cl.refreshInterval = d
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
		refreshInterval: 1 * time.Hour,
	}
	for _, opt := range opts {
		opt(c)
	}

	// Enforce HTTPS in production. Tests can bypass via withAllowHTTP.
	if parsedURL.Scheme != "https" && !c.allowHTTP {
		return nil, fmt.Errorf("registry: client: registry URL must use HTTPS, got %q", parsedURL.Scheme)
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

	verified, err := reg.VerifyAll()
	if err != nil {
		return nil, err
	}

	// Sort by Added descending (most recent first).
	sort.Slice(verified, func(i, j int) bool {
		return verified[i].Added.After(verified[j].Added)
	})

	return verified, nil
}
