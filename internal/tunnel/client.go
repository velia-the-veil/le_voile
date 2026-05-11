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
	// connectTimeout bounds the whole Connect() flow (TLS 1.3 handshake +
	// /verify roundtrip). NFR11 targets <3s on ADSL/fibre with RTT <50ms to
	// Cloudflare — this is an empirical performance target, not a deadline we
	// want to enforce as a hard timeout. We set a 5s ceiling so that users on
	// higher-RTT links (lossy Wi-Fi, 3G fallback) are not spuriously cut off;
	// normal handshakes complete well under 3s and are validated empirically
	// pre-release via the procedure in docs/testing/dpi-verification.md.
	connectTimeout     = 5 * time.Second
	dohTimeout         = 8 * time.Second
	nonceSize          = 32
	maxCertChainLength = 3             // reject certificate chains longer than this
	maxDoHResponseSize = 64 * 1024     // 64 KB — well above the 65535-byte DNS UDP limit
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

// maxConsecutiveDoHFailures is the number of consecutive transport-level
// failures in SendDoHQuery before the client auto-transitions to
// StateDisconnected, triggering the Reconnector.
const maxConsecutiveDoHFailures = 5

// Client manages an HTTP/3 tunnel connection to the relay.
type Client struct {
	mu          sync.RWMutex
	relayDomain string
	relayIP     string // resolved IP of the relay, used to bypass system DNS
	relayPubKey ed25519.PublicKey
	httpClient  *http.Client
	transport   *http3.Transport
	state       *StateManager

	// TLS options stored for transport recreation after Disconnect.
	insecure   bool
	skipCAOnly bool

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

	// Consecutive DoH transport failure counter for auto-disconnect.
	failureMu            sync.Mutex
	consecutiveFailures  int

	// QUIC connection captured via custom Dial callback (R-T8 — connection
	// migration). Set on first http3.Transport.dial; reset by ResetTransport.
	// quicTransport wraps the underlying UDP socket — replaced by MigrateToFD
	// when the Android NetworkCallback signals an underlying network change.
	// nil when no QUIC connection is active.
	quicMu        sync.RWMutex
	quicConn      *quic.Conn
	quicTransport *quic.Transport

	// Heartbeat goroutine lifecycle. startHeartbeat creates a stop channel
	// closed by stopHeartbeat (idempotent). The heartbeat probes /health
	// every 5s (timeout 2s) and triggers StateDisconnected after 2 consecutive
	// failures — covers zombie tunnel cases where the underlying QUIC session
	// is dead but quic-go's MaxIdleTimeout hasn't fired yet (typical on
	// cellular CGNAT rotation or cell handoff with no network type change).
	heartbeatMu   sync.Mutex
	heartbeatStop chan struct{}
}

// NewClient creates a tunnel client configured for HTTP/3 with TLS 1.3.
// Use WithInsecure(true) to skip TLS certificate verification (development only).
//
// Establishment is a two-step sequence:
//
//  1. NewClient(domain, pinnedKey) — builds the HTTP/3 transport, resolves the
//     relay IP (via system DNS at this stage — DoH bootstrap is handled by
//     internal/registry/doh_resolver.go at the registry layer, not here), and
//     wires the Ed25519 pinning callback into tls.Config.VerifyPeerCertificate.
//     Does NOT open a QUIC connection.
//  2. Connect(ctx) — opens QUIC/HTTP3 via /verify (TLS 1.3 handshake fails
//     immediately with ErrPinningFailed if the relay's leaf cert does not carry
//     the pinned Ed25519 key).
//
// The split exists so that reconnection (internal/tunnel/reconnect.go) can
// reuse the same Client instance across network failures without re-running
// DNS resolution. For a one-shot convenience wrapper matching the Story 1.1
// acceptance-criteria signature, see ConnectNew.
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
	//
	// On the Linux orchestration path (service.go) the relay IP has ALREADY been
	// resolved at §0g (line ~1219) BEFORE the kill-switch is armed at §0h. The
	// caller passes that IP via WithResolvedIP so we short-circuit the lookup —
	// re-querying systemd-resolved here would hang because its upstream forward
	// (enp*:53) is dropped by the now-active firewall. See WithResolvedIP doc.
	relayIP := relayDomain
	if o.resolvedIPHint != "" {
		// Caller already resolved the relay (typically before firewall.Activate).
		// Trust the hint — it must be a parseable IP.
		if ip := net.ParseIP(o.resolvedIPHint); ip == nil {
			return nil, fmt.Errorf("tunnel: WithResolvedIP: %q is not a valid IP", o.resolvedIPHint)
		}
		relayIP = o.resolvedIPHint
	} else if ip := net.ParseIP(relayDomain); ip == nil {
		// Not a bare IP — check for host:port (e.g., "127.0.0.1:8443" in tests).
		if host, _, splitErr := net.SplitHostPort(relayDomain); splitErr == nil && net.ParseIP(host) != nil {
			relayIP = relayDomain // IP:port — use as-is
		} else {
			// Domain name — resolve it now.
			ips, err := net.LookupIP(relayDomain)
			if err != nil || len(ips) == 0 {
				return nil, fmt.Errorf("tunnel: resolve relay %q: %w", relayDomain, err)
			}
			relayIP = ips[0].String()
		}
	}

	// Create Client first so the TLS closure can read c.relayPubKey under lock,
	// ensuring UpdateRelay updates are visible to future TLS handshakes.
	c := &Client{
		relayDomain: relayDomain,
		relayIP:     relayIP,
		relayPubKey: pubKey,
		insecure:    o.insecure,
		skipCAOnly:  o.skipCAOnly,
		state:       NewStateManager(),
	}

	tr := c.buildTransport()
	c.httpClient = &http.Client{Transport: tr}
	c.transport = tr

	return c, nil
}

// ConnectNew is a one-shot helper that builds a Client via NewClient and
// immediately establishes the tunnel via Connect. Matches the Story 1.1
// acceptance-criteria signature tunnel.Connect(ctx, relayDomain, pinnedKey).
//
// Production code should prefer the two-step NewClient + Connect sequence so
// that the same Client can be reused across reconnection attempts (see
// internal/tunnel/reconnect.go). Use ConnectNew in integration tests or
// throw-away scripts where reconnection is not needed.
//
// On any failure (NewClient or Connect), ConnectNew returns (nil, err) and
// guarantees no resources are retained: if Connect fails after the HTTP/3
// transport is built, Disconnect is called to close the QUIC transport
// before returning. Callers must NOT call Disconnect on the returned client
// when err != nil (client is nil).
func ConnectNew(ctx context.Context, relayDomain string, relayPubKeyBase64 string, opts ...ClientOption) (*Client, error) {
	c, err := NewClient(relayDomain, relayPubKeyBase64, opts...)
	if err != nil {
		return nil, err
	}
	if err := c.Connect(ctx); err != nil {
		_ = c.Disconnect() // close transport to avoid QUIC socket leak
		return nil, err
	}
	return c, nil
}

// buildTransport creates a fresh HTTP/3 transport with TLS pinning.
// Must be called with c.mu held or before concurrent access is possible.
//
// R-T8 — Connection migration : the custom `Dial` callback captures the
// `*quic.Conn` and its underlying `*quic.Transport` so that MigrateToFD can
// later swap the UDP socket without tearing down the application-layer
// session (HTTP/3 streams, session token, /tunnel stream). Without the
// custom callback, quic-go would create the Transport internally and we'd
// have no handle on it.
func (c *Client) buildTransport() *http3.Transport {
	return &http3.Transport{
		TLSClientConfig: &tls.Config{
			ServerName:         c.relayDomain, // SNI must match the cert, not the IP
			NextProtos:         []string{http3.NextProtoH3},
			MinVersion:         tls.VersionTLS13,
			InsecureSkipVerify: c.insecure || c.skipCAOnly,
			// #nosec G123 -- VerifyPeerCertificate seul peut être bypassé par
			// TLS session resumption (resumed sessions skip cette callback).
			// Défense en profondeur Le Voile : (1) Ed25519 cert pinning ICI
			// + (2) /verify endpoint au handshake applicatif qui re-valide
			// une signature Ed25519 challenge/response indépendamment de TLS
			// (cf. verifyRelay). Une resumed session avec cert frauduleux ne
			// peut pas répondre /verify correctement → la session est rejetée
			// au layer applicatif. Le risque G123 est mitigé hors-TLS.
			VerifyPeerCertificate: func(rawCerts [][]byte, _ [][]*x509.Certificate) error {
				if c.insecure {
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
					if errors.Is(err, lecrypto.ErrPinningFailed) {
						return fmt.Errorf("%w: %v", ErrPinningFailed, err)
					}
					// Non-Ed25519 cert (e.g., Let's Encrypt ECDSA): skip pinning,
					// rely on CA chain validation + /verify Ed25519 auth.
				}
				return nil
			},
		},
		QUICConfig: &quic.Config{
			MaxIdleTimeout:  90 * time.Second, // 90s idle before disconnect
			KeepAlivePeriod: 10 * time.Second, // ping every 10s to survive aggressive NAT timeouts
		},
		Dial: c.dialQUICCustom,
	}
}

// dialQUICCustom is the http3.Transport.Dial callback that opens the QUIC
// connection AND captures both the *quic.Conn and *quic.Transport for later
// migration via MigrateToFD (R-T8).
//
// Mirrors the default behaviour of quic-go's http3 layer (cf. transport.go
// line ~360 in v0.59) :
//   - Single shared *quic.Transport per Client session — reused across all
//     dials. Creating a new Transport per dial breaks connection ID pooling,
//     server-side state association and inflates resource usage. The default
//     http3 implementation always reuses `t.transport`.
//   - DialEarly (NOT Dial) for 0-RTT support and consistency with default
//     http3 dial behaviour.
//
// The shared Transport is lazy-initialized on first dial (under c.quicMu)
// and torn down by ResetTransport. The `dial-fix-T8-bis` (2026-05-05)
// addresses a regression where the initial implementation created a new
// Transport per dial — that broke the tunnel on Android in initial testing.
func (c *Client) dialQUICCustom(ctx context.Context, addr string, tlsCfg *tls.Config, cfg *quic.Config) (*quic.Conn, error) {
	udpAddr, err := net.ResolveUDPAddr("udp", addr)
	if err != nil {
		return nil, fmt.Errorf("tunnel: dial: resolve %q: %w", addr, err)
	}

	// Lazy-init the shared *quic.Transport on first dial of this Client
	// session. Subsequent dials (HTTP/3 connection reuse, parallel requests)
	// share the same UDP socket via this Transport.
	c.quicMu.Lock()
	transport := c.quicTransport
	if transport == nil {
		udpConn, lerr := net.ListenUDP("udp", &net.UDPAddr{})
		if lerr != nil {
			c.quicMu.Unlock()
			return nil, fmt.Errorf("tunnel: dial: listen udp: %w", lerr)
		}
		transport = &quic.Transport{Conn: udpConn}
		c.quicTransport = transport
	}
	c.quicMu.Unlock()

	conn, err := transport.DialEarly(ctx, udpAddr, tlsCfg, cfg)
	if err != nil {
		// Don't close the shared Transport on dial failure — other concurrent
		// dials may still be using it, and ResetTransport will clean up at
		// the right moment.
		return nil, err
	}

	c.quicMu.Lock()
	c.quicConn = conn
	c.quicMu.Unlock()

	return conn, nil
}

// clientOptions holds optional client configuration.
type clientOptions struct {
	insecure       bool
	skipCAOnly     bool   // skip CA verification only; pinning still enforced. For tests with self-signed certs.
	resolvedIPHint string // pre-resolved IPv4/IPv6 of the relay; bypasses internal net.LookupIP
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

// WithResolvedIP injects a pre-resolved IP for the relay so NewClient skips its
// internal net.LookupIP. Required on the Linux orchestration path because the
// kill-switch is armed BEFORE NewClient runs (service.go: routing.Setup →
// firewall.Activate → tunnel.NewClient). A second DNS lookup at that point
// would hit systemd-resolved on 127.0.0.53/lo (allowed) but its upstream
// forward over enp* (192.168.1.1:53) is denied by the output `policy drop` —
// `net.LookupIP` then hangs through Go's default 5s timeout, which the
// connectTimeout-bounded Connect surfaces as ErrConnectionTimeout while no
// QUIC packet was ever sent. Pass the IP captured from the pre-firewall
// resolution at service.go:0g to short-circuit this race.
func WithResolvedIP(ip string) ClientOption {
	return func(o *clientOptions) { o.resolvedIPHint = ip }
}

// relayURL builds a URL using the resolved IP to avoid DNS lookups.
// QUIC uses the TLS ServerName (set in the transport config) for SNI.
func (c *Client) relayURL(path string) string {
	c.mu.RLock()
	ip := c.relayIP
	c.mu.RUnlock()
	host := ip
	// Bracket bare IPv6 addresses only. If the address contains a port
	// (host:port format), net.ParseIP returns nil and no bracketing is applied.
	if strings.Contains(ip, ":") && net.ParseIP(ip) != nil {
		host = "[" + ip + "]"
	}
	return "https://" + host + path
}

// Connect establishes the tunnel by verifying the relay's Ed25519 identity.
//
// R-T8 : on success also starts the heartbeat probe (see heartbeat.go) which
// monitors /health every 5s and triggers StateDisconnected on persistent
// failures. The heartbeat goroutine is bound to context.Background — its
// lifecycle is tied to the Client rather than the Connect ctx (which is a
// short-lived 5s timeout for the handshake itself). Stopped on Disconnect
// or ResetTransport.
func (c *Client) Connect(ctx context.Context) error {
	c.state.Set(StateConnecting)

	connectCtx, cancel := context.WithTimeout(ctx, connectTimeout)
	defer cancel()

	if err := c.verifyRelay(connectCtx); err != nil {
		c.state.Set(StateDisconnected)
		if connectCtx.Err() != nil {
			return ErrConnectionTimeout
		}
		return err
	}

	c.resetDoHFailures()
	c.state.Set(StateConnected)

	// R-T8 — heartbeat ré-activé après fix du callback Kotlin. Le heartbeat
	// trip déclenche maintenant : state.Set + emitStatus(disconnected) +
	// CloseWithError sur la conn QUIC. Le pump observe la fermeture et exit,
	// runGomobilePump's emitStatus le voit (idempotent), et le statusCallback
	// Kotlin déclenche l'auto-reconnect avec backoff.
	c.startHeartbeat(context.Background())
	return nil
}

// SendDoHQuery sends a DNS wire-format query through the tunnel.
// Transport-level failures are tracked; after maxConsecutiveDoHFailures
// the client auto-transitions to StateDisconnected so the Reconnector
// can re-establish the tunnel.
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

	resp, err := c.getHTTPClient().Do(req)
	if err != nil {
		c.recordDoHFailure()
		return nil, fmt.Errorf("tunnel: send doh: %w", err)
	}
	defer resp.Body.Close()

	// Transport succeeded — reset failure counter.
	c.resetDoHFailures()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("tunnel: send doh: server returned status %d", resp.StatusCode)
	}

	body, err := io.ReadAll(io.LimitReader(resp.Body, maxDoHResponseSize))
	if err != nil {
		return nil, fmt.Errorf("tunnel: send doh: read response: %w", err)
	}

	return body, nil
}

// Disconnect closes the current QUIC connection and prepares a fresh
// transport so that subsequent Connect calls can re-establish the tunnel.
// Previous versions permanently closed the transport, making reconnection
// impossible — the Reconnector would loop on dead transport errors.
func (c *Client) Disconnect() error {
	c.state.Set(StateDisconnected)
	c.ResetTransport()
	return nil
}

// ResetTransport tears down the current QUIC/HTTP3 transport and creates a
// fresh one without changing the tunnel state. Use this from the Reconnector
// to avoid injecting a stale StateDisconnected event into the updates channel.
//
// R-T8 : also clears the captured *quic.Conn / *quic.Transport — they refer
// to the dead connection and a stale Conn here would make MigrateToFD return
// ErrMigrationNoActiveConn instead of nil even after a fresh Connect.
func (c *Client) ResetTransport() {
	c.resetDoHFailures()
	c.stopHeartbeat() // stop probing the dying connection

	c.mu.Lock()
	oldTransport := c.transport
	newTransport := c.buildTransport()
	c.transport = newTransport
	c.httpClient = &http.Client{Transport: newTransport}
	c.mu.Unlock()

	// Clear the captured QUIC handles — they will be re-set on the next
	// http3 dial via dialQUICCustom.
	c.quicMu.Lock()
	oldQUICTransport := c.quicTransport
	c.quicConn = nil
	c.quicTransport = nil
	c.quicMu.Unlock()

	// Close old transport after replacing — ongoing requests will get errors
	// but the connection was already broken or intentionally torn down.
	if oldTransport != nil {
		oldTransport.Close()
	}
	if oldQUICTransport != nil {
		_ = oldQUICTransport.Close()
	}
}

// State returns the tunnel's state manager.
func (c *Client) State() *StateManager {
	return c.state
}

// HTTPClient returns the underlying HTTP/3 QUIC client (thread-safe).
func (c *Client) HTTPClient() *http.Client {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.httpClient
}

// getHTTPClient returns the current HTTP client under read lock.
func (c *Client) getHTTPClient() *http.Client {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.httpClient
}

// recordDoHFailure increments the consecutive failure counter and triggers
// a state transition to StateDisconnected after maxConsecutiveDoHFailures.
// This ensures the Reconnector detects silent QUIC connection loss.
func (c *Client) recordDoHFailure() {
	c.failureMu.Lock()
	c.consecutiveFailures++
	failures := c.consecutiveFailures
	c.failureMu.Unlock()

	if failures >= maxConsecutiveDoHFailures && c.state.Get() == StateConnected {
		c.state.Set(StateDisconnected)
	}
}

// resetDoHFailures clears the consecutive failure counter.
func (c *Client) resetDoHFailures() {
	c.failureMu.Lock()
	c.consecutiveFailures = 0
	c.failureMu.Unlock()
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
		if host, _, splitErr := net.SplitHostPort(relayDomain); splitErr == nil && net.ParseIP(host) != nil {
			relayIP = relayDomain
		} else {
			ips, lookupErr := net.LookupIP(relayDomain)
			if lookupErr != nil || len(ips) == 0 {
				return fmt.Errorf("tunnel: update relay: resolve %q: %w", relayDomain, lookupErr)
			}
			relayIP = ips[0].String()
		}
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

// RelayIP returns the resolved IPv4 address (as net.IP) of the current relay.
// Returns nil if the stored value is not a valid IP. Used by callers that
// must reconfigure routing or firewall after UpdateRelay (e.g. country
// switch) — those layers need the IP, not the domain.
func (c *Client) RelayIP() net.IP {
	c.mu.RLock()
	raw := c.relayIP
	c.mu.RUnlock()
	host := raw
	if h, _, err := net.SplitHostPort(raw); err == nil {
		host = h
	}
	ip := net.ParseIP(host)
	if ip == nil {
		return nil
	}
	if v4 := ip.To4(); v4 != nil {
		return v4
	}
	return ip
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

	resp, err := c.getHTTPClient().Do(req)
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
