package httpproxy

import (
	"context"
	"net"
	"net/http"
	"strings"
	"testing"
	"time"
)

// mockTunnelClient implements TunnelClient for testing.
type mockTunnelClient struct {
	sessionToken             string
	sessionTokenNeedsRefresh bool
	sessionTokenExpired      bool
	ensureSessionTokenErr    error
	relayDomain              string
	httpClient               *http.Client

	ensureSessionTokenCalled bool
}

func (m *mockTunnelClient) SessionToken() string               { return m.sessionToken }
func (m *mockTunnelClient) SessionTokenNeedsRefresh() bool     { return m.sessionTokenNeedsRefresh }
func (m *mockTunnelClient) SessionTokenExpired() bool          { return m.sessionTokenExpired }
func (m *mockTunnelClient) EnsureSessionToken(_ context.Context) error {
	m.ensureSessionTokenCalled = true
	return m.ensureSessionTokenErr
}
func (m *mockTunnelClient) RelayDomain() string   { return m.relayDomain }
func (m *mockTunnelClient) HTTPClient() *http.Client { return m.httpClient }

func TestStartReadyLifecycle(t *testing.T) {
	mock := &mockTunnelClient{}
	srv := NewServer("127.0.0.1:0", mock, nil)

	ctx, cancel := context.WithCancel(context.Background())
	errCh := make(chan error, 1)
	go func() {
		errCh <- srv.Start(ctx)
	}()

	select {
	case <-srv.Ready():
		// Server is listening — good.
	case <-time.After(3 * time.Second):
		t.Fatal("timed out waiting for server to become ready")
	}

	cancel()

	select {
	case err := <-errCh:
		if err != nil {
			t.Fatalf("expected nil error after context cancel, got: %v", err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("timed out waiting for Start to return")
	}
}

func TestListenAddrReturnsActualAddress(t *testing.T) {
	mock := &mockTunnelClient{}
	srv := NewServer("127.0.0.1:0", mock, nil)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	go srv.Start(ctx)

	select {
	case <-srv.Ready():
	case <-time.After(3 * time.Second):
		t.Fatal("timed out waiting for server to become ready")
	}

	addr := srv.ListenAddr()
	if addr == "" || addr == "127.0.0.1:0" {
		t.Fatalf("expected a resolved listen address, got %q", addr)
	}

	// Verify the address is parseable.
	host, port, err := net.SplitHostPort(addr)
	if err != nil {
		t.Fatalf("ListenAddr returned unparseable address %q: %v", addr, err)
	}
	if host != "127.0.0.1" {
		t.Fatalf("expected host 127.0.0.1, got %q", host)
	}
	if port == "0" {
		t.Fatalf("expected a non-zero port, got %q", port)
	}
}

func TestPortAlreadyInUseReturnsError(t *testing.T) {
	// Bind a port first.
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("failed to bind port: %v", err)
	}
	defer ln.Close()

	occupiedAddr := ln.Addr().String()

	mock := &mockTunnelClient{}
	srv := NewServer(occupiedAddr, mock, nil)

	err = srv.Start(context.Background())
	if err == nil {
		t.Fatal("expected error when port is already in use, got nil")
	}
	if !strings.Contains(err.Error(), "configure a different port") {
		t.Fatalf("expected error to contain %q, got: %v", "configure a different port", err)
	}
}

func TestWaitGroupIsUsable(t *testing.T) {
	mock := &mockTunnelClient{}
	srv := NewServer("127.0.0.1:0", mock, nil)

	wg := srv.WaitGroup()
	if wg == nil {
		t.Fatal("WaitGroup returned nil")
	}

	// Verify it functions: Add/Done should not panic.
	wg.Add(1)
	wg.Done()
	wg.Wait()
}
