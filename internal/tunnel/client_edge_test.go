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
		relayIP:     ts.Listener.Addr().String(),
		relayPubKey: pubKey,
		httpClient:  ts.Client(),
		insecure:    true,
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

	// Start a slow relay that blocks until explicitly released.
	// httptest.Server.Close() calls wg.Wait() before CloseClientConnections(),
	// so we must unblock the handler ourselves via the done channel.
	done := make(chan struct{})
	ts := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		<-done
	}))
	defer func() {
		close(done)
		ts.Close()
	}()
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
		insecure:  true,
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

// TestClient_Disconnect_ThenReconnect verifies that after Disconnect(),
// the transport is recreated and Connect() can succeed again.
// This was the root cause of tunnel drops: Disconnect() permanently
// closed the transport, making the Reconnector loop on dead transport errors.
func TestClient_Disconnect_ThenReconnect(t *testing.T) {
	pub, priv, err := lecrypto.GenerateKeyPair()
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}

	addr, cleanup := startTestRelay(t, priv)
	defer cleanup()

	pubB64 := lecrypto.ExportPublicKeyBase64(pub)
	client := newTestClient(t, addr, pubB64)

	// First connect
	if err := client.Connect(context.Background()); err != nil {
		t.Fatalf("first Connect: %v", err)
	}
	if client.state.Get() != StateConnected {
		t.Fatalf("state after first connect = %q, want connected", client.state.Get())
	}

	// Disconnect (previously this would permanently kill the transport)
	if err := client.Disconnect(); err != nil {
		t.Fatalf("Disconnect: %v", err)
	}
	if client.state.Get() != StateDisconnected {
		t.Fatalf("state after disconnect = %q, want disconnected", client.state.Get())
	}

	// Reconnect — this MUST succeed with the fix
	if err := client.Connect(context.Background()); err != nil {
		t.Fatalf("second Connect after Disconnect: %v (transport was not recreated)", err)
	}
	if client.state.Get() != StateConnected {
		t.Errorf("state after reconnect = %q, want connected", client.state.Get())
	}

	// Verify DoH still works after reconnect
	dnsPayload := []byte{
		0x00, 0x01, 0x01, 0x00, 0x00, 0x01, 0x00, 0x00,
		0x00, 0x00, 0x00, 0x00, 0x07, 'e', 'x', 'a',
		'm', 'p', 'l', 'e', 0x03, 'c', 'o', 'm',
		0x00, 0x00, 0x01, 0x00, 0x01,
	}
	resp, err := client.SendDoHQuery(context.Background(), dnsPayload)
	if err != nil {
		t.Fatalf("SendDoHQuery after reconnect: %v", err)
	}
	if len(resp) == 0 {
		t.Error("expected non-empty DNS response after reconnect")
	}

	client.Disconnect()
}

// TestClient_AutoDisconnect_OnConsecutiveDoHFailures verifies that after
// maxConsecutiveDoHFailures transport errors, the client auto-transitions
// to StateDisconnected. This triggers the Reconnector.
func TestClient_AutoDisconnect_OnConsecutiveDoHFailures(t *testing.T) {
	pub, _, err := lecrypto.GenerateKeyPair()
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}
	pubB64 := lecrypto.ExportPublicKeyBase64(pub)
	pubKey, _ := lecrypto.ImportPublicKeyBase64(pubB64)

	// Create a client pointing at a dead address so DoH requests fail.
	client := &Client{
		relayDomain: "127.0.0.1",
		relayIP:     "127.0.0.1",
		relayPubKey: pubKey,
		httpClient:  &http.Client{Timeout: 100 * time.Millisecond},
		transport:   &http3.Transport{},
		insecure:    true,
		state:       NewStateManager(),
	}
	// Force state to Connected so SendDoHQuery doesn't short-circuit.
	client.state.Set(StateConnected)

	// Drain the state update for the Connected transition.
	<-client.state.Updates()

	dnsPayload := []byte{
		0x00, 0x01, 0x01, 0x00, 0x00, 0x01, 0x00, 0x00,
		0x00, 0x00, 0x00, 0x00, 0x07, 'e', 'x', 'a',
		'm', 'p', 'l', 'e', 0x03, 'c', 'o', 'm',
		0x00, 0x00, 0x01, 0x00, 0x01,
	}

	// First N-1 failures should NOT trigger disconnect.
	for i := 0; i < maxConsecutiveDoHFailures-1; i++ {
		_, err := client.SendDoHQuery(context.Background(), dnsPayload)
		if err == nil {
			t.Fatalf("expected error on failure %d", i+1)
		}
		if client.state.Get() != StateConnected {
			t.Fatalf("state changed to %q after only %d failures, want connected until %d",
				client.state.Get(), i+1, maxConsecutiveDoHFailures)
		}
	}

	// The Nth failure should trigger auto-disconnect.
	_, err = client.SendDoHQuery(context.Background(), dnsPayload)
	if err == nil {
		t.Fatal("expected error on final failure")
	}
	if client.state.Get() != StateDisconnected {
		t.Errorf("state = %q after %d consecutive failures, want disconnected",
			client.state.Get(), maxConsecutiveDoHFailures)
	}

	// Verify the StateDisconnected was sent to the updates channel.
	select {
	case state := <-client.state.Updates():
		if state != StateDisconnected {
			t.Errorf("update channel received %q, want disconnected", state)
		}
	default:
		t.Error("expected StateDisconnected on updates channel")
	}
}

// TestClient_DoHFailureCounter_ResetsOnSuccess verifies that a successful
// DoH query resets the consecutive failure counter.
func TestClient_DoHFailureCounter_ResetsOnSuccess(t *testing.T) {
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

	// Simulate N-1 failures by directly calling recordDoHFailure.
	for i := 0; i < maxConsecutiveDoHFailures-1; i++ {
		client.recordDoHFailure()
	}

	// A successful DoH query should reset the counter.
	dnsPayload := []byte{
		0x00, 0x01, 0x01, 0x00, 0x00, 0x01, 0x00, 0x00,
		0x00, 0x00, 0x00, 0x00, 0x07, 'e', 'x', 'a',
		'm', 'p', 'l', 'e', 0x03, 'c', 'o', 'm',
		0x00, 0x00, 0x01, 0x00, 0x01,
	}
	if _, err := client.SendDoHQuery(context.Background(), dnsPayload); err != nil {
		t.Fatalf("SendDoHQuery: %v", err)
	}

	// After reset, N-1 more failures should NOT trigger disconnect.
	for i := 0; i < maxConsecutiveDoHFailures-1; i++ {
		client.recordDoHFailure()
	}

	if client.state.Get() != StateConnected {
		t.Errorf("state = %q, want connected (counter should have reset)", client.state.Get())
	}
}
