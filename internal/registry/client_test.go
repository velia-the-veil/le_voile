package registry

import (
	"context"
	"crypto/ed25519"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
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
	client, err := NewClient(srv.URL, masterB64)
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
	client, err := NewClient(srv.URL, masterB64)
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
	client, err := NewClient(srv.URL, masterB64)
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

func TestClient_Fetch_Timeout(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(2 * time.Second)
	}))
	defer srv.Close()

	masterPub, _, _ := ed25519.GenerateKey(nil)
	masterB64 := base64.StdEncoding.EncodeToString(masterPub)

	httpClient := &http.Client{Timeout: 50 * time.Millisecond}
	client, err := NewClient(srv.URL, masterB64, WithHTTPClient(httpClient))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	_, err = client.Fetch(context.Background())
	if err == nil {
		t.Error("expected timeout error")
	}
}
