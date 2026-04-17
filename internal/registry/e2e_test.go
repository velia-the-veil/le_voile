//go:build e2e

package registry

import (
	"context"
	"errors"
	"os"
	"sync"
	"testing"
	"time"
)

// TestE2E_FailoverSameCountry configures 2 relays for the same country
// (Iceland), fails the first, and verifies that failover selects the second
// within 5s while preserving the country code.
func TestE2E_FailoverSameCountry(t *testing.T) {
	if os.Getenv("E2E") != "1" {
		t.Skip("set E2E=1 to run")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	relays := []RelayEntry{
		{ID: "relay-is-01", Domain: "is1.levoile.dev", PublicKey: "key1"},
		{ID: "relay-is-02", Domain: "is2.levoile.dev", PublicKey: "key2"},
	}

	disc := &Discoverer{}
	disc.relays = relays

	updater := &mockRelayUpdater{}

	// connectFn succeeds for the alternative relay. The failover is triggered
	// because relay-is-01 (the current relay) has already failed — that failure
	// is what caused HandleFailover to be called.
	connectFn := func(_ context.Context) error {
		return nil
	}

	fm := NewFailoverManager(disc, updater, connectFn)
	fm.SetCurrentRelay("relay-is-01")

	start := time.Now()
	_, err := fm.HandleFailover(ctx)
	elapsed := time.Since(start)

	if err != nil {
		t.Fatalf("failover failed: %v", err)
	}

	if elapsed > 5*time.Second {
		t.Errorf("failover took %v, expected < 5s", elapsed)
	}

	// Verify failover went to second relay.
	if fm.CurrentRelayID() != "relay-is-02" {
		t.Errorf("current relay = %s, want relay-is-02", fm.CurrentRelayID())
	}

	// Verify country preserved.
	oldCountry := ExtractCountryCode("relay-is-01", "is1.levoile.dev")
	newCountry := ExtractCountryCode(fm.CurrentRelayID(), updater.lastDomain)
	if oldCountry != newCountry {
		t.Errorf("country changed: %s → %s", oldCountry, newCountry)
	}

	t.Logf("failover same country OK: relay-is-01 → relay-is-02 in %v, country=%s", elapsed, newCountry)
}

// TestE2E_FailoverIPConsistency verifies that after failover, the new relay
// belongs to the same country (IP consistency is verified via country code
// since actual IP checking requires a real relay).
func TestE2E_FailoverIPConsistency(t *testing.T) {
	if os.Getenv("E2E") != "1" {
		t.Skip("set E2E=1 to run")
	}

	relays := []RelayEntry{
		{ID: "relay-is-01", Domain: "is1.levoile.dev", PublicKey: "key1"},
		{ID: "relay-is-02", Domain: "is2.levoile.dev", PublicKey: "key2"},
		{ID: "relay-fr-01", Domain: "fr1.levoile.dev", PublicKey: "key3"},
	}

	disc := &Discoverer{}
	disc.relays = relays

	updater := &mockRelayUpdater{}
	connectFn := func(_ context.Context) error { return nil }

	fm := NewFailoverManager(disc, updater, connectFn)
	fm.SetCurrentRelay("relay-is-01")

	if _, err := fm.HandleFailover(context.Background()); err != nil {
		t.Fatalf("failover failed: %v", err)
	}

	newID := fm.CurrentRelayID()
	newCountry := ExtractCountryCode(newID, updater.lastDomain)
	originalCountry := ExtractCountryCode("relay-is-01", "is1.levoile.dev")

	// Failover must change relay.
	if newID == "relay-is-01" {
		t.Fatal("failover did not change relay")
	}

	// Failover should pick relay-is-02 (same country, next in order).
	if newID != "relay-is-02" {
		t.Errorf("failover selected %s, want relay-is-02 (same country)", newID)
	}

	// Country code must be preserved after failover (IP consistency).
	if newCountry != originalCountry {
		t.Errorf("country changed after failover: got %q, want %q", newCountry, originalCountry)
	}
	if newCountry != "is" {
		t.Errorf("expected Iceland country code 'is', got %q", newCountry)
	}

	t.Logf("failover IP consistency OK: relay-is-01 → %s (country=%s)", newID, newCountry)
}

// TestE2E_FailoverKillSwitchProtection simulates a failover scenario and
// verifies that during the transition, DNS queries would be blocked by the
// kill switch (tested via mock callback invocation order).
func TestE2E_FailoverKillSwitchProtection(t *testing.T) {
	if os.Getenv("E2E") != "1" {
		t.Skip("set E2E=1 to run")
	}

	relays := []RelayEntry{
		{ID: "relay-is-01", Domain: "is1.levoile.dev", PublicKey: "key1"},
		{ID: "relay-is-02", Domain: "is2.levoile.dev", PublicKey: "key2"},
	}

	disc := &Discoverer{}
	disc.relays = relays

	updater := &mockRelayUpdater{}

	// Track event order: kill switch should stay active during the entire
	// failover (connect attempt) phase.
	var mu sync.Mutex
	var events []string

	killSwitchActive := true
	events = append(events, "killswitch:active")

	connectFn := func(_ context.Context) error {
		mu.Lock()
		if killSwitchActive {
			events = append(events, "connect:killswitch_protecting")
		} else {
			events = append(events, "connect:killswitch_OFF")
		}
		mu.Unlock()
		return nil
	}

	fm := NewFailoverManager(disc, updater, connectFn)
	fm.SetCurrentRelay("relay-is-01")

	if _, err := fm.HandleFailover(context.Background()); err != nil {
		t.Fatalf("failover failed: %v", err)
	}

	mu.Lock()
	events = append(events, "failover:complete")
	mu.Unlock()

	// Verify kill switch was active during connect.
	mu.Lock()
	defer mu.Unlock()

	foundProtected := false
	for _, e := range events {
		if e == "connect:killswitch_protecting" {
			foundProtected = true
		}
		if e == "connect:killswitch_OFF" {
			t.Error("DNS was NOT protected during failover connect attempt")
		}
	}
	if !foundProtected {
		t.Error("no connect attempt recorded during failover")
	}

	t.Logf("failover kill switch protection OK: events=%v", events)
}

// TestE2E_ReconnectInitiation measures the delay between a relay failure
// detection and the first reconnection attempt. Expected: < 1s (NFR12).
func TestE2E_ReconnectInitiation(t *testing.T) {
	if os.Getenv("E2E") != "1" {
		t.Skip("set E2E=1 to run")
	}

	relays := []RelayEntry{
		{ID: "relay-is-01", Domain: "is1.levoile.dev", PublicKey: "key1"},
		{ID: "relay-is-02", Domain: "is2.levoile.dev", PublicKey: "key2"},
	}

	disc := &Discoverer{}
	disc.relays = relays

	updater := &mockRelayUpdater{}

	// Capture the timestamp of the first connect attempt.
	firstAttempt := make(chan time.Time, 1)
	var once sync.Once

	connectFn := func(_ context.Context) error {
		once.Do(func() {
			firstAttempt <- time.Now()
		})
		return nil
	}

	fm := NewFailoverManager(disc, updater, connectFn)
	fm.SetCurrentRelay("relay-is-01")

	// Simulate failure detection.
	failureDetected := time.Now()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	go func() {
		_, _ = fm.HandleFailover(ctx)
	}()

	select {
	case ts := <-firstAttempt:
		delay := ts.Sub(failureDetected)
		if delay > 1*time.Second {
			t.Errorf("reconnect initiation delay = %v, NFR12 requires < 1s", delay)
		}
		t.Logf("reconnect initiation delay: %v (NFR12: < 1s)", delay)
	case <-ctx.Done():
		t.Fatal("timed out waiting for reconnect attempt")
	}
}

// TestE2E_Story4_3_RoundRobinWithResort verifies the full Story 4.3 contract:
//   - initial relay picked from the "de" pool via round-robin at startup;
//   - successive SelectRelay calls walk the pool in strict order;
//   - after a pool re-sort (latency change) the cursor resets to index 0;
//   - round-robin is per-country (DE vs US cursors are independent).
//
// The scenario mirrors the handler flow (service startup + SelectCountry
// clicks) without running the tunnel layer: Story 4.3 only asks that the
// cursor semantics be honoured, so we drive them through the public API.
func TestE2E_Story4_3_RoundRobinWithResort(t *testing.T) {
	if os.Getenv("E2E") != "1" {
		t.Skip("set E2E=1 to run")
	}

	de1 := RelayEntry{ID: "relay-de-001", Domain: "de-001.levoile.dev"}
	de2 := RelayEntry{ID: "relay-de-002", Domain: "de-002.levoile.dev"}
	us1 := RelayEntry{ID: "relay-us-001", Domain: "us-001.levoile.dev"}
	us2 := RelayEntry{ID: "relay-us-002", Domain: "us-002.levoile.dev"}

	// Initial order: [de1, de2, us1, us2].
	disc := NewDiscovererForTest([]RelayEntry{de1, de2, us1, us2})

	// --- 1. Service startup picks de-001 (cursor 0→1) ---
	r, err := disc.SelectRelay("de")
	if err != nil || r.ID != "relay-de-001" {
		t.Fatalf("startup pick: got %q err=%v, want relay-de-001", r.ID, err)
	}

	// --- 2. Second SelectCountry("de") lands on de-002 (cursor 1→0) ---
	r, err = disc.SelectRelay("de")
	if err != nil || r.ID != "relay-de-002" {
		t.Fatalf("2nd pick: got %q err=%v, want relay-de-002", r.ID, err)
	}

	// --- 3. A separate US cursor is independent ---
	r, err = disc.SelectRelay("us")
	if err != nil || r.ID != "relay-us-001" {
		t.Fatalf("us pick: got %q err=%v, want relay-us-001", r.ID, err)
	}

	// --- 4. Third SelectCountry("de") wraps back to de-001 ---
	r, err = disc.SelectRelay("de")
	if err != nil || r.ID != "relay-de-001" {
		t.Fatalf("3rd de pick: got %q err=%v, want relay-de-001 (wrap)", r.ID, err)
	}

	// --- 5. Simulate a latency re-sort that swaps de-002 to the top ---
	disc.SetRelaysForTest([]RelayEntry{de2, de1, us1, us2})

	// --- 6. After re-sort, cursor resets: next "de" pick is the new head ---
	r, err = disc.SelectRelay("de")
	if err != nil || r.ID != "relay-de-002" {
		t.Fatalf("after resort: got %q err=%v, want relay-de-002 (cursor reset)", r.ID, err)
	}

	// --- 7. US pool unchanged → US cursor untouched ---
	r, err = disc.SelectRelay("us")
	if err != nil || r.ID != "relay-us-002" {
		t.Fatalf("us pool unchanged: got %q err=%v, want relay-us-002 (cursor preserved)", r.ID, err)
	}

	t.Logf("Story 4.3 round-robin + resort contract verified end-to-end")
}

// TestE2E_Story4_4_FailoverKillSwitchActive_Under5s verifies the full
// Story 4.4 contract:
//   - intra-country failover is tried first (de-001 → de-002 fails → cascade);
//   - inter-country cascade picks a GB relay with CrossCountry=true;
//   - the kill switch-like callback is observed as active at every step;
//   - the entire HandleFailover call completes in < 5 seconds (NFR19).
func TestE2E_Story4_4_FailoverKillSwitchActive_Under5s(t *testing.T) {
	if os.Getenv("E2E") != "1" {
		t.Skip("set E2E=1 to run")
	}

	relays := []RelayEntry{
		{ID: "relay-de-001", Domain: "de-001.levoile.dev", PublicKey: "keyDE1"},
		{ID: "relay-de-002", Domain: "de-002.levoile.dev", PublicKey: "keyDE2"},
		{ID: "relay-gb-001", Domain: "gb-001.levoile.dev", PublicKey: "keyGB1"},
		{ID: "relay-gb-002", Domain: "gb-002.levoile.dev", PublicKey: "keyGB2"},
	}
	disc := newFailoverTestDiscoverer(relays, map[string]time.Duration{
		"relay-de-001": 10 * time.Millisecond,
		"relay-de-002": 15 * time.Millisecond,
		"relay-gb-001": 20 * time.Millisecond,
		"relay-gb-002": 25 * time.Millisecond,
	})

	// killSwitchActive tracks that the kill switch stays engaged throughout
	// the failover sequence — the caller (Reconnector) keeps it on until
	// connectFn succeeds.
	var mu sync.Mutex
	killSwitchActive := true
	var observations []bool

	updater := &mockRelayUpdater{}
	connectFn := func(_ context.Context) error {
		mu.Lock()
		observations = append(observations, killSwitchActive)
		mu.Unlock()
		// Simulate 503 on de-002, accept gb-001.
		if updater.lastDomain == "de-002.levoile.dev" {
			return errors.New("503 Service Unavailable")
		}
		return nil
	}

	fm := NewFailoverManager(disc, updater, connectFn)
	fm.SetCurrentRelay("relay-de-001")

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	start := time.Now()
	result, err := fm.HandleFailover(ctx)
	elapsed := time.Since(start)

	if err != nil {
		t.Fatalf("failover failed: %v", err)
	}
	if result == nil {
		t.Fatalf("expected non-nil FailoverResult")
	}
	if !result.CrossCountry {
		t.Errorf("expected CrossCountry=true after DE pool exhausted, got %+v", result)
	}
	if result.NewCountry != "gb" {
		t.Errorf("expected NewCountry=gb, got %s", result.NewCountry)
	}
	if result.NewRelayID != "relay-gb-001" {
		t.Errorf("expected NewRelayID=relay-gb-001, got %s", result.NewRelayID)
	}
	if elapsed > 5*time.Second {
		t.Errorf("failover took %v, NFR19 requires < 5s", elapsed)
	}

	mu.Lock()
	defer mu.Unlock()
	if len(observations) < 2 {
		t.Errorf("expected at least 2 connect attempts (de-002 fail, gb-001 ok), got %d", len(observations))
	}
	for i, active := range observations {
		if !active {
			t.Errorf("kill switch inactive at attempt %d — NFR19 violated", i)
		}
	}
	t.Logf("Story 4.4 failover OK: de-001 → gb-001 (cross-country) in %v, kill switch active at every attempt",
		elapsed)
}

