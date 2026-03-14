package tunnel

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/quic-go/quic-go"
	"github.com/quic-go/quic-go/http3"

	lecrypto "github.com/velia-the-veil/le_voile/internal/crypto"
)

var (
	ErrVerificationFailed = errors.New("tunnel: relay verification failed")
	ErrNotConnected       = errors.New("tunnel: not connected")
	ErrConnectionTimeout  = errors.New("tunnel: connection timeout")
	ErrPinningFailed      = errors.New("tunnel: certificate pinning failed")
)

const (
	connectTimeout     = 3 * time.Second
	dohTimeout         = 5 * time.Second
	stunRelayTimeout   = 5 * time.Second
	nonceSize          = 32
	maxCertChainLength = 3   // reject certificate chains longer than this
	maxDoHResponseSize = 64 * 1024  // 64 KB — well above the 65535-byte DNS UDP limit
	maxSTUNResponseSize = 1600      // slightly above typical 1500-byte STUN messages
)

// verifyRequest is the JSON body sent to the relay /verify endpoint.
type verifyRequest struct {
	Nonce string `json:"nonce"`
}

// verifyResponse is the JSON reply from the relay /verify endpoint.
type verifyResponse struct {
	Signature    string `json:"signature"`
	SessionToken string `json:"session_token,omitempty"`
}

// ErrTokenExpired is returned when the session token has expired and refresh failed.
var ErrTokenExpired = errors.New("tunnel: session token expired")

// Token refresh constants.
const (
	tokenRefreshMargin    = 5 * time.Minute  // refresh 5min before expiry
	tokenRefreshBackoffInit = 1 * time.Second
	tokenRefreshBackoffMax  = 60 * time.Second
	tokenCircuitBreakerMax  = 5  // consecutive failures before circuit breaker opens
	tokenCircuitBreakerPause = 60 * time.Second
)

// Client manages an HTTP/3 tunnel connection to the relay.
type Client struct {
	mu          sync.RWMutex
	relayDomain string
	relayIP     string // resolved IP of the relay, used to bypass system DNS
	relayPubKey ed25519.PublicKey
	httpClient  *http.Client
	transport   *http3.Transport
	state       *StateManager

	// Session token for proxy CONNECT authentication.
	sessionToken       string // raw token string
	sessionTokenIssued int64  // unix timestamp
	sessionTokenTTL    int64  // seconds

	// Token refresh single-flight + circuit breaker.
	refreshMu       sync.Mutex
	refreshFailures int
	refreshBackoff  time.Duration
	circuitOpen     bool
	circuitOpenedAt time.Time
}

// NewClient creates a tunnel client configured for HTTP/3 with TLS 1.3.
// Use WithInsecure(true) to skip TLS certificate verification (development only).
func NewClient(relayDomain string, relayPubKeyBase64 string, opts ...ClientOption) (*Client, error) {
	o := clientOptions{}
	for _, opt := range opts {
		opt(&o)
	}

	pubKey, err := lecrypto.ImportPublicKeyBase64(relayPubKeyBase64)
	if err != nil {
		return nil, fmt.Errorf("tunnel: new client: %w", err)
	}

	// Resolve relay IP at startup, BEFORE system DNS is redirected to the local
	// proxy. This prevents a deadlock where the tunnel needs DNS to connect, but
	// DNS is routed through the tunnel that isn't connected yet.
	relayIP := relayDomain
	if ip := net.ParseIP(relayDomain); ip == nil {
		// Domain name, not an IP — resolve it now.
		ips, err := net.LookupIP(relayDomain)
		if err != nil || len(ips) == 0 {
			return nil, fmt.Errorf("tunnel: resolve relay %q: %w", relayDomain, err)
		}
		relayIP = ips[0].String()
	}

	// Create Client first so the TLS closure can read c.relayPubKey under lock,
	// ensuring UpdateRelay updates are visible to future TLS handshakes.
	c := &Client{
		relayDomain: relayDomain,
		relayIP:     relayIP,
		relayPubKey: pubKey,
		state:       NewStateManager(),
	}

	tr := &http3.Transport{
		TLSClientConfig: &tls.Config{
			ServerName: relayDomain, // SNI must match the cert, not the IP
			NextProtos:         []string{http3.NextProtoH3},
			MinVersion:         tls.VersionTLS13,
			InsecureSkipVerify: o.insecure || o.skipCAOnly,
			VerifyPeerCertificate: func(rawCerts [][]byte, _ [][]*x509.Certificate) error {
				if o.insecure {
					return nil // development mode bypass — never set in production builds
				}
				if len(rawCerts) == 0 {
					return ErrPinningFailed
				}
				// Reject suspiciously long certificate chains.
				if len(rawCerts) > maxCertChainLength {
					return fmt.Errorf("%w: chain too long (%d certs)", ErrPinningFailed, len(rawCerts))
				}
				cert, err := x509.ParseCertificate(rawCerts[0])
				if err != nil {
					return fmt.Errorf("tunnel: parse cert: %w", err)
				}
				c.mu.RLock()
				pinnedKey := c.relayPubKey
				c.mu.RUnlock()
				if err := lecrypto.VerifyEd25519CertPin(cert, pinnedKey); err != nil {
					return fmt.Errorf("%w: %v", ErrPinningFailed, err)
				}
				return nil
			},
		},
		QUICConfig: &quic.Config{
			MaxIdleTimeout:  180 * time.Second, // 3 min idle before disconnect
			KeepAlivePeriod: 30 * time.Second,  // ping every 30s to prevent NAT/firewall timeout
		},
	}

	c.httpClient = &http.Client{Transport: tr}
	c.transport = tr

	return c, nil
}

// clientOptions holds optional client configuration.
type clientOptions struct {
	insecure   bool
	skipCAOnly bool // skip CA verification only; pinning still enforced. For tests with self-signed certs.
}

// ClientOption configures optional client behavior.
type ClientOption func(*clientOptions)

// WithInsecure skips TLS certificate verification. Development use only.
func WithInsecure(insecure bool) ClientOption {
	return func(o *clientOptions) { o.insecure = insecure }
}

// WithInsecureSkipCAOnly skips TLS CA verification but still enforces certificate pinning.
// Use in tests that need self-signed certificates while validating the pinning code path.
func WithInsecureSkipCAOnly() ClientOption {
	return func(o *clientOptions) { o.skipCAOnly = true }
}

// relayURL builds a URL using the resolved IP to avoid DNS lookups.
// QUIC uses the TLS ServerName (set in the transport config) for SNI.
func (c *Client) relayURL(path string) string {
	c.mu.RLock()
	ip := c.relayIP
	c.mu.RUnlock()
	host := ip
	if strings.Contains(ip, ":") {
		host = "[" + ip + "]"
	}
	return "https://" + host + path
}

// Connect establishes the tunnel by verifying the relay's Ed25519 identity.
func (c *Client) Connect(ctx context.Context) error {
	c.state.Set(StateConnecting)

	ctx, cancel := context.WithTimeout(ctx, connectTimeout)
	defer cancel()

	if err := c.verifyRelay(ctx); err != nil {
		c.state.Set(StateDisconnected)
		if ctx.Err() != nil {
			return ErrConnectionTimeout
		}
		return err
	}

	c.state.Set(StateConnected)
	return nil
}

// SendDoHQuery sends a DNS wire-format query through the tunnel.
func (c *Client) SendDoHQuery(ctx context.Context, dnsPayload []byte) ([]byte, error) {
	if c.state.Get() != StateConnected {
		return nil, ErrNotConnected
	}

	ctx, cancel := context.WithTimeout(ctx, dohTimeout)
	defer cancel()

	url := c.relayURL("/dns-query")
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(dnsPayload))
	if err != nil {
		return nil, fmt.Errorf("tunnel: send doh: %w", err)
	}
	req.Header.Set("Content-Type", "application/dns-message")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("tunnel: send doh: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("tunnel: send doh: server returned status %d", resp.StatusCode)
	}

	body, err := io.ReadAll(io.LimitReader(resp.Body, maxDoHResponseSize))
	if err != nil {
		return nil, fmt.Errorf("tunnel: send doh: read response: %w", err)
	}

	return body, nil
}

// SendSTUNRelay sends a STUN packet through the tunnel to be relayed by the VPS.
func (c *Client) SendSTUNRelay(ctx context.Context, stunPacket []byte, targetAddr string) ([]byte, error) {
	if c.state.Get() != StateConnected {
		return nil, ErrNotConnected
	}

	ctx, cancel := context.WithTimeout(ctx, stunRelayTimeout)
	defer cancel()

	url := c.relayURL("/stun-relay")
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(stunPacket))
	if err != nil {
		return nil, fmt.Errorf("tunnel: send stun relay: %w", err)
	}
	req.Header.Set("Content-Type", "application/stun-message")
	req.Header.Set("X-Stun-Target", targetAddr)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("tunnel: send stun relay: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("tunnel: send stun relay: server returned status %d", resp.StatusCode)
	}

	body, err := io.ReadAll(io.LimitReader(resp.Body, maxSTUNResponseSize))
	if err != nil {
		return nil, fmt.Errorf("tunnel: send stun relay: read response: %w", err)
	}

	return body, nil
}

// Disconnect closes the tunnel and releases QUIC resources.
func (c *Client) Disconnect() error {
	c.state.Set(StateDisconnected)
	if err := c.transport.Close(); err != nil {
		return fmt.Errorf("tunnel: disconnect: %w", err)
	}
	return nil
}

// State returns the tunnel's state manager.
func (c *Client) State() *StateManager {
	return c.state
}

// HTTPClient returns the underlying HTTP client for test injection.
func (c *Client) HTTPClient() *http.Client {
	return c.httpClient
}

// UpdateRelay updates the relay domain and public key in a thread-safe manner.
// The caller is responsible for calling Connect() afterwards to establish a new connection.
func (c *Client) UpdateRelay(relayDomain string, relayPubKeyBase64 string) error {
	pubKey, err := lecrypto.ImportPublicKeyBase64(relayPubKeyBase64)
	if err != nil {
		return fmt.Errorf("tunnel: update relay: %w", err)
	}
	// Re-resolve IP for the new relay domain.
	relayIP := relayDomain
	if ip := net.ParseIP(relayDomain); ip == nil {
		ips, lookupErr := net.LookupIP(relayDomain)
		if lookupErr != nil || len(ips) == 0 {
			return fmt.Errorf("tunnel: update relay: resolve %q: %w", relayDomain, lookupErr)
		}
		relayIP = ips[0].String()
	}
	c.mu.Lock()
	c.relayDomain = relayDomain
	c.relayIP = relayIP
	c.relayPubKey = pubKey
	c.mu.Unlock()
	return nil
}

// RelayDomain returns the current relay domain in a thread-safe manner.
func (c *Client) RelayDomain() string {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.relayDomain
}

// SessionToken returns the current session token (thread-safe).
func (c *Client) SessionToken() string {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.sessionToken
}

// SessionTokenNeedsRefresh returns true if the token is expired or near expiry.
func (c *Client) SessionTokenNeedsRefresh() bool {
	c.mu.RLock()
	token := c.sessionToken
	issued := c.sessionTokenIssued
	ttl := c.sessionTokenTTL
	c.mu.RUnlock()
	if token == "" {
		return true
	}
	expiresAt := issued + ttl
	margin := int64(tokenRefreshMargin.Seconds())
	return time.Now().Unix() >= expiresAt-margin
}

// SessionTokenExpired returns true if the token is fully expired.
func (c *Client) SessionTokenExpired() bool {
	c.mu.RLock()
	token := c.sessionToken
	issued := c.sessionTokenIssued
	ttl := c.sessionTokenTTL
	c.mu.RUnlock()
	if token == "" {
		return true
	}
	return time.Now().Unix() >= issued+ttl
}

// RefreshSessionToken attempts to refresh the session token via verifyRelay.
// Single-flight (mutex), backoff, and circuit breaker.
func (c *Client) RefreshSessionToken(ctx context.Context) error {
	c.refreshMu.Lock()
	defer c.refreshMu.Unlock()

	// Circuit breaker check.
	if c.circuitOpen {
		if time.Since(c.circuitOpenedAt) < tokenCircuitBreakerPause {
			if !c.SessionTokenExpired() {
				return nil // token still valid, skip refresh
			}
			return ErrTokenExpired
		}
		// Reset circuit breaker after pause.
		c.circuitOpen = false
		c.refreshFailures = 0
		c.refreshBackoff = 0
	}

	// Double-check: maybe another goroutine refreshed while we waited.
	if !c.SessionTokenNeedsRefresh() {
		return nil
	}

	if err := c.verifyRelay(ctx); err != nil {
		c.refreshFailures++
		if c.refreshBackoff == 0 {
			c.refreshBackoff = tokenRefreshBackoffInit
		} else {
			c.refreshBackoff *= 2
			if c.refreshBackoff > tokenRefreshBackoffMax {
				c.refreshBackoff = tokenRefreshBackoffMax
			}
		}
		if c.refreshFailures >= tokenCircuitBreakerMax {
			c.circuitOpen = true
			c.circuitOpenedAt = time.Now()
		}
		if !c.SessionTokenExpired() {
			return nil // token still valid, use it
		}
		return fmt.Errorf("tunnel: refresh token: %w", err)
	}

	// Reset on success.
	c.refreshFailures = 0
	c.refreshBackoff = 0
	return nil
}

// EnsureSessionToken refreshes the token if needed before a CONNECT request.
func (c *Client) EnsureSessionToken(ctx context.Context) error {
	if !c.SessionTokenNeedsRefresh() {
		return nil
	}
	return c.RefreshSessionToken(ctx)
}

func (c *Client) verifyRelay(ctx context.Context) error {
	// Read relay public key under lock for thread-safety with UpdateRelay.
	c.mu.RLock()
	pubKey := c.relayPubKey
	c.mu.RUnlock()

	nonce := make([]byte, nonceSize)
	if _, err := rand.Read(nonce); err != nil {
		return fmt.Errorf("tunnel: connect: generate nonce: %w", err)
	}

	reqBody, err := json.Marshal(verifyRequest{
		Nonce: base64.StdEncoding.EncodeToString(nonce),
	})
	if err != nil {
		return fmt.Errorf("tunnel: connect: marshal request: %w", err)
	}

	url := c.relayURL("/verify")
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(reqBody))
	if err != nil {
		return fmt.Errorf("tunnel: connect: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("tunnel: connect: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("tunnel: connect: verify returned status %d", resp.StatusCode)
	}

	var vResp verifyResponse
	if err := json.NewDecoder(resp.Body).Decode(&vResp); err != nil {
		return fmt.Errorf("tunnel: connect: decode verify response: %w", err)
	}

	sig, err := base64.StdEncoding.DecodeString(vResp.Signature)
	if err != nil {
		return fmt.Errorf("tunnel: connect: decode signature: %w", err)
	}

	if !lecrypto.Verify(pubKey, nonce, sig) {
		return ErrVerificationFailed
	}

	// Store session token if provided.
	if vResp.SessionToken != "" {
		c.mu.Lock()
		c.sessionToken = vResp.SessionToken
		c.sessionTokenIssued = time.Now().Unix()
		c.sessionTokenTTL = 14400 // 4h default
		c.mu.Unlock()
	}

	return nil
}
