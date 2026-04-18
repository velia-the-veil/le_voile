package relay

import (
	"context"
	"crypto/ed25519"
	"crypto/tls"
	"net/http"
	"time"

	"github.com/quic-go/quic-go"
	"github.com/quic-go/quic-go/http3"
)

// Server wraps an HTTP/3 server for the stateless relay.
//
// Public endpoints (/health, /verify, /ip, /dns-query,
// /.well-known/relay-registry.json) are protected by CFSourceMiddleware
// when CFIPValidator is set in strict (non-insecure) mode: requests from
// IPs outside Cloudflare's published ranges receive HTTP 403.
// /connect and /tunnel are documented exceptions — they carry their own
// bearer-token + per-IP validation via IPLimiter.
//
// Story 6.1: /stun-relay removed — post-Epic-2 L3 capture routes STUN
// Binding Requests through the tunnel pump natively.
type Server struct {
	Addr           string
	CertFile       string
	KeyFile        string
	Handler        http.Handler
	ConnectHandler http.Handler
	TunnelHandler  http.Handler
	SigningKey     ed25519.PrivateKey
	Limiter        *Limiter
	TunnelLimiter  *Limiter
	IPLimiter      *IPLimiter
	CFIPValidator  *CloudflareIPValidator
	BWLimiter      *BandwidthLimiter
	StartTime      time.Time
	NATStatsFunc   NATStatsProvider // optional: provides NAT stats for /health
	RegistryFile   string           // path to relay-registry.json (served at /.well-known/relay-registry.json)
	CFRejectLog    func(string)     // invoked with "cf-reject" on each refused request; never receives IPs (NFR20)
	h3             *http3.Server
}

// NewServer creates a relay server configured for TLS 1.3 with Ed25519 certificates.
func NewServer(addr, certFile, keyFile string) *Server {
	return &Server{
		Addr:      addr,
		CertFile:  certFile,
		KeyFile:   keyFile,
		Limiter:       NewLimiter(MaxConnections),
		TunnelLimiter: NewLimiter(MaxTunnels),
		StartTime:     time.Now(),
	}
}

// ListenAndServe starts the HTTP/3 server. It blocks until the context is
// cancelled or an unrecoverable error occurs.
func (s *Server) ListenAndServe(ctx context.Context) error {
	cert, err := tls.LoadX509KeyPair(s.CertFile, s.KeyFile)
	if err != nil {
		return &ServerError{Op: "load-tls", Err: err}
	}

	tlsCfg := http3.ConfigureTLSConfig(&tls.Config{
		Certificates: []tls.Certificate{cert},
		MinVersion:   tls.VersionTLS13,
	})

	mux := http.NewServeMux()

	// cfWrap applies CF source filtering BEFORE the connection limiter so
	// non-CF requests don't consume a slot. nil-safe and insecure-aware.
	cfWrap := func(h http.Handler) http.Handler {
		return CFSourceMiddleware(s.CFIPValidator, s.CFRejectLog, h)
	}

	if s.Handler != nil {
		mux.Handle("/dns-query", cfWrap(LimitMiddleware(s.Limiter, s.Handler)))
	}
	healthHandler := NewHealthHandler(s.Limiter, s.TunnelLimiter, s.StartTime)
	if s.NATStatsFunc != nil {
		healthHandler.SetNATStatsProvider(s.NATStatsFunc)
	}
	mux.Handle("/health", cfWrap(LimitMiddleware(s.Limiter, healthHandler)))

	// Create verify handler and wire CF validator for session token issuance.
	if s.SigningKey != nil {
		vh := NewVerifyHandler(s.SigningKey)
		if s.CFIPValidator != nil {
			vh.SetCFValidator(s.CFIPValidator)
		}
		mux.Handle("/verify", cfWrap(LimitMiddleware(s.Limiter, vh)))
	}
	if s.ConnectHandler != nil {
		// /connect uses its own per-IP limiter (IPLimiter), not the global
		// connection limiter. CONNECT tunnels are long-lived streams that
		// would exhaust the global short-request limiter.
		// Documented exception to CF source filtering — handler enforces
		// its own bearer-token + per-IP validation.
		mux.Handle("/connect", s.ConnectHandler)
	}
	if s.TunnelHandler != nil {
		// /tunnel uses the dedicated TunnelLimiter (HTTP 503 when saturated)
		// — separate from the global request Limiter.
		// CF source filtering is not applied — handler enforces bearer-token
		// + IP-hash auth directly.
		mux.Handle("/tunnel", LimitMiddleware(s.TunnelLimiter, s.TunnelHandler))
	}

	// /ip returns the client's visible IP — used by desktop to display exit IP.
	mux.Handle("/ip", cfWrap(LimitMiddleware(s.Limiter, NewIPHandler(s.CFIPValidator))))

	// Serve relay registry JSON if configured.
	if s.RegistryFile != "" {
		mux.Handle("/.well-known/relay-registry.json", cfWrap(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			http.ServeFile(w, r, s.RegistryFile)
		})))
	}

	s.h3 = &http3.Server{
		Addr:      s.Addr,
		Handler:   mux,
		TLSConfig: tlsCfg,
		QUICConfig: &quic.Config{
			MaxIncomingStreams: 1000,             // default 100 — too low for browser proxy
			MaxIdleTimeout:     90 * time.Second, // match client
			KeepAlivePeriod:    10 * time.Second, // survive aggressive NAT timeouts
		},
	}

	// Start background goroutines for IP limiter cleanup, bandwidth limiter cleanup, and CF IP refresh.
	if s.IPLimiter != nil {
		go s.IPLimiter.StartCleanup(ctx)
	}
	if s.BWLimiter != nil {
		go s.BWLimiter.StartCleanup(ctx)
	}
	if s.CFIPValidator != nil {
		go s.CFIPValidator.StartRefresh(ctx)
	}

	errCh := make(chan error, 2)
	go func() {
		errCh <- s.h3.ListenAndServe()
	}()

	// TCP HTTPS listener — serves the same mux over HTTP/1.1 and HTTP/2.
	// Required for: registry client (HTTP GET), latency checker (/health),
	// and /ip endpoint used by the desktop client for visible IP detection.
	tcpServer := &http.Server{
		Addr:      s.Addr,
		Handler:   mux,
		TLSConfig: tlsCfg.Clone(),
	}
	go func() {
		if err := tcpServer.ListenAndServeTLS(s.CertFile, s.KeyFile); err != nil && err != http.ErrServerClosed {
			errCh <- &ServerError{Op: "tcp-serve", Err: err}
		}
	}()

	select {
	case <-ctx.Done():
		tcpServer.Shutdown(context.Background())
		return s.Shutdown(context.Background())
	case err := <-errCh:
		tcpServer.Shutdown(context.Background())
		if err != nil {
			return &ServerError{Op: "serve", Err: err}
		}
		return nil
	}
}

// Shutdown gracefully shuts down the HTTP/3 server.
func (s *Server) Shutdown(ctx context.Context) error {
	if s.h3 == nil {
		return nil
	}
	if err := s.h3.Close(); err != nil {
		return &ServerError{Op: "shutdown", Err: err}
	}
	return nil
}

// ServerError represents a relay server error with operation context.
type ServerError struct {
	Op  string
	Err error
}

func (e *ServerError) Error() string {
	return "relay: " + e.Op + ": " + e.Err.Error()
}

func (e *ServerError) Unwrap() error {
	return e.Err
}
