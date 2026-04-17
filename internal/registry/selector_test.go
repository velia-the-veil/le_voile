package registry

import (
	"context"
	"crypto/ed25519"
	"encoding/base64"
	"errors"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// newTestDiscoverer returns a Discoverer pre-populated with the given relays
// and no online client (for selector-only unit tests).
func newTestDiscoverer(t *testing.T, relays []RelayEntry) *Discoverer {
	t.Helper()
	cachePath := filepath.Join(t.TempDir(), "cache.toml")
	cache := NewCache(cachePath)
	// A nil Client would crash RefreshInterval(), but SelectRelay never calls
	// the client. We use a minimal placeholder via NewDiscoverer with a cache
	// path and the default relay derived from the first input.
	defaultRelay := RelayEntry{ID: "default", Domain: "default.example.com"}
	disc := &Discoverer{
		cache:        cache,
		defaultRelay: defaultRelay,
		selector:     newCountrySelector(),
		relays:       relays,
		stopCh:       make(chan struct{}),
	}
	return disc
}

func mustRelay(id, domain string) RelayEntry {
	return RelayEntry{ID: id, Domain: domain}
}

func TestSelectRelay_RoundRobinTwoRelays(t *testing.T) {
	relays := []RelayEntry{
		mustRelay("relay-de-001", "de-001.levoile.dev"),
		mustRelay("relay-de-002", "de-002.levoile.dev"),
	}
	disc := newTestDiscoverer(t, relays)

	want := []string{
		"relay-de-001",
		"relay-de-002",
		"relay-de-001",
		"relay-de-002",
	}
	for i, w := range want {
		got, err := disc.SelectRelay("de")
		if err != nil {
			t.Fatalf("iter %d: unexpected error: %v", i, err)
		}
		if got.ID != w {
			t.Errorf("iter %d: got %q, want %q", i, got.ID, w)
		}
	}
}

func TestSelectRelay_UnknownCountry(t *testing.T) {
	disc := newTestDiscoverer(t, []RelayEntry{
		mustRelay("relay-de-001", "de-001.levoile.dev"),
	})
	_, err := disc.SelectRelay("xx")
	if !errors.Is(err, ErrUnknownCountry) {
		t.Fatalf("err = %v, want ErrUnknownCountry", err)
	}
}

func TestSelectRelay_NoRelaysForCountry(t *testing.T) {
	disc := newTestDiscoverer(t, []RelayEntry{
		mustRelay("relay-de-001", "de-001.levoile.dev"),
	})
	// "fr" is in CountryMetaMap but has no relay in the pool.
	_, err := disc.SelectRelay("fr")
	if !errors.Is(err, ErrNoRelaysForCountry) {
		t.Fatalf("err = %v, want ErrNoRelaysForCountry", err)
	}
}

func TestSelectRelay_ResetOnPoolChange(t *testing.T) {
	relays := []RelayEntry{
		mustRelay("relay-de-001", "de-001.levoile.dev"),
		mustRelay("relay-de-002", "de-002.levoile.dev"),
	}
	disc := newTestDiscoverer(t, relays)

	// Advance cursor to index 1.
	got, err := disc.SelectRelay("de")
	if err != nil || got.ID != "relay-de-001" {
		t.Fatalf("first pick: got %q err %v", got.ID, err)
	}

	// Swap the pool order (simulates a latency re-sort).
	disc.setRelays([]RelayEntry{
		mustRelay("relay-de-002", "de-002.levoile.dev"),
		mustRelay("relay-de-001", "de-001.levoile.dev"),
	})

	// Cursor must reset — next pick is index 0 of the new pool.
	got, err = disc.SelectRelay("de")
	if err != nil {
		t.Fatalf("second pick: %v", err)
	}
	if got.ID != "relay-de-002" {
		t.Errorf("after pool change: got %q, want %q", got.ID, "relay-de-002")
	}
}

func TestSelectRelay_PerCountryIndependentCursors(t *testing.T) {
	relays := []RelayEntry{
		mustRelay("relay-de-001", "de-001.levoile.dev"),
		mustRelay("relay-de-002", "de-002.levoile.dev"),
		mustRelay("relay-us-001", "us-001.levoile.dev"),
		mustRelay("relay-us-002", "us-002.levoile.dev"),
	}
	disc := newTestDiscoverer(t, relays)

	// Advance "de" to index 1 then back to 0.
	for i := 0; i < 2; i++ {
		_, _ = disc.SelectRelay("de")
	}
	// "us" cursor must still be at 0.
	got, err := disc.SelectRelay("us")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got.ID != "relay-us-001" {
		t.Errorf("us first pick: got %q, want relay-us-001", got.ID)
	}
}

func TestSameIDs_EdgeCases(t *testing.T) {
	cases := []struct {
		name string
		a, b []string
		want bool
	}{
		{"both nil", nil, nil, true},
		{"nil vs empty", nil, []string{}, true},
		{"nil vs non-empty", nil, []string{"x"}, false},
		{"same order", []string{"a", "b"}, []string{"a", "b"}, true},
		{"different order", []string{"a", "b"}, []string{"b", "a"}, false},
		{"different length shrink", []string{"a", "b"}, []string{"a"}, false},
		{"different length grow", []string{"a"}, []string{"a", "b"}, false},
		{"different content", []string{"a", "b"}, []string{"a", "c"}, false},
		{"single same", []string{"x"}, []string{"x"}, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := sameIDs(tc.a, tc.b); got != tc.want {
				t.Errorf("sameIDs(%v, %v) = %v, want %v", tc.a, tc.b, got, tc.want)
			}
		})
	}
}

func TestTriggerBackgroundDiscover_SingleFlight(t *testing.T) {
	// A test registry server that counts fetches; we trigger the background
	// Discover 10x and assert only ONE actual fetch happens while the first
	// is in flight (single-flight semantics — M2 fix).
	var fetchCount int64
	var fetchHold sync.WaitGroup
	fetchHold.Add(1) // held until we release it

	masterPub, masterPriv, _ := testRegistry(t, 0)
	relayPub, _, _ := ed25519.GenerateKey(nil)
	relayPubB64 := base64.StdEncoding.EncodeToString(relayPub)
	msg := append([]byte(SignaturePrefix), relayPub...)
	sig := ed25519.Sign(masterPriv, msg)
	entry := RelayEntry{
		ID:        "relay-de-001",
		Domain:    "de-001.example.com",
		PublicKey: relayPubB64,
		Signature: base64.StdEncoding.EncodeToString(sig),
		Added:     time.Now(),
	}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != EndpointPath {
			http.NotFound(w, r)
			return
		}
		atomic.AddInt64(&fetchCount, 1)
		fetchHold.Wait() // block until the test releases the handler
		w.Header().Set("Content-Type", "application/json")
		w.Write(makeRegistryJSON(t, 1, masterPub, []RelayEntry{entry}))
	}))
	defer srv.Close()

	masterB64 := base64.StdEncoding.EncodeToString(masterPub)
	client, err := NewClient(srv.URL, masterB64, withAllowHTTP)
	if err != nil {
		t.Fatal(err)
	}

	cache := NewCache(filepath.Join(t.TempDir(), "cache.toml"))
	defaultRelay := RelayEntry{ID: "default", Domain: "default.example.com"}
	disc := NewDiscoverer(client, cache, defaultRelay)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	// Fire 10 triggers rapidly while the first fetch is blocked.
	for i := 0; i < 10; i++ {
		disc.TriggerBackgroundDiscover(ctx)
	}

	// Let the (single) in-flight fetch complete.
	time.Sleep(50 * time.Millisecond)
	got := atomic.LoadInt64(&fetchCount)
	if got != 1 {
		t.Errorf("fetchCount during in-flight triggers = %d, want 1 (single-flight)", got)
	}

	// Release the handler and wait for the background goroutine to finish.
	fetchHold.Done()
	// Poll until bgDiscoverRunning flips back to false (goroutine done).
	deadline := time.Now().Add(2 * time.Second)
	for disc.bgDiscoverRunning.Load() && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	if disc.bgDiscoverRunning.Load() {
		t.Fatal("bgDiscoverRunning did not reset after Discover completed")
	}

	// A subsequent trigger AFTER the first one finished should run a new fetch.
	disc.TriggerBackgroundDiscover(ctx)
	time.Sleep(100 * time.Millisecond)
	got = atomic.LoadInt64(&fetchCount)
	if got != 2 {
		t.Errorf("fetchCount after second trigger = %d, want 2 (first done, second fired)", got)
	}
}

func TestClient_DefaultRefreshInterval_Is6h(t *testing.T) {
	// AC Story 4.3 — refresh par défaut à 6h.
	pub, _, err := ed25519.GenerateKey(nil)
	if err != nil {
		t.Fatal(err)
	}
	c, err := NewClient("https://example.com", base64.StdEncoding.EncodeToString(pub))
	if err != nil {
		t.Fatal(err)
	}
	if c.RefreshInterval() != 6*time.Hour {
		t.Errorf("RefreshInterval = %v, want 6h (AC Story 4.3)", c.RefreshInterval())
	}
}

func TestSelectRelay_ConcurrentPicks(t *testing.T) {
	relays := []RelayEntry{
		mustRelay("relay-de-001", "de-001.levoile.dev"),
		mustRelay("relay-de-002", "de-002.levoile.dev"),
	}
	disc := newTestDiscoverer(t, relays)

	const goroutines = 50
	const perG = 100
	var counts sync.Map
	var wg sync.WaitGroup
	for g := 0; g < goroutines; g++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := 0; i < perG; i++ {
				r, err := disc.SelectRelay("de")
				if err != nil {
					t.Errorf("unexpected error: %v", err)
					return
				}
				ctr, _ := counts.LoadOrStore(r.ID, new(int64))
				atomic.AddInt64(ctr.(*int64), 1)
			}
		}()
	}
	wg.Wait()

	totalPicks := int64(goroutines * perG)
	expected := totalPicks / int64(len(relays))
	counts.Range(func(_, v any) bool {
		n := atomic.LoadInt64(v.(*int64))
		// Each relay should be picked exactly half the time (strict RR).
		if n != expected {
			t.Errorf("uneven distribution: got %d, want %d", n, expected)
		}
		return true
	})
}
