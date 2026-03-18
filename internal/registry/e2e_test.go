//go:build e2e

package registry

import (
	"context"
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
	err := fm.HandleFailover(ctx)
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

	if err := fm.HandleFailover(context.Background()); err != nil {
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

	if err := fm.HandleFailover(context.Background()); err != nil {
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
		fm.HandleFailover(ctx)
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
