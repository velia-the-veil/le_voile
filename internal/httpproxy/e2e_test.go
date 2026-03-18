//go:build e2e

package httpproxy

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// e2eTunnelClient implements TunnelClient for E2E integration tests.
type e2eTunnelClient struct {
	mu           sync.Mutex
	relayDomain  string
	httpClient   *http.Client
	token        string
	refreshToken string // token after refresh; empty = keep current
	expired      bool
	refreshCount atomic.Int64
}

func (c *e2eTunnelClient) SessionToken() string {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.token
}

func (c *e2eTunnelClient) SessionTokenNeedsRefresh() bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.expired
}

func (c *e2eTunnelClient) SessionTokenExpired() bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.expired
}

func (c *e2eTunnelClient) EnsureSessionToken(_ context.Context) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.refreshCount.Add(1)
	c.expired = false
	if c.refreshToken != "" {
		c.token = c.refreshToken
	}
	return nil
}

func (c *e2eTunnelClient) RelayDomain() string     { return c.relayDomain }
func (c *e2eTunnelClient) HTTPClient() *http.Client { return c.httpClient }

// relayTransport is a custom RoundTripper that simulates the relay's /connect
// endpoint at the transport level. This avoids HTTP/1.1 streaming limitations
// that prevent bidirectional relay in a standard HTTP handler.
//
// It reads the JSON target from the request body, dials the target, then
// returns a response whose Body is the target connection (downstream) while
// forwarding the remaining request body to the target (upstream) in a goroutine.
type relayTransport struct {
	authCheck func(string) bool
}

func (rt *relayTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	if rt.authCheck != nil {
		if !rt.authCheck(req.Header.Get("Authorization")) {
			return &http.Response{
				StatusCode: http.StatusUnauthorized,
				Body:       io.NopCloser(strings.NewReader("Unauthorized")),
				Header:     make(http.Header),
			}, nil
		}
	}

	if req.URL.Path != "/connect" {
		return &http.Response{
			StatusCode: http.StatusNotFound,
			Body:       io.NopCloser(strings.NewReader("Not Found")),
			Header:     make(http.Header),
		}, nil
	}

	dec := json.NewDecoder(req.Body)
	var body connectBody
	if err := dec.Decode(&body); err != nil {
		return &http.Response{
			StatusCode: http.StatusBadRequest,
			Body:       io.NopCloser(strings.NewReader(err.Error())),
			Header:     make(http.Header),
		}, nil
	}

	conn, err := net.DialTimeout("tcp", body.Target, 5*time.Second)
	if err != nil {
		return &http.Response{
			StatusCode: http.StatusBadGateway,
			Body:       io.NopCloser(strings.NewReader(err.Error())),
			Header:     make(http.Header),
		}, nil
	}

	// Forward remaining request body (upstream) to target in background.
	remaining := io.MultiReader(dec.Buffered(), req.Body)
	go func() {
		io.Copy(conn, remaining)
		if tc, ok := conn.(*net.TCPConn); ok {
			tc.CloseWrite()
		}
	}()

	// Return target connection as response body (downstream).
	return &http.Response{
		StatusCode: http.StatusOK,
		Body:       conn, // net.Conn implements io.ReadCloser
		Header:     make(http.Header),
	}, nil
}

// deadRelayTransport always returns a connection refused error.
type deadRelayTransport struct{}

func (drt *deadRelayTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	return nil, &net.OpError{Op: "dial", Net: "tcp", Err: fmt.Errorf("connection refused")}
}

// startE2EProxy starts an httpproxy.Server in a goroutine and waits for readiness.
func startE2EProxy(t *testing.T, ctx context.Context, tc TunnelClient) *Server {
	t.Helper()
	srv := NewServer("127.0.0.1:0", tc)
	go func() { _ = srv.Start(ctx) }()
	select {
	case <-srv.Ready():
	case <-time.After(3 * time.Second):
		t.Fatal("proxy did not become ready")
	}
	return srv
}

// TestE2E_IPCamouflage verifies the full CONNECT pipeline:
// client → proxy → relay transport → target → response.
func TestE2E_IPCamouflage(t *testing.T) {
	if os.Getenv("E2E") != "1" {
		t.Skip("set E2E=1 to run")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	// Target: simulates a "what is my IP" endpoint.
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("198.51.100.42"))
	}))
	defer target.Close()

	tc := &e2eTunnelClient{
		relayDomain: "relay.test",
		token:       "valid-token",
		httpClient:  &http.Client{Transport: &relayTransport{}},
	}

	proxy := startE2EProxy(t, ctx, tc)

	conn, err := net.DialTimeout("tcp", proxy.ListenAddr(), 5*time.Second)
	if err != nil {
		t.Fatalf("dial proxy: %v", err)
	}
	defer conn.Close()
	conn.SetDeadline(time.Now().Add(10 * time.Second))

	targetAddr := target.Listener.Addr().String()
	fmt.Fprintf(conn, "CONNECT %s HTTP/1.1\r\nHost: %s\r\n\r\n", targetAddr, targetAddr)

	br := bufio.NewReader(conn)
	resp, err := http.ReadResponse(br, nil)
	if err != nil {
		t.Fatalf("read CONNECT response: %v", err)
	}
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		t.Fatalf("CONNECT failed: %d %s", resp.StatusCode, string(body))
	}

	// Send HTTP request through the established tunnel.
	fmt.Fprintf(conn, "GET /ip HTTP/1.1\r\nHost: %s\r\nConnection: close\r\n\r\n", targetAddr)

	tunnelResp, err := http.ReadResponse(br, nil)
	if err != nil {
		t.Fatalf("read tunnel response: %v", err)
	}
	defer tunnelResp.Body.Close()

	body, err := io.ReadAll(tunnelResp.Body)
	if err != nil {
		t.Fatalf("read body: %v", err)
	}

	if got := string(body); got != "198.51.100.42" {
		t.Errorf("IP = %q, want 198.51.100.42", got)
	}

	t.Logf("IP camouflage OK: received %q through proxy → relay → target", string(body))
}

// TestE2E_ProxyCONNECT_TunnelDown verifies that when the relay is unreachable,
// the proxy returns an error within 10 seconds (no infinite hang).
func TestE2E_ProxyCONNECT_TunnelDown(t *testing.T) {
	if os.Getenv("E2E") != "1" {
		t.Skip("set E2E=1 to run")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	tc := &e2eTunnelClient{
		relayDomain: "relay.test",
		token:       "valid-token",
		httpClient:  &http.Client{Transport: &deadRelayTransport{}, Timeout: 10 * time.Second},
	}

	proxy := startE2EProxy(t, ctx, tc)

	start := time.Now()

	conn, err := net.DialTimeout("tcp", proxy.ListenAddr(), 5*time.Second)
	if err != nil {
		t.Fatalf("dial proxy: %v", err)
	}
	defer conn.Close()
	conn.SetDeadline(time.Now().Add(15 * time.Second))

	fmt.Fprintf(conn, "CONNECT example.com:443 HTTP/1.1\r\nHost: example.com:443\r\n\r\n")

	br := bufio.NewReader(conn)
	resp, err := http.ReadResponse(br, nil)
	elapsed := time.Since(start)

	if err != nil {
		if elapsed > 10*time.Second {
			t.Errorf("error took %v, expected < 10s", elapsed)
		}
		t.Logf("tunnel down: connection error in %v (expected)", elapsed)
		return
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusOK {
		t.Error("expected error status when relay is down, got 200")
	}
	if elapsed > 10*time.Second {
		t.Errorf("response took %v, expected < 10s (no hang)", elapsed)
	}

	t.Logf("tunnel down OK: status %d in %v", resp.StatusCode, elapsed)
}

// TestE2E_SessionTokenRefresh verifies that the proxy calls EnsureSessionToken
// before making relay requests and uses the refreshed token.
func TestE2E_SessionTokenRefresh(t *testing.T) {
	if os.Getenv("E2E") != "1" {
		t.Skip("set E2E=1 to run")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("OK"))
	}))
	defer target.Close()

	var tokensMu sync.Mutex
	var receivedTokens []string

	const refreshedToken = "refreshed-abc-xyz"

	tc := &e2eTunnelClient{
		relayDomain:  "relay.test",
		token:        "old-expired-token",
		refreshToken: refreshedToken,
		expired:      true,
		httpClient: &http.Client{Transport: &relayTransport{
			authCheck: func(auth string) bool {
				tokensMu.Lock()
				receivedTokens = append(receivedTokens, auth)
				tokensMu.Unlock()
				return true
			},
		}},
	}

	proxy := startE2EProxy(t, ctx, tc)

	conn, err := net.DialTimeout("tcp", proxy.ListenAddr(), 5*time.Second)
	if err != nil {
		t.Fatalf("dial proxy: %v", err)
	}
	defer conn.Close()
	conn.SetDeadline(time.Now().Add(10 * time.Second))

	targetAddr := target.Listener.Addr().String()
	fmt.Fprintf(conn, "CONNECT %s HTTP/1.1\r\nHost: %s\r\n\r\n", targetAddr, targetAddr)

	br := bufio.NewReader(conn)
	resp, err := http.ReadResponse(br, nil)
	if err != nil {
		t.Fatalf("read CONNECT response: %v", err)
	}
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		t.Fatalf("CONNECT failed: %d %s", resp.StatusCode, string(body))
	}

	fmt.Fprintf(conn, "GET / HTTP/1.1\r\nHost: %s\r\nConnection: close\r\n\r\n", targetAddr)
	tunnelResp, err := http.ReadResponse(br, nil)
	if err != nil {
		t.Fatalf("read tunnel response: %v", err)
	}
	io.ReadAll(tunnelResp.Body)
	tunnelResp.Body.Close()

	if tc.refreshCount.Load() == 0 {
		t.Error("EnsureSessionToken was never called")
	}

	tokensMu.Lock()
	defer tokensMu.Unlock()

	found := false
	for _, tok := range receivedTokens {
		if tok == "Bearer "+refreshedToken {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("refreshed token not used; received tokens: %v", receivedTokens)
	}

	t.Logf("session token refresh OK: %d refreshes, tokens: %v", tc.refreshCount.Load(), receivedTokens)
}
