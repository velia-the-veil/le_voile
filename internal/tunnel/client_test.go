package tunnel

import (
	"context"
	"crypto/ed25519"
	"crypto/tls"
	"errors"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	lecrypto "github.com/velia-the-veil/le_voile/internal/crypto"
	"github.com/velia-the-veil/le_voile/relay/relay"

	"github.com/quic-go/quic-go/http3"
)

// freeUDPAddr finds a free UDP port on localhost.
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

// waitForRelay polls the test relay until it's ready.
func waitForRelay(t *testing.T, addr string) {
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
	t.Fatalf("relay at %s not ready after 3s", addr)
}

// startTestRelay starts an HTTP/3 relay with /verify and /health endpoints.
func startTestRelay(t *testing.T, signingKey ed25519.PrivateKey) (addr string, cleanup func()) {
	t.Helper()

	_, tlsPriv, err := lecrypto.GenerateKeyPair()
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

	if err := os.WriteFile(certPath, certPEM, 0600); err != nil {
		t.Fatalf("write cert: %v", err)
	}
	if err := os.WriteFile(keyPath, keyPEM, 0600); err != nil {
		t.Fatalf("write key: %v", err)
	}

	addr = freeUDPAddr(t)
	srv := relay.NewServer(addr, certPath, keyPath)
	srv.SigningKey = signingKey

	// Minimal DoH handler for SendDoHQuery tests
	dohHandler := relay.NewDoHHandler([]string{"https://1.1.1.1/dns-query"}, nil)
	srv.Handler = dohHandler

	ctx, cancel := context.WithCancel(context.Background())

	go func() {
		_ = srv.ListenAndServe(ctx)
	}()

	waitForRelay(t, addr)

	return addr, func() {
		cancel()
		time.Sleep(100 * time.Millisecond)
	}
}

// newTestClient creates a tunnel Client pointing at a local test relay with InsecureSkipVerify.
//
// R-T8 — uses c.buildTransport() so the custom Dial callback (dialQUICCustom)
// is wired in tests too. Without it, the http3 layer would dial via its
// internal default transport and the *quic.Conn / *quic.Transport capture
// (used by MigrateToFD) would never populate, breaking migration_test.go
// and heartbeat_test.go.
func newTestClient(t *testing.T, addr string, pubKeyBase64 string) *Client {
	t.Helper()

	pubKey, err := lecrypto.ImportPublicKeyBase64(pubKeyBase64)
	if err != nil {
		t.Fatalf("import public key: %v", err)
	}

	c := &Client{
		relayDomain: addr,
		relayIP:     addr,
		relayPubKey: pubKey,
		insecure:    true,
		state:       NewStateManager(),
	}
	tr := c.buildTransport()
	c.httpClient = &http.Client{Transport: tr}
	c.transport = tr
	return c
}

func TestClient_NewClient_ValidKey(t *testing.T) {
	pub, _, err := lecrypto.GenerateKeyPair()
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}

	pubB64 := lecrypto.ExportPublicKeyBase64(pub)
	// Use a raw IP so NewClient skips DNS resolution.
	client, err := NewClient("127.0.0.1", pubB64)
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}

	if client.relayDomain != "127.0.0.1" {
		t.Errorf("relayDomain = %q, want %q", client.relayDomain, "127.0.0.1")
	}
	if client.state.Get() != StateDisconnected {
		t.Errorf("initial state = %q, want %q", client.state.Get(), StateDisconnected)
	}
}

func TestClient_NewClient_InvalidKey(t *testing.T) {
	_, err := NewClient("levoile.dev", "not-valid-base64-key")
	if err == nil {
		t.Fatal("expected error for invalid key, got nil")
	}
}

func TestClient_NewClient_WithResolvedIP_BypassesLookup(t *testing.T) {
	// Garantit que NewClient n'appelle PAS net.LookupIP quand le hint est
	// fourni. Sans ce bypass, la séquence Linux (firewall.Activate avant
	// tunnel.NewClient) faisait hang la résolution systemd-resolved et
	// surfacait comme ErrConnectionTimeout sans qu'aucun paquet QUIC ne
	// soit émis. Le domaine ".invalid" est garanti non-résolvable (RFC 6761)
	// donc si NewClient ignore le hint et tape DNS, le test échoue.
	pub, _, err := lecrypto.GenerateKeyPair()
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}
	pubB64 := lecrypto.ExportPublicKeyBase64(pub)

	client, err := NewClient("relay.invalid", pubB64, WithResolvedIP("203.0.113.42"))
	if err != nil {
		t.Fatalf("NewClient with resolved hint: %v", err)
	}
	if client.relayDomain != "relay.invalid" {
		t.Errorf("relayDomain = %q, want preserved %q", client.relayDomain, "relay.invalid")
	}
	if client.relayIP != "203.0.113.42" {
		t.Errorf("relayIP = %q, want hint %q", client.relayIP, "203.0.113.42")
	}
}

func TestClient_NewClient_WithResolvedIP_RejectsBadHint(t *testing.T) {
	pub, _, err := lecrypto.GenerateKeyPair()
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}
	pubB64 := lecrypto.ExportPublicKeyBase64(pub)

	_, err = NewClient("relay.example.com", pubB64, WithResolvedIP("not-an-ip"))
	if err == nil {
		t.Fatal("expected error for malformed resolved IP hint, got nil")
	}
}

func TestClient_Connect_VerificationSuccess(t *testing.T) {
	pub, priv, err := lecrypto.GenerateKeyPair()
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}

	addr, cleanup := startTestRelay(t, priv)
	defer cleanup()

	pubB64 := lecrypto.ExportPublicKeyBase64(pub)
	client := newTestClient(t, addr, pubB64)
	defer client.Disconnect()

	if err := client.Connect(context.Background()); err != nil {
		t.Fatalf("Connect: %v", err)
	}

	if client.state.Get() != StateConnected {
		t.Errorf("state = %q, want %q", client.state.Get(), StateConnected)
	}
}

func TestClient_Connect_VerificationFailed(t *testing.T) {
	_, priv, err := lecrypto.GenerateKeyPair()
	if err != nil {
		t.Fatalf("generate relay key: %v", err)
	}

	addr, cleanup := startTestRelay(t, priv)
	defer cleanup()

	// Use a different public key so verification fails
	wrongPub, _, err := lecrypto.GenerateKeyPair()
	if err != nil {
		t.Fatalf("generate wrong key: %v", err)
	}

	wrongPubB64 := lecrypto.ExportPublicKeyBase64(wrongPub)
	client := newTestClient(t, addr, wrongPubB64)
	defer client.Disconnect()

	err = client.Connect(context.Background())
	if err == nil {
		t.Fatal("expected verification error, got nil")
	}

	if client.state.Get() != StateDisconnected {
		t.Errorf("state = %q, want %q", client.state.Get(), StateDisconnected)
	}
}

func TestClient_Connect_Timeout(t *testing.T) {
	pub, _, err := lecrypto.GenerateKeyPair()
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}

	pubB64 := lecrypto.ExportPublicKeyBase64(pub)
	// Point to unreachable address to trigger timeout
	client := newTestClient(t, "192.0.2.1:443", pubB64)

	ctx, cancel := context.WithTimeout(context.Background(), 1*time.Second)
	defer cancel()

	err = client.Connect(ctx)
	if err == nil {
		t.Fatal("expected timeout error, got nil")
	}

	if client.state.Get() != StateDisconnected {
		t.Errorf("state = %q after timeout, want %q", client.state.Get(), StateDisconnected)
	}
}

func TestClient_SendDoHQuery(t *testing.T) {
	pub, priv, err := lecrypto.GenerateKeyPair()
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}

	addr, cleanup := startTestRelay(t, priv)
	defer cleanup()

	pubB64 := lecrypto.ExportPublicKeyBase64(pub)
	client := newTestClient(t, addr, pubB64)
	defer client.Disconnect()

	if err := client.Connect(context.Background()); err != nil {
		t.Fatalf("Connect: %v", err)
	}

	// Send a minimal DNS wire-format payload (will be forwarded by DoH handler)
	dnsPayload := []byte{
		0x00, 0x01, // ID
		0x01, 0x00, // Flags: standard query
		0x00, 0x01, // Questions: 1
		0x00, 0x00, // Answers: 0
		0x00, 0x00, // Authority: 0
		0x00, 0x00, // Additional: 0
		0x07, 'e', 'x', 'a', 'm', 'p', 'l', 'e',
		0x03, 'c', 'o', 'm',
		0x00,       // Root
		0x00, 0x01, // Type A
		0x00, 0x01, // Class IN
	}

	resp, err := client.SendDoHQuery(context.Background(), dnsPayload)
	if err != nil {
		t.Fatalf("SendDoHQuery: %v", err)
	}

	if len(resp) == 0 {
		t.Error("expected non-empty DNS response")
	}
}

func TestClient_SendDoHQuery_NotConnected(t *testing.T) {
	pub, _, err := lecrypto.GenerateKeyPair()
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}

	pubB64 := lecrypto.ExportPublicKeyBase64(pub)
	client := newTestClient(t, "127.0.0.1:1", pubB64)

	_, err = client.SendDoHQuery(context.Background(), []byte{0x00})
	if err != ErrNotConnected {
		t.Errorf("error = %v, want ErrNotConnected", err)
	}
}

func TestClient_Disconnect(t *testing.T) {
	pub, priv, err := lecrypto.GenerateKeyPair()
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}

	addr, cleanup := startTestRelay(t, priv)
	defer cleanup()

	pubB64 := lecrypto.ExportPublicKeyBase64(pub)
	client := newTestClient(t, addr, pubB64)

	if err := client.Connect(context.Background()); err != nil {
		t.Fatalf("Connect: %v", err)
	}

	if err := client.Disconnect(); err != nil {
		t.Fatalf("Disconnect: %v", err)
	}

	if client.state.Get() != StateDisconnected {
		t.Errorf("state = %q, want %q", client.state.Get(), StateDisconnected)
	}
}

func TestClient_StateTransitions(t *testing.T) {
	pub, priv, err := lecrypto.GenerateKeyPair()
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}

	addr, cleanup := startTestRelay(t, priv)
	defer cleanup()

	pubB64 := lecrypto.ExportPublicKeyBase64(pub)
	client := newTestClient(t, addr, pubB64)

	// Initial state
	if client.state.Get() != StateDisconnected {
		t.Fatalf("initial state = %q, want %q", client.state.Get(), StateDisconnected)
	}

	// Connect → should transition through connecting → connected
	if err := client.Connect(context.Background()); err != nil {
		t.Fatalf("Connect: %v", err)
	}
	if client.state.Get() != StateConnected {
		t.Errorf("after connect state = %q, want %q", client.state.Get(), StateConnected)
	}

	// Disconnect → disconnected
	if err := client.Disconnect(); err != nil {
		t.Fatalf("Disconnect: %v", err)
	}
	if client.state.Get() != StateDisconnected {
		t.Errorf("after disconnect state = %q, want %q", client.state.Get(), StateDisconnected)
	}
}

// startTestRelayWithSharedKey starts a relay that uses key for BOTH the TLS
// certificate and the signing (challenge/response) endpoint. This allows
// integration tests to verify certificate pinning against a known public key.
func startTestRelayWithSharedKey(t *testing.T, key ed25519.PrivateKey) (addr string, cleanup func()) {
	t.Helper()

	certPEM, keyPEM, err := lecrypto.GenerateSelfSignedTLSCert(key)
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
	srv := relay.NewServer(addr, certPath, keyPath)
	srv.SigningKey = key // same key for signing
	srv.Handler = relay.NewDoHHandler([]string{"https://1.1.1.1/dns-query"}, nil)

	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		_ = srv.ListenAndServe(ctx)
	}()

	waitForRelay(t, addr)

	return addr, func() {
		cancel()
		time.Sleep(100 * time.Millisecond)
	}
}



// TestClient_PinningRefusesWrongKey verifies that a client with the wrong
// pinned key cannot connect to the relay (TLS handshake fails at pinning step).
// Uses NewClient with WithInsecureSkipCAOnly to exercise the production VerifyPeerCertificate
// code path while allowing self-signed test certificates.
func TestClient_PinningRefusesWrongKey(t *testing.T) {
	// Generate relay key used for BOTH TLS cert and signing
	pub, priv, err := lecrypto.GenerateKeyPair()
	if err != nil {
		t.Fatalf("generate relay key: %v", err)
	}

	addr, cleanup := startTestRelayWithSharedKey(t, priv)
	defer cleanup()

	// A different key — wrong for pinning
	wrongPub, _, err := lecrypto.GenerateKeyPair()
	if err != nil {
		t.Fatalf("generate wrong key: %v", err)
	}

	pubB64 := lecrypto.ExportPublicKeyBase64(pub)
	wrongPubB64 := lecrypto.ExportPublicKeyBase64(wrongPub)

	// Client with wrong pinned key — connection must fail (pinning rejects mismatched cert key)
	wrongClient, err := NewClient(addr, wrongPubB64, WithInsecureSkipCAOnly())
	if err != nil {
		t.Fatalf("NewClient (wrong key): %v", err)
	}
	err = wrongClient.Connect(context.Background())
	if err == nil {
		wrongClient.Disconnect()
		t.Fatal("expected pinning failure, got nil")
	}
	// NFR regression guard: the error must wrap ErrPinningFailed so callers
	// can distinguish pinning rejection from generic transport failures.
	// Note: quic-go wraps VerifyPeerCertificate errors — errors.Is walks the
	// chain and should still find ErrPinningFailed from our %w wrapping in
	// VerifyPeerCertificate.
	if !errors.Is(err, ErrPinningFailed) {
		t.Errorf("expected error wrapping ErrPinningFailed, got %v", err)
	}
	if wrongClient.state.Get() != StateDisconnected {
		t.Errorf("state = %q after pinning failure, want %q", wrongClient.state.Get(), StateDisconnected)
	}

	// Client with correct pinned key — connection must succeed
	correctClient, err := NewClient(addr, pubB64, WithInsecureSkipCAOnly())
	if err != nil {
		t.Fatalf("NewClient (correct key): %v", err)
	}
	if err := correctClient.Connect(context.Background()); err != nil {
		t.Fatalf("expected success with correct pinned key: %v", err)
	}
	if correctClient.state.Get() != StateConnected {
		t.Errorf("state = %q after correct pinning, want %q", correctClient.state.Get(), StateConnected)
	}
	correctClient.Disconnect()
}

// TestConnectNew_OneShot verifies the Story 1.1 convenience helper that
// matches the acceptance-criteria signature tunnel.Connect(ctx, domain, key).
func TestConnectNew_OneShot(t *testing.T) {
	pub, priv, err := lecrypto.GenerateKeyPair()
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}

	addr, cleanup := startTestRelayWithSharedKey(t, priv)
	defer cleanup()

	pubB64 := lecrypto.ExportPublicKeyBase64(pub)
	client, err := ConnectNew(context.Background(), addr, pubB64, WithInsecureSkipCAOnly())
	if err != nil {
		t.Fatalf("ConnectNew: %v", err)
	}
	defer client.Disconnect()

	if client.state.Get() != StateConnected {
		t.Errorf("state = %q, want %q", client.state.Get(), StateConnected)
	}
}

// TestConnectNew_PinningFailure verifies ConnectNew propagates pinning errors
// from Connect and guarantees (nil, err) on failure (no client leak).
func TestConnectNew_PinningFailure(t *testing.T) {
	_, priv, err := lecrypto.GenerateKeyPair()
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}
	addr, cleanup := startTestRelayWithSharedKey(t, priv)
	defer cleanup()

	wrongPub, _, err := lecrypto.GenerateKeyPair()
	if err != nil {
		t.Fatalf("generate wrong key: %v", err)
	}
	wrongB64 := lecrypto.ExportPublicKeyBase64(wrongPub)

	client, err := ConnectNew(context.Background(), addr, wrongB64, WithInsecureSkipCAOnly())
	if err == nil {
		t.Fatal("expected pinning error, got nil")
	}
	if client != nil {
		t.Errorf("contract violation: expected nil client on failure, got %v", client)
	}
}

func TestClient_UpdateRelay_ThreadSafe(t *testing.T) {
	pub, priv, err := ed25519.GenerateKey(nil)
	if err != nil {
		t.Fatal(err)
	}
	_ = priv

	addr, cleanup := startTestRelay(t, priv)
	defer cleanup()
	waitForRelay(t, addr)

	pubB64 := lecrypto.ExportPublicKeyBase64(pub)
	client, err := NewClient(addr, pubB64, WithInsecure(true))
	if err != nil {
		t.Fatal(err)
	}

	// Concurrent UpdateRelay calls should not race.
	var wg sync.WaitGroup
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_ = client.UpdateRelay("192.0.2.1", pubB64)
		}()
		wg.Add(1)
		go func() {
			defer wg.Done()
			_ = client.RelayDomain()
		}()
	}
	wg.Wait()
}

func TestClient_RelayDomain_AfterUpdate(t *testing.T) {
	pub, priv, err := ed25519.GenerateKey(nil)
	if err != nil {
		t.Fatal(err)
	}
	_ = priv

	addr, cleanup := startTestRelay(t, priv)
	defer cleanup()
	waitForRelay(t, addr)

	pubB64 := lecrypto.ExportPublicKeyBase64(pub)
	client, err := NewClient(addr, pubB64, WithInsecure(true))
	if err != nil {
		t.Fatal(err)
	}

	// Use an IP address to avoid DNS resolution in test environment.
	if err := client.UpdateRelay("192.0.2.1", pubB64); err != nil {
		t.Fatalf("UpdateRelay: %v", err)
	}
	if got := client.RelayDomain(); got != "192.0.2.1" {
		t.Errorf("RelayDomain() = %q, want %q", got, "192.0.2.1")
	}
}
