package relay

import (
	"context"
	"crypto/tls"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
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
