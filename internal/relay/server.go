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
// /.well-known/relay-registry.json, /connect, /tunnel) are served directly
// to clients. The relay is reached via DNS A records pointing at the VPS
// origin — there is no CDN fronting, so source-IP validation is done on
// r.RemoteAddr, and authenticated endpoints (/connect, /tunnel) bind their
// session tokens to SHA256(client-remote-addr) for IP-hash verification.
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
	BWLimiter      *BandwidthLimiter
	StartTime      time.Time
	NATStatsFunc   NATStatsProvider // optional: provides NAT stats for /health
	RegistryFile   string           // path to relay-registry.json (served at /.well-known/relay-registry.json)
	h3             *http3.Server
}

// NewServer creates a relay server configured for TLS 1.3 with Ed25519 certificates.
func NewServer(addr, certFile, keyFile string) *Server {
	return &Server{
		Addr:          addr,
		CertFile:      certFile,
		KeyFile:       keyFile,
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

	if s.Handler != nil {
		mux.Handle("/dns-query", LimitMiddleware(s.Limiter, s.Handler))
	}
	healthHandler := NewHealthHandler(s.Limiter, s.TunnelLimiter, s.StartTime)
	if s.NATStatsFunc != nil {
		healthHandler.SetNATStatsProvider(s.NATStatsFunc)
	}
	mux.Handle("/health", LimitMiddleware(s.Limiter, healthHandler))

	// Create verify handler for session token issuance.
	if s.SigningKey != nil {
		vh := NewVerifyHandler(s.SigningKey)
		// Fix H10 (audit sécurité) : ajouter un per-IP limiter en amont du
		// Limiter global. Sans ça, un client unique peut spammer /verify
		// jusqu'à saturer la capacité globale et dégrader le service pour
		// tous les autres. L'Ed25519 sign côté relay est rapide mais non
		// gratuit ; le plafond per-IP évite qu'un client monopolise le CPU.
		var verifyHandler http.Handler = vh
		if s.IPLimiter != nil {
			verifyHandler = IPLimitMiddleware(s.IPLimiter, verifyHandler)
		}
		mux.Handle("/verify", LimitMiddleware(s.Limiter, verifyHandler))
	}
	if s.ConnectHandler != nil {
		// /connect uses its own per-IP limiter (IPLimiter), not the global
		// connection limiter. CONNECT tunnels are long-lived streams that
		// would exhaust the global short-request limiter. Handler enforces
		// its own bearer-token + per-IP validation.
		mux.Handle("/connect", s.ConnectHandler)
	}
	if s.TunnelHandler != nil {
		// /tunnel uses the dedicated TunnelLimiter (HTTP 503 when saturated)
		// — separate from the global request Limiter. Handler enforces
		// bearer-token + IP-hash auth directly.
		mux.Handle("/tunnel", LimitMiddleware(s.TunnelLimiter, s.TunnelHandler))
	}

	// /ip returns the client's visible IP — used by desktop to display exit IP.
	mux.Handle("/ip", LimitMiddleware(s.Limiter, NewIPHandler()))

	// Serve relay registry JSON if configured.
	if s.RegistryFile != "" {
		mux.Handle("/.well-known/relay-registry.json", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			http.ServeFile(w, r, s.RegistryFile)
		}))
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

	// Start background goroutines for IP limiter cleanup and bandwidth limiter cleanup.
	if s.IPLimiter != nil {
		go s.IPLimiter.StartCleanup(ctx)
	}
	if s.BWLimiter != nil {
		go s.BWLimiter.StartCleanup(ctx)
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
