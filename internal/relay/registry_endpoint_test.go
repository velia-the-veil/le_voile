package relay

import (
	"context"
	"crypto/ed25519"
	"crypto/tls"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	lecrypto "github.com/velia-the-veil/le_voile/internal/crypto"
	"github.com/velia-the-veil/le_voile/internal/registry"
)

// testGoodIDPrefix / testBadIDPrefix — deterministic IDs for buildSignedRegistry.
// Keeping them package-level means assertions can re-derive expected values
// instead of duplicating string literals at each call site.
const (
	testGoodIDPrefix = "relay-test-"
	testBadIDPrefix  = "bad-test-"
)

// buildSignedRegistry creates a relay-registry.json document signed by a
// fresh master key and returns (master public key base64, raw bytes, good IDs, bad IDs).
// extraBadSig: if >0, appends that many entries with invalid signatures
// so the test can exercise the RejectLogger path.
func buildSignedRegistry(t *testing.T, goodCount, extraBadSig int) (masterPubB64 string, payload []byte, goodIDs, badIDs []string) {
	t.Helper()
	masterPub, masterPriv, err := ed25519.GenerateKey(nil)
	if err != nil {
		t.Fatalf("gen master key: %v", err)
	}

	type entry struct {
		ID        string    `json:"id"`
		Domain    string    `json:"domain"`
		PublicKey string    `json:"public_key"`
		Signature string    `json:"signature"`
		Added     time.Time `json:"added"`
	}

	now := time.Now().UTC().Truncate(time.Second)
	entries := make([]entry, 0, goodCount+extraBadSig)

	for i := 0; i < goodCount; i++ {
		relayPub, _, err := ed25519.GenerateKey(nil)
		if err != nil {
			t.Fatalf("gen relay key: %v", err)
		}
		id := fmt.Sprintf("%s%03d", testGoodIDPrefix, i)
		msg := append([]byte(registry.SignaturePrefix), relayPub...)
		sig := ed25519.Sign(masterPriv, msg)
		entries = append(entries, entry{
			ID:        id,
			Domain:    id + ".test.local",
			PublicKey: base64.StdEncoding.EncodeToString(relayPub),
			Signature: base64.StdEncoding.EncodeToString(sig),
			Added:     now,
		})
		goodIDs = append(goodIDs, id)
	}
	for i := 0; i < extraBadSig; i++ {
		relayPub, _, _ := ed25519.GenerateKey(nil)
		id := fmt.Sprintf("%s%03d", testBadIDPrefix, i)
		entries = append(entries, entry{
			ID:        id,
			Domain:    id + ".test.local",
			PublicKey: base64.StdEncoding.EncodeToString(relayPub),
			Signature: base64.StdEncoding.EncodeToString(make([]byte, 64)),
			Added:     now,
		})
		badIDs = append(badIDs, id)
	}

	doc := struct {
		Version         int       `json:"version"`
		MasterPublicKey string    `json:"master_public_key"`
		Relays          []entry   `json:"relays"`
		Updated         time.Time `json:"updated"`
	}{
		Version:         1,
		MasterPublicKey: base64.StdEncoding.EncodeToString(masterPub),
		Relays:          entries,
		Updated:         now,
	}
	payload, err = json.Marshal(doc)
	if err != nil {
		t.Fatalf("marshal registry: %v", err)
	}
	return base64.StdEncoding.EncodeToString(masterPub), payload, goodIDs, badIDs
}

// startRelayWithRegistry spawns a relay server that serves the given signed
// registry JSON. Returns the listen address and a cleanup func. CF validator
// is NOT configured (tests are on loopback).
func startRelayWithRegistry(t *testing.T, payload []byte) (addr string, cleanup func()) {
	t.Helper()

	_, tlsPriv, err := lecrypto.GenerateKeyPair()
	if err != nil {
		t.Fatalf("gen tls priv: %v", err)
	}
	certPEM, keyPEM, err := lecrypto.GenerateSelfSignedTLSCert(tlsPriv)
	if err != nil {
		t.Fatalf("gen tls cert: %v", err)
	}

	dir := t.TempDir()
	certPath := filepath.Join(dir, "cert.pem")
	keyPath := filepath.Join(dir, "key.pem")
	regPath := filepath.Join(dir, "relay-registry.json")
	if err := os.WriteFile(certPath, certPEM, 0600); err != nil {
		t.Fatalf("write cert: %v", err)
	}
	if err := os.WriteFile(keyPath, keyPEM, 0600); err != nil {
		t.Fatalf("write key: %v", err)
	}
	if err := os.WriteFile(regPath, payload, 0600); err != nil {
		t.Fatalf("write registry: %v", err)
	}

	addr = freeUDPAddr(t)
	srv := NewServer(addr, certPath, keyPath)
	srv.RegistryFile = regPath

	ctx, cancel := context.WithCancel(context.Background())
	go func() { _ = srv.ListenAndServe(ctx) }()

	waitForServer(t, addr)
	// Also poll TCP (listener starts in a sibling goroutine — tiny race).
	tcpClient := &http.Client{
		Transport: &http.Transport{TLSClientConfig: &tls.Config{InsecureSkipVerify: true}},
		Timeout:   1 * time.Second,
	}
	defer tcpClient.CloseIdleConnections()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		resp, err := tcpClient.Get("https://" + addr + "/health")
		if err == nil {
			io.Copy(io.Discard, resp.Body)
			resp.Body.Close()
			break
		}
		time.Sleep(50 * time.Millisecond)
	}

	return addr, func() {
		cancel()
		time.Sleep(100 * time.Millisecond)
	}
}

// TestRegistryEndpoint_HTTP3_Fetch covers Story 4.1 AC1+AC2: the relay serves
// /.well-known/relay-registry.json over HTTP/3 with Content-Type JSON, and a
// registry.Client can Fetch+VerifyAll the full relay list.
func TestRegistryEndpoint_HTTP3_Fetch(t *testing.T) {
	masterPubB64, payload, goodIDs, _ := buildSignedRegistry(t, 3, 0)
	addr, cleanup := startRelayWithRegistry(t, payload)
	defer cleanup()

	// Raw Content-Type assertion first (AC1).
	raw := newHTTP3TestClient()
	defer raw.CloseIdleConnections()
	resp, err := raw.Get("https://" + addr + "/.well-known/relay-registry.json")
	if err != nil {
		t.Fatalf("raw GET: %v", err)
	}
	defer resp.Body.Close()
	io.Copy(io.Discard, resp.Body)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status: got %d, want 200", resp.StatusCode)
	}
	if ct := resp.Header.Get("Content-Type"); !strings.HasPrefix(ct, "application/json") {
		t.Errorf("Content-Type: got %q, want application/json prefix", ct)
	}

	// Full registry.Client path via HTTP/3 transport (AC2).
	client, err := registry.NewClient(
		"https://"+addr,
		masterPubB64,
		registry.WithHTTPClient(newHTTP3TestClient()),
	)
	if err != nil {
		// Allow HTTP tests? No — we use https scheme with self-signed cert.
		t.Fatalf("new client: %v", err)
	}
	relays, err := client.Fetch(context.Background())
	if err != nil {
		t.Fatalf("fetch: %v", err)
	}
	if len(relays) != len(goodIDs) {
		t.Errorf("verified relays: got %d, want %d", len(relays), len(goodIDs))
	}
}

// TestRegistryEndpoint_TCP_Fetch covers AC4 for the TCP transport: the same
// endpoint must also be reachable via HTTP/1.1 or HTTP/2 over TCP so curl
// (smoke tests) and the non-HTTP/3 http.Client path both work.
func TestRegistryEndpoint_TCP_Fetch(t *testing.T) {
	masterPubB64, payload, goodIDs, _ := buildSignedRegistry(t, 2, 0)
	addr, cleanup := startRelayWithRegistry(t, payload)
	defer cleanup()

	tcpClient := &http.Client{
		Transport: &http.Transport{TLSClientConfig: &tls.Config{InsecureSkipVerify: true}},
		Timeout:   5 * time.Second,
	}
	defer tcpClient.CloseIdleConnections()

	resp, err := tcpClient.Get("https://" + addr + "/.well-known/relay-registry.json")
	if err != nil {
		t.Fatalf("tcp GET: %v", err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("tcp status: got %d, want 200", resp.StatusCode)
	}
	if ct := resp.Header.Get("Content-Type"); !strings.HasPrefix(ct, "application/json") {
		t.Errorf("tcp Content-Type: got %q, want application/json prefix", ct)
	}

	// Pipe the raw body through registry.Parse+VerifyAll to confirm the
	// byte stream is identical across transports.
	reg, err := registry.Parse(body)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	// Trust-anchor override (client would do this — we mirror here).
	reg.MasterPublicKey = masterPubB64
	verified, err := reg.VerifyAll()
	if err != nil {
		t.Fatalf("verify: %v", err)
	}
	if len(verified) != len(goodIDs) {
		t.Errorf("tcp verified: got %d, want %d", len(verified), len(goodIDs))
	}
}

// TestRegistryEndpoint_RejectLogger_Partial covers Story 4.1 AC3: when the
// registry contains a mix of valid + invalid entries, the client returns the
// valid ones AND the RejectLogger is called once per invalid entry with
// id/domain/reason.
func TestRegistryEndpoint_RejectLogger_Partial(t *testing.T) {
	masterPubB64, payload, goodIDs, badIDs := buildSignedRegistry(t, 2, 1) // 2 good, 1 bad-sig
	addr, cleanup := startRelayWithRegistry(t, payload)
	defer cleanup()

	type call struct {
		id, domain, reason string
	}
	var got []call
	logger := func(id, domain, reason string) {
		got = append(got, call{id, domain, reason})
	}

	client, err := registry.NewClient(
		"https://"+addr,
		masterPubB64,
		registry.WithHTTPClient(newHTTP3TestClient()),
		registry.WithRejectLogger(logger),
	)
	if err != nil {
		t.Fatalf("new client: %v", err)
	}
	relays, err := client.Fetch(context.Background())
	if err != nil {
		t.Fatalf("fetch: %v", err)
	}
	if len(relays) != len(goodIDs) {
		t.Errorf("relays: got %d, want %d", len(relays), len(goodIDs))
	}
	if len(got) != 1 {
		t.Fatalf("reject log calls: got %d, want 1 (%+v)", len(got), got)
	}
	// Sanity: the rejection carries id/domain/reason, never binary content.
	wantID := badIDs[0]
	if got[0].id != wantID {
		t.Errorf("reject id: got %q, want %q", got[0].id, wantID)
	}
	if got[0].domain != wantID+".test.local" {
		t.Errorf("reject domain: got %q, want %q", got[0].domain, wantID+".test.local")
	}
	if got[0].reason != registry.RejectReasonInvalidSignature {
		t.Errorf("reject reason: got %q, want %q", got[0].reason, registry.RejectReasonInvalidSignature)
	}
}

// TestRegistryEndpoint_RejectLogger_AllInvalid covers AC3's edge case: if every
// entry in the registry is corrupted, the client returns ErrNoValidRelays AND
// the logger is called for each rejection so operators can diagnose.
func TestRegistryEndpoint_RejectLogger_AllInvalid(t *testing.T) {
	masterPubB64, payload, _, _ := buildSignedRegistry(t, 0, 3) // only bad entries

	// buildSignedRegistry with goodCount=0 produces a doc with only invalid
	// signatures — but it still passes Parse (non-empty relays array).
	addr, cleanup := startRelayWithRegistry(t, payload)
	defer cleanup()

	var rejects int
	logger := func(_, _, _ string) { rejects++ }

	client, err := registry.NewClient(
		"https://"+addr,
		masterPubB64,
		registry.WithHTTPClient(newHTTP3TestClient()),
		registry.WithRejectLogger(logger),
	)
	if err != nil {
		t.Fatalf("new client: %v", err)
	}
	_, err = client.Fetch(context.Background())
	if err == nil {
		t.Fatal("expected error when all entries are invalid")
	}
	if rejects != 3 {
		t.Errorf("reject log calls: got %d, want 3", rejects)
	}
}

