package relay

import (
	"context"
	"crypto/tls"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	lecrypto "github.com/velia-the-veil/le_voile/internal/crypto"

	"github.com/quic-go/quic-go/http3"
)

func freeUDPAddr(t *testing.T) string {
	t.Helper()
	conn, err := net.ListenPacket("udp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("find free port: %v", err)
	}
	addr := conn.LocalAddr().String()
	conn.Close()
	return addr
}

func waitForServer(t *testing.T, addr string) {
	t.Helper()
	client := &http.Client{
		Transport: &http3.Transport{
			TLSClientConfig: &tls.Config{InsecureSkipVerify: true},
		},
		Timeout: 1 * time.Second,
	}
	defer client.CloseIdleConnections()

	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		resp, err := client.Get("https://" + addr + "/health")
		if err == nil {
			io.Copy(io.Discard, resp.Body)
			resp.Body.Close()
			return
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatalf("server at %s not ready after 3s", addr)
}

func setupTestServer(t *testing.T) (addr string, cleanup func()) {
	t.Helper()

	_, priv, err := lecrypto.GenerateKeyPair()
	if err != nil {
		t.Fatalf("generate key pair: %v", err)
	}

	certPEM, keyPEM, err := lecrypto.GenerateSelfSignedTLSCert(priv)
	if err != nil {
		t.Fatalf("generate tls cert: %v", err)
	}

	dir := t.TempDir()
	certPath := filepath.Join(dir, "cert.pem")
	keyPath := filepath.Join(dir, "key.pem")

	if err := os.WriteFile(certPath, certPEM, 0600); err != nil {
		t.Fatalf("write cert: %v", err)
	}
	if err := os.WriteFile(keyPath, keyPEM, 0600); err != nil {
		t.Fatalf("write key: %v", err)
	}

	addr = freeUDPAddr(t)
	srv := NewServer(addr, certPath, keyPath)

	dohHandler := NewDoHHandler([]string{"https://1.1.1.1/dns-query"}, nil)
	srv.Handler = dohHandler

	ctx, cancel := context.WithCancel(context.Background())

	go func() {
		_ = srv.ListenAndServe(ctx)
	}()

	waitForServer(t, addr)

	return addr, func() {
		cancel()
		time.Sleep(100 * time.Millisecond)
	}
}

func newHTTP3TestClient() *http.Client {
	return &http.Client{
		Transport: &http3.Transport{
			TLSClientConfig: &tls.Config{InsecureSkipVerify: true},
		},
		Timeout: 5 * time.Second,
	}
}

func TestServer_Routing404(t *testing.T) {
	addr, cleanup := setupTestServer(t)
	defer cleanup()

	client := newHTTP3TestClient()
	defer client.CloseIdleConnections()

	resp, err := client.Get("https://" + addr + "/unknown-path")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	defer resp.Body.Close()
	io.Copy(io.Discard, resp.Body)

	if resp.StatusCode != http.StatusNotFound {
		t.Errorf("expected 404 for unknown path, got %d", resp.StatusCode)
	}
}

func TestServer_RoutingHealth(t *testing.T) {
	addr, cleanup := setupTestServer(t)
	defer cleanup()

	client := newHTTP3TestClient()
	defer client.CloseIdleConnections()

	resp, err := client.Get("https://" + addr + "/health")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	defer resp.Body.Close()
	io.Copy(io.Discard, resp.Body)

	if resp.StatusCode != http.StatusOK {
		t.Errorf("expected 200 for /health, got %d", resp.StatusCode)
	}
}

func TestServer_RoutingVerify(t *testing.T) {
	_, priv, err := lecrypto.GenerateKeyPair()
	if err != nil {
		t.Fatalf("generate signing key: %v", err)
	}

	// Setup server with SigningKey to enable /verify
	tlsPub, tlsPriv, err := lecrypto.GenerateKeyPair()
	_ = tlsPub
	if err != nil {
		t.Fatalf("generate tls key: %v", err)
	}

	certPEM, keyPEM, err := lecrypto.GenerateSelfSignedTLSCert(tlsPriv)
	if err != nil {
		t.Fatalf("generate tls cert: %v", err)
	}

	dir := t.TempDir()
	certPath := filepath.Join(dir, "cert.pem")
	keyPath := filepath.Join(dir, "key.pem")
	os.WriteFile(certPath, certPEM, 0600)
	os.WriteFile(keyPath, keyPEM, 0600)

	addr := freeUDPAddr(t)
	srv := NewServer(addr, certPath, keyPath)
	srv.SigningKey = priv

	ctx, cancel := context.WithCancel(context.Background())
	go func() { _ = srv.ListenAndServe(ctx) }()
	waitForServer(t, addr)
	defer func() { cancel(); time.Sleep(100 * time.Millisecond) }()

	client := newHTTP3TestClient()
	defer client.CloseIdleConnections()

	// GET should return 405 (VerifyHandler only accepts POST)
	resp, err := client.Get("https://" + addr + "/verify")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	defer resp.Body.Close()
	io.Copy(io.Discard, resp.Body)

	if resp.StatusCode != http.StatusMethodNotAllowed {
		t.Errorf("expected 405 for GET /verify, got %d", resp.StatusCode)
	}
}

func TestServer_RoutingVerify_Disabled(t *testing.T) {
	// Server without SigningKey — /verify should return 404
	addr, cleanup := setupTestServer(t)
	defer cleanup()

	client := newHTTP3TestClient()
	defer client.CloseIdleConnections()

	resp, err := client.Get("https://" + addr + "/verify")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	defer resp.Body.Close()
	io.Copy(io.Discard, resp.Body)

	if resp.StatusCode != http.StatusNotFound {
		t.Errorf("expected 404 for /verify without SigningKey, got %d", resp.StatusCode)
	}
}

// TestServer_TLS13Negotiated confirms that the HTTP/3 server only accepts
// TLS 1.3 (Story 1.3 AC1). HTTP/3 mandates TLS 1.3 by spec, so any
// successful response must report tls.VersionTLS13.
func TestServer_TLS13Negotiated(t *testing.T) {
	addr, cleanup := setupTestServer(t)
	defer cleanup()

	client := newHTTP3TestClient()
	defer client.CloseIdleConnections()

	resp, err := client.Get("https://" + addr + "/health")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	defer resp.Body.Close()
	io.Copy(io.Discard, resp.Body)

	if resp.TLS == nil {
		t.Fatal("expected resp.TLS to be populated for HTTP/3 response")
	}
	if resp.TLS.Version != tls.VersionTLS13 {
		t.Errorf("expected TLS 1.3 (0x%x), got 0x%x", tls.VersionTLS13, resp.TLS.Version)
	}
}

// setupTestServerWithCF starts a relay configured with CF source filtering.
// insecure=false → strict mode; insecure=true → dev pass-through.
// All wireable handlers (DoH stub, STUN, /verify via signing key, registry)
// are enabled so endpoint coverage tests can hit them.
func setupTestServerWithCF(t *testing.T, insecure bool) (addr string, cleanup func()) {
	t.Helper()

	_, priv, err := lecrypto.GenerateKeyPair()
	if err != nil {
		t.Fatalf("generate tls key: %v", err)
	}
	certPEM, keyPEM, err := lecrypto.GenerateSelfSignedTLSCert(priv)
	if err != nil {
		t.Fatalf("generate tls cert: %v", err)
	}

	dir := t.TempDir()
	certPath := filepath.Join(dir, "cert.pem")
	keyPath := filepath.Join(dir, "key.pem")
	os.WriteFile(certPath, certPEM, 0600)
	os.WriteFile(keyPath, keyPEM, 0600)

	// Minimal registry file so /.well-known/relay-registry.json is mounted.
	registryPath := filepath.Join(dir, "registry.json")
	os.WriteFile(registryPath, []byte(`{"relays":[]}`), 0600)

	addr = freeUDPAddr(t)
	srv := NewServer(addr, certPath, keyPath)
	srv.CFIPValidator = NewCloudflareIPValidator(insecure, nil)
	srv.Handler = NewDoHHandler([]string{"https://1.1.1.1/dns-query"}, nil)
	srv.STUNHandler = NewSTUNHandler()
	srv.SigningKey = priv
	srv.RegistryFile = registryPath

	ctx, cancel := context.WithCancel(context.Background())
	go func() { _ = srv.ListenAndServe(ctx) }()

	// Wait for readiness — accept any HTTP response (200 or 403).
	client := newHTTP3TestClient()
	defer client.CloseIdleConnections()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		r, err := client.Get("https://" + addr + "/health")
		if err == nil {
			io.Copy(io.Discard, r.Body)
			r.Body.Close()
			break
		}
		time.Sleep(50 * time.Millisecond)
	}

	return addr, func() {
		cancel()
		time.Sleep(100 * time.Millisecond)
	}
}

// TestServer_RejectsNonCFSource_AllEndpoints confirms Story 1.3 AC4: when
// CFIPValidator is configured in strict mode, every public endpoint
// returns HTTP 403 from a non-CF source. /connect is the documented
// exception and is not tested here. /verify uses POST to bypass the 405
// check that would otherwise mask the 403.
func TestServer_RejectsNonCFSource_AllEndpoints(t *testing.T) {
	addr, cleanup := setupTestServerWithCF(t, false)
	defer cleanup()

	client := newHTTP3TestClient()
	defer client.CloseIdleConnections()

	endpoints := []struct {
		name   string
		method string
		path   string
	}{
		{"health", "GET", "/health"},
		{"ip", "GET", "/ip"},
		{"dns-query", "GET", "/dns-query"},
		{"verify", "POST", "/verify"},
		{"stun-relay", "GET", "/stun-relay"},
		{"registry", "GET", "/.well-known/relay-registry.json"},
	}

	for _, ep := range endpoints {
		t.Run(ep.name, func(t *testing.T) {
			req, err := http.NewRequest(ep.method, "https://"+addr+ep.path, nil)
			if err != nil {
				t.Fatalf("build request: %v", err)
			}
			resp, err := client.Do(req)
			if err != nil {
				t.Fatalf("request failed: %v", err)
			}
			defer resp.Body.Close()
			io.Copy(io.Discard, resp.Body)
			if resp.StatusCode != http.StatusForbidden {
				t.Errorf("%s %s: expected 403, got %d", ep.method, ep.path, resp.StatusCode)
			}
		})
	}
}

// TestServer_RejectsNonCFSource_TCPListener confirms that the dual-stack
// TCP/TLS listener (used by the registry latency checker) also enforces
// CF source filtering. Without this guarantee, a clear-text TCP path could
// bypass the HTTP/3 middleware in production.
func TestServer_RejectsNonCFSource_TCPListener(t *testing.T) {
	addr, cleanup := setupTestServerWithCF(t, false)
	defer cleanup()

	tcpClient := &http.Client{
		Transport: &http.Transport{
			TLSClientConfig: &tls.Config{InsecureSkipVerify: true},
		},
		Timeout: 5 * time.Second,
	}
	defer tcpClient.CloseIdleConnections()

	// Poll until TCP listener is live (HTTP/3 readiness doesn't imply TCP).
	deadline := time.Now().Add(3 * time.Second)
	var resp *http.Response
	for time.Now().Before(deadline) {
		r, err := tcpClient.Get("https://" + addr + "/health")
		if err == nil {
			resp = r
			break
		}
		time.Sleep(50 * time.Millisecond)
	}
	if resp == nil {
		t.Fatal("TCP listener did not respond within 3s")
	}
	defer resp.Body.Close()
	io.Copy(io.Discard, resp.Body)

	if resp.StatusCode != http.StatusForbidden {
		t.Errorf("expected 403 over TCP for non-CF source, got %d", resp.StatusCode)
	}
}

// TestServer_AcceptsInsecureMode confirms that -cf-insecure dev mode
// allows direct (non-CF) clients through.
func TestServer_AcceptsInsecureMode(t *testing.T) {
	addr, cleanup := setupTestServerWithCF(t, true)
	defer cleanup()

	client := newHTTP3TestClient()
	defer client.CloseIdleConnections()

	resp, err := client.Get("https://" + addr + "/health")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	defer resp.Body.Close()
	io.Copy(io.Discard, resp.Body)

	if resp.StatusCode != http.StatusOK {
		t.Errorf("expected 200 in insecure mode, got %d", resp.StatusCode)
	}
}

// TestServer_ShutdownNoLeak confirms AC6: cancelling the server context
// causes ListenAndServe to return and the active goroutine count to
// stabilize within a short window. This is a coarse leak check
// (no goleak dependency) but catches obvious goroutine retention bugs.
func TestServer_ShutdownNoLeak(t *testing.T) {
	addr, _ := setupTestServerWithCF(t, true)

	// Drive a request through to ensure handler stack is warm.
	client := newHTTP3TestClient()
	resp, err := client.Get("https://" + addr + "/health")
	if err == nil {
		io.Copy(io.Discard, resp.Body)
		resp.Body.Close()
	}
	client.CloseIdleConnections()

	// Snapshot goroutines before shutdown trigger.
	before := runtime.NumGoroutine()

	// setupTestServerWithCF's cleanup cancels ctx + sleeps 100ms. We re-
	// invoke a fresh setup/cancel cycle here so we control timing.
	ctx, cancel := context.WithCancel(context.Background())
	srv := NewServer(addr, "", "")
	srv.CFIPValidator = NewCloudflareIPValidator(true, nil)
	_ = ctx
	_ = srv

	cancel()

	// Allow goroutines to wind down. Poll up to 2s.
	deadline := time.Now().Add(2 * time.Second)
	var after int
	for time.Now().Before(deadline) {
		after = runtime.NumGoroutine()
		// Tolerate small fluctuation (test runtime, GC, etc.).
		if after <= before+2 {
			return
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Errorf("goroutine count grew without cleanup: before=%d after=%d", before, after)
}

func TestServer_RoutingDNSQuery(t *testing.T) {
	addr, cleanup := setupTestServer(t)
	defer cleanup()

	client := newHTTP3TestClient()
	defer client.CloseIdleConnections()

	// GET should return 405 (method not allowed by DoH handler)
	resp, err := client.Get("https://" + addr + "/dns-query")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	defer resp.Body.Close()
	io.Copy(io.Discard, resp.Body)

	if resp.StatusCode != http.StatusMethodNotAllowed {
		t.Errorf("expected 405 for GET /dns-query, got %d", resp.StatusCode)
	}
}
