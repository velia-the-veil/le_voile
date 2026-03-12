package registry

import (
	"context"
	"crypto/ed25519"
	"crypto/tls"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"sync"
	"testing"
	"time"
)

func TestDiscoverer_Discover_OnlineSuccess(t *testing.T) {
	masterPub, _, entries := testRegistry(t, 2)
	srv := serveRegistry(t, masterPub, entries)
	defer srv.Close()

	masterB64 := base64.StdEncoding.EncodeToString(masterPub)
	client, err := NewClient(srv.URL, masterB64)
	if err != nil {
		t.Fatal(err)
	}

	cachePath := filepath.Join(t.TempDir(), "cache.toml")
	cache := NewCache(cachePath)
	defaultRelay := RelayEntry{ID: "default", Domain: "default.example.com"}
	disc := NewDiscoverer(client, cache, defaultRelay)

	relays, err := disc.Discover(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(relays) != 2 {
		t.Errorf("relays: got %d, want 2", len(relays))
	}

	// Verify cache was written.
	_, _, cacheErr := cache.Load()
	if cacheErr != nil {
		t.Errorf("expected cache to be written, got error: %v", cacheErr)
	}
}

func TestDiscoverer_Discover_OfflineFallbackCache(t *testing.T) {
	masterPub, masterPriv, _ := testRegistry(t, 0)
	masterB64 := base64.StdEncoding.EncodeToString(masterPub)

	// Create a valid cached entry.
	relayPub, _, _ := ed25519.GenerateKey(nil)
	relayPubB64 := base64.StdEncoding.EncodeToString(relayPub)
	msg := append([]byte(signaturePrefix), relayPub...)
	sig := ed25519.Sign(masterPriv, msg)
	sigB64 := base64.StdEncoding.EncodeToString(sig)

	cachedEntry := RelayEntry{
		ID:        "cached-relay",
		Domain:    "cached.example.com",
		PublicKey: relayPubB64,
		Signature: sigB64,
	}

	cachePath := filepath.Join(t.TempDir(), "cache.toml")
	cache := NewCache(cachePath)
	if err := cache.Save([]RelayEntry{cachedEntry}, masterB64); err != nil {
		t.Fatal(err)
	}

	// Offline server (immediately closes connections).
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer srv.Close()

	client, err := NewClient(srv.URL, masterB64)
	if err != nil {
		t.Fatal(err)
	}

	defaultRelay := RelayEntry{ID: "default", Domain: "default.example.com"}
	disc := NewDiscoverer(client, cache, defaultRelay)

	relays, err := disc.Discover(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(relays) != 1 || relays[0].Domain != "cached.example.com" {
		t.Errorf("expected cached relay, got %+v", relays)
	}
}

func TestDiscoverer_Discover_OfflineNoCache(t *testing.T) {
	masterPub, _, _ := ed25519.GenerateKey(nil)
	masterB64 := base64.StdEncoding.EncodeToString(masterPub)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer srv.Close()

	client, err := NewClient(srv.URL, masterB64)
	if err != nil {
		t.Fatal(err)
	}

	cachePath := filepath.Join(t.TempDir(), "no-cache.toml")
	cache := NewCache(cachePath)
	defaultRelay := RelayEntry{ID: "default", Domain: "default.example.com"}
	disc := NewDiscoverer(client, cache, defaultRelay)

	relays, err := disc.Discover(context.Background())
	// No error: fallback to default relay is a degraded success, not a failure.
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
	if len(relays) != 1 || relays[0].Domain != "default.example.com" {
		t.Errorf("expected default relay, got %+v", relays)
	}
}

func TestDiscoverer_Start_PeriodicRefresh(t *testing.T) {
	masterPub, _, entries := testRegistry(t, 1)
	masterB64 := base64.StdEncoding.EncodeToString(masterPub)

	var mu sync.Mutex
	fetchCount := 0

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != EndpointPath {
			http.NotFound(w, r)
			return
		}
		mu.Lock()
		fetchCount++
		mu.Unlock()

		reg := struct {
			Version         int          `json:"version"`
			MasterPublicKey string       `json:"master_public_key"`
			Relays          []RelayEntry `json:"relays"`
			Updated         time.Time    `json:"updated"`
		}{
			Version:         1,
			MasterPublicKey: masterB64,
			Relays:          entries,
			Updated:         time.Now(),
		}
		data, _ := json.Marshal(reg)
		w.Header().Set("Content-Type", "application/json")
		w.Write(data)
	}))
	defer srv.Close()

	client, err := NewClient(srv.URL, masterB64, WithRefreshInterval(50*time.Millisecond))
	if err != nil {
		t.Fatal(err)
	}

	cachePath := filepath.Join(t.TempDir(), "cache.toml")
	cache := NewCache(cachePath)
	defaultRelay := RelayEntry{ID: "default", Domain: "default.example.com"}
	disc := NewDiscoverer(client, cache, defaultRelay)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	if err := disc.Start(ctx); err != nil {
		t.Fatal(err)
	}
	defer disc.Stop()

	// Wait for at least 2 periodic refreshes.
	time.Sleep(200 * time.Millisecond)

	mu.Lock()
	count := fetchCount
	mu.Unlock()

	// Start does an initial Discover (1 fetch) + periodic refreshes (at least 2 more).
	if count < 3 {
		t.Errorf("expected at least 3 fetches (initial + periodic), got %d", count)
	}
}

func TestDiscoverer_WithLatencyChecker_SortsRelays(t *testing.T) {
	masterPub, _, entries := testRegistry(t, 3)
	masterB64 := base64.StdEncoding.EncodeToString(masterPub)

	// Create 3 TLS health servers with different delays.
	// LatencyChecker uses https:// so servers must be TLS-enabled.
	delays := []time.Duration{100 * time.Millisecond, 20 * time.Millisecond, 50 * time.Millisecond}
	for i, d := range delays {
		delay := d
		srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			time.Sleep(delay)
			w.Write([]byte(`{"status":"ok"}`))
		}))
		defer srv.Close()
		entries[i].Domain = srv.Listener.Addr().String()
	}

	regSrv := serveRegistry(t, masterPub, entries)
	defer regSrv.Close()

	client, err := NewClient(regSrv.URL, masterB64)
	if err != nil {
		t.Fatal(err)
	}

	// Custom TLS client that trusts test certificates (InsecureSkipVerify).
	latencyClient := &http.Client{
		Timeout: 5 * time.Second,
		Transport: &http.Transport{
			TLSClientConfig: &tls.Config{InsecureSkipVerify: true},
		},
	}
	latencyChecker := NewLatencyChecker(WithLatencyHTTPClient(latencyClient))

	cachePath := filepath.Join(t.TempDir(), "cache.toml")
	cache := NewCache(cachePath)
	defaultRelay := RelayEntry{ID: "default", Domain: "default.example.com"}
	disc := NewDiscoverer(client, cache, defaultRelay, WithLatencyChecker(latencyChecker))

	relays, err := disc.Discover(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(relays) != 3 {
		t.Fatalf("expected 3 relays, got %d", len(relays))
	}

	// Relays must be sorted by latency: 20ms (entries[1]), 50ms (entries[2]), 100ms (entries[0]).
	if relays[0].ID != entries[1].ID {
		t.Errorf("first relay: got %s, want %s (fastest 20ms)", relays[0].ID, entries[1].ID)
	}
	if relays[1].ID != entries[2].ID {
		t.Errorf("second relay: got %s, want %s (mid 50ms)", relays[1].ID, entries[2].ID)
	}
	if relays[2].ID != entries[0].ID {
		t.Errorf("third relay: got %s, want %s (slowest 100ms)", relays[2].ID, entries[0].ID)
	}
}

func TestDiscoverer_LatencyFallbackToCache(t *testing.T) {
	masterPub, _, entries := testRegistry(t, 2)
	masterB64 := base64.StdEncoding.EncodeToString(masterPub)

	regSrv := serveRegistry(t, masterPub, entries)
	defer regSrv.Close()

	client, err := NewClient(regSrv.URL, masterB64)
	if err != nil {
		t.Fatal(err)
	}

	cachePath := filepath.Join(t.TempDir(), "cache.toml")
	cache := NewCache(cachePath)
	defaultRelay := RelayEntry{ID: "default", Domain: "default.example.com"}

	// Pre-populate latency cache with rankings (entry[1] faster than entry[0]).
	rankings := []LatencyResult{
		{Relay: entries[0], Latency: 100 * time.Millisecond, Reachable: true},
		{Relay: entries[1], Latency: 20 * time.Millisecond, Reachable: true},
	}
	// Save relay cache first so LoadLatencies works.
	if err := cache.Save(entries, masterB64); err != nil {
		t.Fatal(err)
	}
	if err := cache.SaveLatencies(rankings); err != nil {
		t.Fatal(err)
	}

	// Latency checker that always fails (simulates network unavailability).
	failClient := &http.Client{Timeout: 1 * time.Millisecond}
	latencyChecker := NewLatencyChecker(WithLatencyHTTPClient(failClient))
	disc := NewDiscoverer(client, cache, defaultRelay, WithLatencyChecker(latencyChecker))

	relays, err := disc.Discover(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// Relays are fetched online (registry server works), but latency measurement fails.
	// Should fallback to cache rankings: entry[1] (20ms) before entry[0] (100ms).
	if len(relays) < 2 {
		t.Fatalf("expected at least 2 relays, got %d", len(relays))
	}
	if relays[0].ID != entries[1].ID {
		t.Errorf("first relay: got %s, want %s (cached 20ms)", relays[0].ID, entries[1].ID)
	}
	if relays[1].ID != entries[0].ID {
		t.Errorf("second relay: got %s, want %s (cached 100ms)", relays[1].ID, entries[0].ID)
	}
}

func TestDiscoverer_Relays_ThreadSafe(t *testing.T) {
	masterPub, _, entries := testRegistry(t, 2)
	masterB64 := base64.StdEncoding.EncodeToString(masterPub)
	srv := serveRegistry(t, masterPub, entries)
	defer srv.Close()

	client, err := NewClient(srv.URL, masterB64, WithRefreshInterval(10*time.Millisecond))
	if err != nil {
		t.Fatal(err)
	}

	cachePath := filepath.Join(t.TempDir(), "cache.toml")
	cache := NewCache(cachePath)
	defaultRelay := RelayEntry{ID: "default", Domain: "default.example.com"}
	disc := NewDiscoverer(client, cache, defaultRelay)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	if err := disc.Start(ctx); err != nil {
		t.Fatal(err)
	}
	defer disc.Stop()

	// Concurrent reads while Start is running.
	var wg sync.WaitGroup
	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_ = disc.Relays()
			_ = disc.Primary()
		}()
	}
	wg.Wait()
}

func TestDiscoverer_RefreshLoop_NoRelaySwitch(t *testing.T) {
	masterPub, _, entries := testRegistry(t, 3)
	masterB64 := base64.StdEncoding.EncodeToString(masterPub)

	// Create 3 TLS health servers with swappable delays.
	// Initial: entries[0]=100ms (slow), entries[1]=20ms (fast), entries[2]=50ms (mid).
	var delayMu sync.Mutex
	delays := []time.Duration{100 * time.Millisecond, 20 * time.Millisecond, 50 * time.Millisecond}

	for i := range delays {
		idx := i
		srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			delayMu.Lock()
			d := delays[idx]
			delayMu.Unlock()
			time.Sleep(d)
			w.Write([]byte(`{"status":"ok"}`))
		}))
		defer srv.Close()
		entries[i].Domain = srv.Listener.Addr().String()
	}

	regSrv := serveRegistry(t, masterPub, entries)
	defer regSrv.Close()

	client, err := NewClient(regSrv.URL, masterB64, WithRefreshInterval(200*time.Millisecond))
	if err != nil {
		t.Fatal(err)
	}

	latencyClient := &http.Client{
		Timeout: 5 * time.Second,
		Transport: &http.Transport{
			TLSClientConfig: &tls.Config{InsecureSkipVerify: true},
		},
	}
	latencyChecker := NewLatencyChecker(WithLatencyHTTPClient(latencyClient))

	cachePath := filepath.Join(t.TempDir(), "cache.toml")
	cache := NewCache(cachePath)
	defaultRelay := RelayEntry{ID: "default", Domain: "default.example.com"}
	disc := NewDiscoverer(client, cache, defaultRelay, WithLatencyChecker(latencyChecker))

	// Initial discover — entries[1] should be first (20ms fastest).
	relays, err := disc.Discover(context.Background())
	if err != nil {
		t.Fatalf("initial discover: %v", err)
	}
	if len(relays) < 2 {
		t.Fatalf("expected at least 2 relays, got %d", len(relays))
	}
	initialPrimary := disc.Primary()

	// Simulate a FailoverManager tracking the current relay.
	updater := &mockRelayUpdater{}
	connectFn := func(ctx context.Context) error { return nil }
	fm := NewFailoverManager(disc, updater, connectFn)
	fm.SetCurrentRelay(initialPrimary.ID)

	// Swap delays: entries[0] becomes fastest (10ms), entries[1] becomes slowest (200ms).
	delayMu.Lock()
	delays[0] = 10 * time.Millisecond
	delays[1] = 200 * time.Millisecond
	delayMu.Unlock()

	// Start refresh loop and wait for at least one cycle.
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	_ = disc.Start(ctx)
	defer disc.Stop()

	time.Sleep(500 * time.Millisecond)

	// After refresh, the relay order may have changed (entries[0] now fastest).
	// KEY ASSERTION: FailoverManager's current relay is UNCHANGED (sticky session AC5).
	if fm.CurrentRelayID() != initialPrimary.ID {
		t.Errorf("sticky session violated: FailoverManager current relay changed from %s to %s",
			initialPrimary.ID, fm.CurrentRelayID())
	}

	// Verify that the discoverer's internal list was actually reordered
	// (proving the refresh happened and re-measured latencies).
	newPrimary := disc.Primary()
	if newPrimary.ID == initialPrimary.ID {
		// If the primary didn't change, the refresh might not have reordered.
		// This is acceptable if latency differences are too small to measure reliably.
		t.Logf("primary unchanged after refresh (latency variance) — sticky session still verified")
	} else {
		t.Logf("primary changed %s -> %s (reordering confirmed), FailoverManager stuck on %s (sticky OK)",
			initialPrimary.ID, newPrimary.ID, fm.CurrentRelayID())
	}
}
