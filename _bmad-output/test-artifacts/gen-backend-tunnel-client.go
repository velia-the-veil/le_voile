package tunnel

import (
	"context"
	"crypto/ed25519"
	"crypto/tls"
	"encoding/base64"
	"encoding/json"
	"net"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	lecrypto "github.com/velia-the-veil/le_voile/internal/crypto"
	"github.com/quic-go/quic-go/http3"
)

func TestClient_ConcurrentConnect(t *testing.T) {
	pub, priv, err := lecrypto.GenerateKeyPair()
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}

	addr, cleanup := startTestRelay(t, priv)
	defer cleanup()

	pubB64 := lecrypto.ExportPublicKeyBase64(pub)
	client := newTestClient(t, addr, pubB64)
	defer client.Disconnect()

	// Launch multiple concurrent Connect calls.
	var wg sync.WaitGroup
	var successCount atomic.Int64
	var errorCount atomic.Int64
	const goroutines = 10

	for i := 0; i < goroutines; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			err := client.Connect(context.Background())
			if err != nil {
				errorCount.Add(1)
			} else {
				successCount.Add(1)
			}
		}()
	}

	wg.Wait()

	// At least one should succeed.
	if successCount.Load() == 0 {
		t.Error("expected at least one successful Connect, got none")
	}

	// Final state should be connected or disconnected (consistent).
	state := client.state.Get()
	if state != StateConnected && state != StateDisconnected {
		t.Errorf("final state = %q, want connected or disconnected", state)
	}
}

func TestClient_Connect_ContextCancelledBeforeVerify(t *testing.T) {
	pub, _, err := lecrypto.GenerateKeyPair()
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}

	pubB64 := lecrypto.ExportPublicKeyBase64(pub)
	// Point to unreachable address.
	client := newTestClient(t, "192.0.2.1:443", pubB64)

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // cancel immediately

	err = client.Connect(ctx)
	if err == nil {
		t.Fatal("expected error from cancelled context, got nil")
	}

	if client.state.Get() != StateDisconnected {
		t.Errorf("state = %q, want %q", client.state.Get(), StateDisconnected)
	}
}

func TestClient_VerifyRelay_InvalidSignatureBase64(t *testing.T) {
	// Set up an HTTP/1.1 test server that returns invalid base64 in signature.
	pub, _, err := lecrypto.GenerateKeyPair()
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}

	ts := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/verify" {
			resp := verifyResponse{Signature: "!!!not-valid-base64!!!"}
			json.NewEncoder(w).Encode(resp)
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	defer ts.Close()

	// Use the test server's TLS client.
	pubB64 := lecrypto.ExportPublicKeyBase64(pub)
	pubKey, _ := lecrypto.ImportPublicKeyBase64(pubB64)

	client := &Client{
		relayDomain: ts.Listener.Addr().String(),
		relayPubKey: pubKey,
		httpClient:  ts.Client(),
		state:       NewStateManager(),
	}

	err = client.Connect(context.Background())
	if err == nil {
		t.Fatal("expected error for invalid base64 signature, got nil")
	}

	if client.state.Get() != StateDisconnected {
		t.Errorf("state = %q after failed connect, want %q", client.state.Get(), StateDisconnected)
	}
}

func TestClient_VerifyRelay_WrongSignature(t *testing.T) {
	// Server signs with a different key than client expects.
	pub, _, err := lecrypto.GenerateKeyPair()
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}

	// Generate a separate signing key for the server.
	_, wrongPriv, err := lecrypto.GenerateKeyPair()
	if err != nil {
		t.Fatalf("generate wrong key: %v", err)
	}

	ts := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/verify" {
			var vReq verifyRequest
			json.NewDecoder(r.Body).Decode(&vReq)
			nonce, _ := base64.StdEncoding.DecodeString(vReq.Nonce)
			sig := ed25519.Sign(wrongPriv, nonce)
			resp := verifyResponse{Signature: base64.StdEncoding.EncodeToString(sig)}
			json.NewEncoder(w).Encode(resp)
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	defer ts.Close()

	pubB64 := lecrypto.ExportPublicKeyBase64(pub)
	pubKey, _ := lecrypto.ImportPublicKeyBase64(pubB64)

	client := &Client{
		relayDomain: ts.Listener.Addr().String(),
		relayPubKey: pubKey,
		httpClient:  ts.Client(),
		state:       NewStateManager(),
	}

	err = client.Connect(context.Background())
	if err != ErrVerificationFailed {
		t.Errorf("expected ErrVerificationFailed, got %v", err)
	}
}

func TestClient_VerifyRelay_ServerReturnsNon200(t *testing.T) {
	pub, _, err := lecrypto.GenerateKeyPair()
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}

	ts := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer ts.Close()

	pubB64 := lecrypto.ExportPublicKeyBase64(pub)
	pubKey, _ := lecrypto.ImportPublicKeyBase64(pubB64)

	client := &Client{
		relayDomain: ts.Listener.Addr().String(),
		relayPubKey: pubKey,
		httpClient:  ts.Client(),
		state:       NewStateManager(),
	}

	err = client.Connect(context.Background())
	if err == nil {
		t.Fatal("expected error for non-200 status, got nil")
	}
}

func TestClient_VerifyRelay_InvalidJSONResponse(t *testing.T) {
	pub, _, err := lecrypto.GenerateKeyPair()
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}

	ts := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("not-json"))
	}))
	defer ts.Close()

	pubB64 := lecrypto.ExportPublicKeyBase64(pub)
	pubKey, _ := lecrypto.ImportPublicKeyBase64(pubB64)

	client := &Client{
		relayDomain: ts.Listener.Addr().String(),
		relayPubKey: pubKey,
		httpClient:  ts.Client(),
		state:       NewStateManager(),
	}

	err = client.Connect(context.Background())
	if err == nil {
		t.Fatal("expected error for invalid JSON response, got nil")
	}
}

// freeTCPAddr finds a free TCP port. Used as helper for HTTP/1.1 test servers.
func freeTCPAddr(t *testing.T) string {
	t.Helper()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("find free port: %v", err)
	}
	addr := l.Addr().String()
	l.Close()
	return addr
}

func TestClient_ConnectTimeout_VeryShort(t *testing.T) {
	pub, priv, err := lecrypto.GenerateKeyPair()
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}

	// Start a slow relay that delays responding.
	ts := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(10 * time.Second) // way longer than connectTimeout
		w.WriteHeader(http.StatusOK)
	}))
	defer ts.Close()
	_ = priv // not used since we use httptest

	pubB64 := lecrypto.ExportPublicKeyBase64(pub)
	pubKey, _ := lecrypto.ImportPublicKeyBase64(pubB64)

	// Use a client with a short transport-level timeout.
	client := &Client{
		relayDomain: ts.Listener.Addr().String(),
		relayPubKey: pubKey,
		httpClient: &http.Client{
			Transport: &http.Transport{
				TLSClientConfig: &tls.Config{InsecureSkipVerify: true},
			},
			Timeout: 100 * time.Millisecond,
		},
		transport: &http3.Transport{},
		state:     NewStateManager(),
	}

	err = client.Connect(context.Background())
	if err == nil {
		t.Fatal("expected timeout error, got nil")
	}

	if client.state.Get() != StateDisconnected {
		t.Errorf("state = %q after timeout, want %q", client.state.Get(), StateDisconnected)
	}
}
