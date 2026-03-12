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
	"net/http"
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
	Signature string `json:"signature"`
}

// Client manages an HTTP/3 tunnel connection to the relay.
type Client struct {
	mu          sync.RWMutex
	relayDomain string
	relayPubKey ed25519.PublicKey
	httpClient  *http.Client
	transport   *http3.Transport
	state       *StateManager
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

	// Create Client first so the TLS closure can read c.relayPubKey under lock,
	// ensuring UpdateRelay updates are visible to future TLS handshakes.
	c := &Client{
		relayDomain: relayDomain,
		relayPubKey: pubKey,
		state:       NewStateManager(),
	}

	tr := &http3.Transport{
		TLSClientConfig: &tls.Config{
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
		QUICConfig: &quic.Config{},
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

	c.mu.RLock()
	domain := c.relayDomain
	c.mu.RUnlock()

	url := "https://" + domain + "/dns-query"
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

	c.mu.RLock()
	domain := c.relayDomain
	c.mu.RUnlock()

	url := "https://" + domain + "/stun-relay"
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
	c.mu.Lock()
	c.relayDomain = relayDomain
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

func (c *Client) verifyRelay(ctx context.Context) error {
	// Read relay coordinates under lock for thread-safety with UpdateRelay.
	c.mu.RLock()
	domain := c.relayDomain
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

	url := "https://" + domain + "/verify"
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

	return nil
}
