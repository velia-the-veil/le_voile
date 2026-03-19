// Package httpproxy implements a local HTTP CONNECT proxy that tunnels
// web traffic through the Le Voile relay for IP camouflage.
package httpproxy

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"sync"
)

// TunnelClient is the interface required from tunnel.Client for proxy operations.
type TunnelClient interface {
	SessionToken() string
	SessionTokenNeedsRefresh() bool
	SessionTokenExpired() bool
	EnsureSessionToken(ctx context.Context) error
	RelayDomain() string
	HTTPClient() *http.Client
	TCPHTTPClient() *http.Client // TCP/TLS client for CONNECT streaming
}

// Server is a local HTTP CONNECT proxy that listens on loopback only.
type Server struct {
	listenAddr    string
	tunnelClient  TunnelClient
	volumeTracker *VolumeTracker
	ready         chan struct{}
	wg            sync.WaitGroup // tracks hijacked CONNECT connections for draining
	httpServer    *http.Server
	listener      net.Listener
}

// NewServer creates a local HTTP proxy server.
// volumeTracker may be nil to disable volume-based bypass.
func NewServer(listenAddr string, tunnelClient TunnelClient, volumeTracker *VolumeTracker) *Server {
	return &Server{
		listenAddr:    listenAddr,
		tunnelClient:  tunnelClient,
		volumeTracker: volumeTracker,
		ready:         make(chan struct{}),
	}
}

// Ready returns a channel that is closed when the server is listening.
func (s *Server) Ready() <-chan struct{} {
	return s.ready
}

// WaitGroup returns the WaitGroup tracking active CONNECT connections.
// Used by the service for connection draining during shutdown.
func (s *Server) WaitGroup() *sync.WaitGroup {
	return &s.wg
}

// Start begins listening and serving. Blocks until ctx is cancelled.
func (s *Server) Start(ctx context.Context) error {
	ln, err := net.Listen("tcp", s.listenAddr)
	if err != nil {
		return fmt.Errorf("httpproxy: listen %s: %w (configure a different port in [http_proxy] section)", s.listenAddr, err)
	}
	s.listener = ln

	handler := &connectHandler{
		tunnelClient:  s.tunnelClient,
		wg:            &s.wg,
		volumeTracker: s.volumeTracker,
	}

	s.httpServer = &http.Server{
		Handler: handler,
	}

	// Start volume tracker cleanup if enabled.
	if s.volumeTracker != nil {
		go s.volumeTracker.StartCleanup(ctx)
	}

	// Signal readiness.
	close(s.ready)

	// Serve until context cancelled.
	errCh := make(chan error, 1)
	go func() {
		errCh <- s.httpServer.Serve(ln)
	}()

	select {
	case <-ctx.Done():
		// Graceful shutdown — don't wait for hijacked connections here,
		// the service handles draining via WaitGroup.
		s.httpServer.Close()
		return nil
	case err := <-errCh:
		if err == http.ErrServerClosed {
			return nil
		}
		return err
	}
}

// ListenAddr returns the actual address the server is listening on.
// Useful when port 0 is used for testing.
func (s *Server) ListenAddr() string {
	if s.listener != nil {
		return s.listener.Addr().String()
	}
	return s.listenAddr
}
