package httpproxy

import (
	"context"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestNonConnectNonAbsoluteURLReturns400(t *testing.T) {
	mock := &mockTunnelClient{
		sessionToken: "test-token",
		relayDomain:  "relay.example.com",
		httpClient:   http.DefaultClient,
	}

	handler := &connectHandler{
		tunnelClient: mock,
		wg:           &sync.WaitGroup{},
	}

	// Start a real TCP server with the handler so hijack works.
	srv := httptest.NewServer(handler)
	defer srv.Close()

	// Make a plain GET request with a relative URL — should get 400
	// because handleHTTP requires absolute proxy-style URLs.
	client := &http.Client{
		Timeout: 2 * time.Second,
	}
	resp, err := client.Get(srv.URL + "/anything")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusBadRequest {
		t.Errorf("expected 400, got %d", resp.StatusCode)
	}
}

func TestConnectWithNoHostReturns400(t *testing.T) {
	mock := &mockTunnelClient{
		sessionToken: "test-token",
		relayDomain:  "relay.example.com",
		httpClient:   http.DefaultClient,
	}

	handler := &connectHandler{
		tunnelClient: mock,
		wg:           &sync.WaitGroup{},
	}

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("failed to listen: %v", err)
	}
	defer ln.Close()

	srv := &http.Server{Handler: handler}
	go srv.Serve(ln)
	defer srv.Close()

	conn, err := net.DialTimeout("tcp", ln.Addr().String(), 2*time.Second)
	if err != nil {
		t.Fatalf("failed to dial: %v", err)
	}
	defer conn.Close()

	// Send a CONNECT request with empty host.
	_, err = conn.Write([]byte("CONNECT HTTP/1.1\r\nHost: \r\n\r\n"))
	if err != nil {
		t.Fatalf("failed to write request: %v", err)
	}

	conn.SetReadDeadline(time.Now().Add(2 * time.Second))
	buf := make([]byte, 4096)
	n, _ := conn.Read(buf)
	response := string(buf[:n])

	if !strings.Contains(response, "400") {
		t.Fatalf("expected 400 response, got: %s", response)
	}
}

// captureTransport is an http.RoundTripper that captures the outgoing relay
// request details and returns a synthetic 200 OK response with a streaming
// body, avoiding the bidirectional pipe deadlock in unit tests.
type captureTransport struct {
	method string
	url    string
	auth   string
	called bool
}

func (ct *captureTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	ct.called = true
	ct.method = req.Method
	ct.url = req.URL.String()
	ct.auth = req.Header.Get("Authorization")

	// Return a 200 OK with an empty streaming body.
	// Use a pipe so the response body stays open (like a real relay would).
	pr, pw := net.Pipe()
	go func() {
		// Close after a short delay to let the proxy goroutine run.
		time.Sleep(500 * time.Millisecond)
		pw.Close()
	}()

	return &http.Response{
		StatusCode: http.StatusOK,
		Body:       pr,
		Header:     make(http.Header),
	}, nil
}

func TestConnectWithValidTarget(t *testing.T) {
	ct := &captureTransport{}

	mock := &mockTunnelClient{
		sessionToken: "test-token",
		relayDomain:  "relay.example.com",
		httpClient:   &http.Client{Transport: ct},
	}

	wg := &sync.WaitGroup{}
	handler := &connectHandler{
		tunnelClient: mock,
		wg:           wg,
	}

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("failed to listen: %v", err)
	}
	defer ln.Close()

	srv := &http.Server{Handler: handler}
	go srv.Serve(ln)
	defer srv.Close()

	conn, err := net.DialTimeout("tcp", ln.Addr().String(), 2*time.Second)
	if err != nil {
		t.Fatalf("failed to dial proxy: %v", err)
	}
	defer conn.Close()

	_, err = conn.Write([]byte("CONNECT example.com:443 HTTP/1.1\r\nHost: example.com:443\r\n\r\n"))
	if err != nil {
		t.Fatalf("failed to write CONNECT: %v", err)
	}

	conn.SetReadDeadline(time.Now().Add(3 * time.Second))
	buf := make([]byte, 4096)
	n, _ := conn.Read(buf)
	response := string(buf[:n])

	// The proxy should send "200 Connection Established" after the relay returns 200.
	if !strings.Contains(response, "200") {
		t.Fatalf("expected 200 response, got: %q", response)
	}

	// Verify EnsureSessionToken was called.
	if !mock.ensureSessionTokenCalled {
		t.Fatal("expected EnsureSessionToken to be called")
	}

	// Verify the relay request was properly built.
	if !ct.called {
		t.Fatal("expected relay HTTP request to be made")
	}
	if ct.method != http.MethodPost {
		t.Errorf("expected POST to relay, got %s", ct.method)
	}
	if ct.url != "https://relay.example.com/connect" {
		t.Errorf("expected relay URL https://relay.example.com/connect, got %s", ct.url)
	}
	if ct.auth != "Bearer test-token" {
		t.Errorf("expected Authorization 'Bearer test-token', got %s", ct.auth)
	}

	// Close connection to trigger cleanup and wait for WaitGroup.
	conn.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	done := make(chan struct{})
	go func() {
		wg.Wait()
		close(done)
	}()
	select {
	case <-done:
	case <-ctx.Done():
		// WaitGroup not drained in time — acceptable for test purposes.
	}
}
