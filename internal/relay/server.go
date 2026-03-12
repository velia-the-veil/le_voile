package relay

import (
	"context"
	"crypto/ed25519"
	"crypto/tls"
	"net/http"
	"time"

	"github.com/quic-go/quic-go/http3"
)

// Server wraps an HTTP/3 server for the stateless relay.
type Server struct {
	Addr        string
	CertFile    string
	KeyFile     string
	Handler     http.Handler
	STUNHandler http.Handler
	SigningKey   ed25519.PrivateKey
	Limiter     *Limiter
	StartTime   time.Time
	h3          *http3.Server
}

// NewServer creates a relay server configured for TLS 1.3 with Ed25519 certificates.
func NewServer(addr, certFile, keyFile string) *Server {
	return &Server{
		Addr:      addr,
		CertFile:  certFile,
		KeyFile:   keyFile,
		Limiter:   NewLimiter(MaxConnections),
		StartTime: time.Now(),
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
	mux.Handle("/health", LimitMiddleware(s.Limiter, NewHealthHandler(s.Limiter, s.StartTime)))
	if s.SigningKey != nil {
		mux.Handle("/verify", NewVerifyHandler(s.SigningKey))
	}
	if s.STUNHandler != nil {
		mux.Handle("/stun-relay", LimitMiddleware(s.Limiter, s.STUNHandler))
	}

	s.h3 = &http3.Server{
		Addr:      s.Addr,
		Handler:   mux,
		TLSConfig: tlsCfg,
	}

	errCh := make(chan error, 1)
	go func() {
		errCh <- s.h3.ListenAndServe()
	}()

	select {
	case <-ctx.Done():
		return s.Shutdown(context.Background())
	case err := <-errCh:
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
