package registry

import (
	"context"
	"crypto/ed25519"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"golang.org/x/net/dns/dnsmessage"
)

func serveRegistry(t *testing.T, masterPub ed25519.PublicKey, relays []RelayEntry) *httptest.Server {
	t.Helper()
	reg := struct {
		Version         int          `json:"version"`
		MasterPublicKey string       `json:"master_public_key"`
		Relays          []RelayEntry `json:"relays"`
		Updated         time.Time    `json:"updated"`
	}{
		Version:         1,
		MasterPublicKey: base64.StdEncoding.EncodeToString(masterPub),
		Relays:          relays,
		Updated:         time.Now(),
	}
	data, err := json.Marshal(reg)
	if err != nil {
		t.Fatal(err)
	}

	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != EndpointPath {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.Write(data)
	}))
}

func TestClient_Fetch_Success(t *testing.T) {
	masterPub, _, entries := testRegistry(t, 2)
	srv := serveRegistry(t, masterPub, entries)
	defer srv.Close()

	masterB64 := base64.StdEncoding.EncodeToString(masterPub)
	client, err := NewClient(srv.URL, masterB64, withAllowHTTP)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	relays, err := client.Fetch(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(relays) != 2 {
		t.Errorf("relays: got %d, want 2", len(relays))
	}
}

func TestClient_Fetch_HTTPError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()

	masterPub, _, _ := ed25519.GenerateKey(nil)
	masterB64 := base64.StdEncoding.EncodeToString(masterPub)
	client, err := NewClient(srv.URL, masterB64, withAllowHTTP)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	_, err = client.Fetch(context.Background())
	if err == nil {
		t.Error("expected error for HTTP 500")
	}
}

func TestClient_Fetch_InvalidSignature(t *testing.T) {
	masterPub, _, _ := ed25519.GenerateKey(nil)

	// Create entries signed with a different key.
	_, _, entries := testRegistry(t, 2)
	srv := serveRegistry(t, masterPub, entries) // entries are signed by testRegistry's key, not masterPub
	defer srv.Close()

	masterB64 := base64.StdEncoding.EncodeToString(masterPub)
	client, err := NewClient(srv.URL, masterB64, withAllowHTTP)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	_, err = client.Fetch(context.Background())
	if err == nil {
		t.Error("expected error for invalid signature")
	}
	if !errors.Is(err, ErrNoValidRelays) {
		t.Errorf("expected ErrNoValidRelays in chain, got %v", err)
	}
}

// TestClient_Fetch_RejectLoggerCalled covers AC3 of Story 4.1 at the client
// boundary: when the registry mixes valid + invalid entries, Fetch returns
// the valid ones AND invokes the RejectLogger for the invalid ones with
// id/domain/reason — and only id/domain/reason.
func TestClient_Fetch_RejectLoggerCalled(t *testing.T) {
	masterPub, _, entries := testRegistry(t, 2)

	// One extra entry with a bad signature (cannot be verified by masterPub).
	badEntry := RelayEntry{
		ID:        "bad-relay",
		Domain:    "bad-relay.example.com",
		PublicKey: entries[0].PublicKey,
		Signature: base64.StdEncoding.EncodeToString(make([]byte, 64)),
	}

	srv := serveRegistry(t, masterPub, append(entries, badEntry))
	defer srv.Close()

	type call struct {
		id, domain, reason string
	}
	var got []call
	logger := func(id, domain, reason string) {
		got = append(got, call{id, domain, reason})
	}

	masterB64 := base64.StdEncoding.EncodeToString(masterPub)
	client, err := NewClient(srv.URL, masterB64, WithRejectLogger(logger), withAllowHTTP)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	relays, err := client.Fetch(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(relays) != 2 {
		t.Errorf("relays: got %d, want 2", len(relays))
	}
	if len(got) != 1 {
		t.Fatalf("logger calls: got %d, want 1 (%+v)", len(got), got)
	}
	if got[0].id != "bad-relay" || got[0].domain != "bad-relay.example.com" {
		t.Errorf("logger payload: got %+v, want id=bad-relay domain=bad-relay.example.com", got[0])
	}
	if got[0].reason != RejectReasonInvalidSignature {
		t.Errorf("logger reason: got %q, want %q", got[0].reason, RejectReasonInvalidSignature)
	}
}

// TestClient_Fetch_ViaDoHResolver covers the NFR9i bootstrap path: when a
// DoHResolver is injected via WithResolver, the Client's underlying transport
// resolves the registry hostname through DoH before dialing. The test uses
// withDoHAllowPrivate so the DoH mock can return 127.0.0.1 (loopback test
// server); production resolvers reject private addresses.
func TestClient_Fetch_ViaDoHResolver(t *testing.T) {
	masterPub, _, entries := testRegistry(t, 2)

	regSrv := serveRegistry(t, masterPub, entries)
	defer regSrv.Close()

	// Extract the port the registry test server is listening on so the DoH
	// mock can direct resolution at it.
	regURL, err := url.Parse(regSrv.URL)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	regPort := regURL.Port()

	// DoH mock always answers 127.0.0.1 for any A query. Must be HTTPS so
	// NewDoHResolver accepts the upstream URL (production invariant).
	dohSrv := newDoHServer(t, func(host string, qtype dnsmessage.Type) (int, []byte) {
		return http.StatusOK, buildDoHResponse(t, host, qtype, "127.0.0.1")
	})
	defer dohSrv.Close()

	doh, err := NewDoHResolver(
		WithDoHUpstreams([]string{dohSrv.URL}),
		WithDoHHTTPClient(dohSrv.Client()),
		withDoHAllowPrivate,
	)
	if err != nil {
		t.Fatalf("NewDoHResolver: %v", err)
	}

	// Build a registry URL with a synthetic hostname (NOT 127.0.0.1) so the
	// transport MUST pass through the DoH resolver to find the IP.
	clientURL := "http://fake-registry.invalid:" + regPort
	masterB64 := base64.StdEncoding.EncodeToString(masterPub)
	client, err := NewClient(
		clientURL,
		masterB64,
		WithResolver(doh),
		withAllowHTTP,
	)
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}

	relays, err := client.Fetch(context.Background())
	if err != nil {
		t.Fatalf("Fetch via DoH: %v", err)
	}
	if len(relays) != 2 {
		t.Errorf("relays: got %d, want 2", len(relays))
	}
}

// TestClient_Fetch_ViaDoHResolver_ResolverDown verifies that when DoH cannot
// resolve, the Client surfaces the failure explicitly and does NOT silently
// fall back to the system resolver (AC3, NFR9i).
func TestClient_Fetch_ViaDoHResolver_ResolverDown(t *testing.T) {
	// Dead DoH upstream (non-routable HTTPS URL).
	doh, err := NewDoHResolver(
		WithDoHUpstreams([]string{"https://127.0.0.1:1"}),
		WithDoHTimeout(100*time.Millisecond),
	)
	if err != nil {
		t.Fatalf("NewDoHResolver: %v", err)
	}

	masterPub, _, _ := ed25519.GenerateKey(nil)
	masterB64 := base64.StdEncoding.EncodeToString(masterPub)
	client, err := NewClient(
		"http://example-relay.invalid/",
		masterB64,
		WithResolver(doh),
		withAllowHTTP,
	)
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}

	_, err = client.Fetch(context.Background())
	if err == nil {
		t.Fatal("expected DoH resolution failure to surface from Fetch")
	}
	if !errors.Is(err, ErrAllDoHUpstreamsDown) {
		t.Errorf("err chain missing ErrAllDoHUpstreamsDown: %v", err)
	}
}

// Fix H1: combining WithHTTPClient and WithResolver must fail loudly.
func TestNewClient_ResolverAndHTTPClientMutex(t *testing.T) {
	doh, err := NewDoHResolver(WithDoHUpstreams([]string{"https://example.com/dns"}))
	if err != nil {
		t.Fatalf("NewDoHResolver: %v", err)
	}
	masterPub, _, _ := ed25519.GenerateKey(nil)
	masterB64 := base64.StdEncoding.EncodeToString(masterPub)
	_, err = NewClient(
		"https://relay.example.com",
		masterB64,
		WithHTTPClient(&http.Client{}),
		WithResolver(doh),
	)
	if err == nil {
		t.Fatal("expected error when combining WithHTTPClient and WithResolver")
	}
	if !strings.Contains(err.Error(), "mutually exclusive") {
		t.Errorf("err = %v, want mutually exclusive message", err)
	}
}

// Fix M1: the Client's overall timeout must accommodate N upstreams × per-upstream
// timeout + headroom for the actual HTTP fetch. Verifies via the composed
// httpClient.Timeout that the expected budget is applied.
func TestNewClient_DoHTimeoutBudget(t *testing.T) {
	doh, err := NewDoHResolver(
		WithDoHUpstreams([]string{"https://a/", "https://b/", "https://c/"}),
		WithDoHTimeout(3*time.Second),
	)
	if err != nil {
		t.Fatalf("NewDoHResolver: %v", err)
	}
	masterPub, _, _ := ed25519.GenerateKey(nil)
	masterB64 := base64.StdEncoding.EncodeToString(masterPub)
	client, err := NewClient(
		"https://relay.example.com",
		masterB64,
		WithResolver(doh),
	)
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}
	want := 3*time.Second*3 + 10*time.Second // 3 upstreams × 3s + 10s headroom
	if got := client.httpClient.Timeout; got != want {
		t.Errorf("httpClient.Timeout = %v, want %v", got, want)
	}
}

// TestClient_Fetch_NoResolver_KeepsDefaultClient verifies that if WithResolver
// is not provided, the Client preserves the default http.Client and is not
// accidentally wired through any DoH transport (backward compatibility).
func TestClient_Fetch_NoResolver_KeepsDefaultClient(t *testing.T) {
	masterPub, _, entries := testRegistry(t, 1)
	srv := serveRegistry(t, masterPub, entries)
	defer srv.Close()

	masterB64 := base64.StdEncoding.EncodeToString(masterPub)
	client, err := NewClient(srv.URL, masterB64, withAllowHTTP)
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}
	if client.httpClient == nil {
		t.Fatal("httpClient must not be nil")
	}

	if _, err := client.Fetch(context.Background()); err != nil {
		t.Errorf("Fetch: %v", err)
	}
}

func TestClient_Fetch_Timeout(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(2 * time.Second)
	}))
	defer srv.Close()

	masterPub, _, _ := ed25519.GenerateKey(nil)
	masterB64 := base64.StdEncoding.EncodeToString(masterPub)

	httpClient := &http.Client{Timeout: 50 * time.Millisecond}
	client, err := NewClient(srv.URL, masterB64, WithHTTPClient(httpClient), withAllowHTTP)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	_, err = client.Fetch(context.Background())
	if err == nil {
		t.Error("expected timeout error")
	}
}
