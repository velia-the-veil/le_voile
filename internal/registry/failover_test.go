package registry

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// mockRelayUpdater implements RelayUpdater for testing.
type mockRelayUpdater struct {
	mu          sync.Mutex
	lastDomain  string
	lastPubKey  string
	updateErr   error
	updateCount int
}

func (m *mockRelayUpdater) UpdateRelay(domain string, pubKeyBase64 string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.updateCount++
	if m.updateErr != nil {
		return m.updateErr
	}
	m.lastDomain = domain
	m.lastPubKey = pubKeyBase64
	return nil
}

// newFailoverTestDiscoverer builds a Discoverer populated with the given relays and
// optional latency measurements, bypassing the live fetch/cache/verify path.
func newFailoverTestDiscoverer(relays []RelayEntry, latencies map[string]time.Duration) *Discoverer {
	d := &Discoverer{relays: append([]RelayEntry(nil), relays...)}
	if len(latencies) > 0 {
		d.lastLatencies = make(map[string]time.Duration, len(latencies))
		for k, v := range latencies {
			d.lastLatencies[k] = v
		}
	}
	return d
}

func TestFailoverManager_HandleFailover_Success(t *testing.T) {
	relays := []RelayEntry{
		{ID: "relay-0", Domain: "r0.example.com", PublicKey: "key0"},
		{ID: "relay-1", Domain: "r1.example.com", PublicKey: "key1"},
		{ID: "relay-2", Domain: "r2.example.com", PublicKey: "key2"},
	}

	disc := newFailoverTestDiscoverer(relays, nil)

	updater := &mockRelayUpdater{}
	var connectCalled atomic.Int32
	connectFn := func(ctx context.Context) error {
		connectCalled.Add(1)
		return nil
	}

	fm := NewFailoverManager(disc, updater, connectFn)
	fm.SetCurrentRelay("relay-0")

	result, err := fm.HandleFailover(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result == nil {
		t.Fatalf("expected non-nil result")
	}
	if fm.CurrentRelayID() != "relay-1" {
		t.Errorf("expected current relay relay-1, got %s", fm.CurrentRelayID())
	}
	if updater.lastDomain != "r1.example.com" {
		t.Errorf("expected domain r1.example.com, got %s", updater.lastDomain)
	}
	if connectCalled.Load() != 1 {
		t.Errorf("expected 1 connect call, got %d", connectCalled.Load())
	}
	// Relays here carry no country code, so the manager takes the flat-list
	// fallback path. tryPool then derives NewCountry from the target relay's
	// ID/domain when possible; these mocks ("relay-0", etc.) stay empty.
	if result.CrossCountry {
		t.Errorf("fallback flat-list path must not flag CrossCountry")
	}
}

func TestFailoverManager_HandleFailover_Success_UsingPreferredCountry(t *testing.T) {
	// Mocks without country hints in their IDs/domains must still work when
	// the manager has a preferredCountry configured — it substitutes for the
	// missing country extraction.
	relays := []RelayEntry{
		{ID: "relay-0", Domain: "r0.example.com", PublicKey: "key0"},
		{ID: "relay-1", Domain: "r1.example.com", PublicKey: "key1"},
		{ID: "relay-2", Domain: "r2.example.com", PublicKey: "key2"},
	}

	disc := newFailoverTestDiscoverer(relays, nil)

	// To make RelaysByCountry group these under "de", override the IDs.
	disc.relays[0].ID = "relay-de-01"
	disc.relays[1].ID = "relay-de-02"
	disc.relays[2].ID = "relay-de-03"

	updater := &mockRelayUpdater{}
	var connectCalled atomic.Int32
	connectFn := func(ctx context.Context) error {
		connectCalled.Add(1)
		return nil
	}

	fm := NewFailoverManager(disc, updater, connectFn)
	fm.SetCurrentRelay("relay-de-01")

	result, err := fm.HandleFailover(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result == nil {
		t.Fatalf("expected non-nil result")
	}
	if result.CrossCountry {
		t.Errorf("expected intra-country failover, got CrossCountry=true")
	}
	if result.NewCountry != "de" {
		t.Errorf("expected NewCountry=de, got %s", result.NewCountry)
	}
	if fm.CurrentRelayID() != "relay-de-02" {
		t.Errorf("expected relay-de-02, got %s", fm.CurrentRelayID())
	}
	if connectCalled.Load() != 1 {
		t.Errorf("expected 1 connect call, got %d", connectCalled.Load())
	}
}

func TestFailoverManager_HandleFailover_SkipsCurrent(t *testing.T) {
	relays := []RelayEntry{
		{ID: "relay-de-01", Domain: "de-001.levoile.dev", PublicKey: "key0"},
		{ID: "relay-de-02", Domain: "de-002.levoile.dev", PublicKey: "key1"},
	}

	disc := newFailoverTestDiscoverer(relays, nil)

	updater := &mockRelayUpdater{}
	connectFn := func(ctx context.Context) error { return nil }

	fm := NewFailoverManager(disc, updater, connectFn)
	fm.SetCurrentRelay("relay-de-01")

	result, err := fm.HandleFailover(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result == nil || fm.CurrentRelayID() != "relay-de-02" {
		t.Errorf("expected relay-de-02, got %s", fm.CurrentRelayID())
	}
}

func TestFailoverManager_HandleFailover_NoAlternative(t *testing.T) {
	relays := []RelayEntry{
		{ID: "relay-de-01", Domain: "de-001.levoile.dev", PublicKey: "key0"},
	}

	disc := newFailoverTestDiscoverer(relays, nil)

	updater := &mockRelayUpdater{}
	connectFn := func(ctx context.Context) error { return nil }

	fm := NewFailoverManager(disc, updater, connectFn)
	fm.SetCurrentRelay("relay-de-01")

	result, err := fm.HandleFailover(context.Background())
	if !errors.Is(err, ErrNoAlternativeRelay) {
		t.Errorf("expected ErrNoAlternativeRelay, got %v", err)
	}
	if result != nil {
		t.Errorf("expected nil result on failure, got %+v", result)
	}
}

func TestFailoverManager_HandleFailover_AllFail(t *testing.T) {
	relays := []RelayEntry{
		{ID: "relay-de-01", Domain: "de-001.levoile.dev", PublicKey: "key0"},
		{ID: "relay-de-02", Domain: "de-002.levoile.dev", PublicKey: "key1"},
		{ID: "relay-de-03", Domain: "de-003.levoile.dev", PublicKey: "key2"},
	}

	disc := newFailoverTestDiscoverer(relays, nil)

	updater := &mockRelayUpdater{}
	connectFn := func(ctx context.Context) error {
		return errors.New("connection failed")
	}

	fm := NewFailoverManager(disc, updater, connectFn)
	fm.SetCurrentRelay("relay-de-01")

	result, err := fm.HandleFailover(context.Background())
	if !errors.Is(err, ErrNoAlternativeRelay) {
		t.Errorf("expected ErrNoAlternativeRelay, got %v", err)
	}
	if result != nil {
		t.Errorf("expected nil result on failure, got %+v", result)
	}
	// Current relay should not have changed.
	if fm.CurrentRelayID() != "relay-de-01" {
		t.Errorf("expected relay-de-01 unchanged, got %s", fm.CurrentRelayID())
	}
	// Tunnel client coordinates must be restored to the original relay so that
	// the Reconnector's subsequent backoff retries target the correct relay.
	updater.mu.Lock()
	restoredDomain := updater.lastDomain
	restoredKey := updater.lastPubKey
	updater.mu.Unlock()
	if restoredDomain != "de-001.levoile.dev" {
		t.Errorf("expected updater restored to de-001.levoile.dev, got %s", restoredDomain)
	}
	if restoredKey != "key0" {
		t.Errorf("expected updater restored to key0, got %s", restoredKey)
	}
}

func TestFailoverManager_ThreadSafe(t *testing.T) {
	relays := []RelayEntry{
		{ID: "relay-de-01", Domain: "de-001.levoile.dev", PublicKey: "key0"},
		{ID: "relay-de-02", Domain: "de-002.levoile.dev", PublicKey: "key1"},
		{ID: "relay-de-03", Domain: "de-003.levoile.dev", PublicKey: "key2"},
	}

	disc := newFailoverTestDiscoverer(relays, nil)

	updater := &mockRelayUpdater{}
	connectFn := func(ctx context.Context) error { return nil }

	fm := NewFailoverManager(disc, updater, connectFn)
	fm.SetCurrentRelay("relay-de-01")

	var wg sync.WaitGroup
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, _ = fm.HandleFailover(context.Background())
		}()
	}
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_ = fm.CurrentRelayID()
		}()
	}
	wg.Wait()
}

// --- Story 4.4 country-aware failover tests ---

func TestFailoverManager_IntraCountry_PreferredOverAlternate(t *testing.T) {
	relays := []RelayEntry{
		{ID: "relay-de-01", Domain: "de-001.levoile.dev", PublicKey: "keyDE1"},
		{ID: "relay-de-02", Domain: "de-002.levoile.dev", PublicKey: "keyDE2"},
		{ID: "relay-gb-01", Domain: "gb-001.levoile.dev", PublicKey: "keyGB1"},
		{ID: "relay-gb-02", Domain: "gb-002.levoile.dev", PublicKey: "keyGB2"},
	}
	disc := newFailoverTestDiscoverer(relays, nil)

	updater := &mockRelayUpdater{}
	var tried []string
	connectFn := func(ctx context.Context) error {
		tried = append(tried, updater.lastDomain)
		return nil // accept anything — intra-country must be tried first
	}

	fm := NewFailoverManager(disc, updater, connectFn)
	fm.SetCurrentRelay("relay-de-01")

	result, err := fm.HandleFailover(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result == nil {
		t.Fatalf("expected non-nil result")
	}
	if result.CrossCountry {
		t.Errorf("expected intra-country failover (CrossCountry=false), got %+v", result)
	}
	if result.NewCountry != "de" {
		t.Errorf("expected NewCountry=de, got %s", result.NewCountry)
	}
	if result.NewRelayID != "relay-de-02" {
		t.Errorf("expected NewRelayID=relay-de-02, got %s", result.NewRelayID)
	}
	if len(tried) != 1 || tried[0] != "de-002.levoile.dev" {
		t.Errorf("expected single attempt on de-002, tried=%v", tried)
	}
}

func TestFailoverManager_IntraExhausted_ThenInterCountry(t *testing.T) {
	relays := []RelayEntry{
		{ID: "relay-de-01", Domain: "de-001.levoile.dev", PublicKey: "keyDE1"},
		{ID: "relay-de-02", Domain: "de-002.levoile.dev", PublicKey: "keyDE2"},
		{ID: "relay-gb-01", Domain: "gb-001.levoile.dev", PublicKey: "keyGB1"},
		{ID: "relay-gb-02", Domain: "gb-002.levoile.dev", PublicKey: "keyGB2"},
	}
	disc := newFailoverTestDiscoverer(relays, nil)

	updater := &mockRelayUpdater{}
	connectFn := func(ctx context.Context) error {
		// Reject the remaining DE relay, accept any GB relay.
		if updater.lastDomain == "de-002.levoile.dev" {
			return errors.New("simulated 503 on de-002")
		}
		return nil
	}

	fm := NewFailoverManager(disc, updater, connectFn)
	fm.SetCurrentRelay("relay-de-01")

	result, err := fm.HandleFailover(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result == nil {
		t.Fatalf("expected non-nil result")
	}
	if !result.CrossCountry {
		t.Errorf("expected CrossCountry=true after DE pool exhausted")
	}
	if result.NewCountry != "gb" {
		t.Errorf("expected NewCountry=gb, got %s", result.NewCountry)
	}
	if result.NewRelayID != "relay-gb-01" {
		t.Errorf("expected NewRelayID=relay-gb-01, got %s", result.NewRelayID)
	}
	if fm.CurrentRelayID() != "relay-gb-01" {
		t.Errorf("expected CurrentRelayID=relay-gb-01, got %s", fm.CurrentRelayID())
	}
}

func TestFailoverManager_InterCountry_SortedByLatency(t *testing.T) {
	relays := []RelayEntry{
		{ID: "relay-de-01", Domain: "de-001.levoile.dev", PublicKey: "keyDE1"},
		{ID: "relay-de-02", Domain: "de-002.levoile.dev", PublicKey: "keyDE2"},
		{ID: "relay-us-01", Domain: "us-001.levoile.dev", PublicKey: "keyUS1"},
		{ID: "relay-us-02", Domain: "us-002.levoile.dev", PublicKey: "keyUS2"},
		{ID: "relay-gb-01", Domain: "gb-001.levoile.dev", PublicKey: "keyGB1"},
		{ID: "relay-gb-02", Domain: "gb-002.levoile.dev", PublicKey: "keyGB2"},
	}
	// US median ≈ 80ms, GB median ≈ 20ms → GB must be tried before US even
	// though US appears earlier in the relay list.
	latencies := map[string]time.Duration{
		"relay-de-01": 10 * time.Millisecond,
		"relay-de-02": 15 * time.Millisecond,
		"relay-us-01": 80 * time.Millisecond,
		"relay-us-02": 85 * time.Millisecond,
		"relay-gb-01": 20 * time.Millisecond,
		"relay-gb-02": 22 * time.Millisecond,
	}
	disc := newFailoverTestDiscoverer(relays, latencies)

	updater := &mockRelayUpdater{}
	attempted := []string{}
	connectFn := func(ctx context.Context) error {
		attempted = append(attempted, updater.lastDomain)
		// Reject every DE alternative and every GB relay so the manager must
		// eventually settle on a US relay — this also verifies the cascade
		// order: DE(intra) → GB(closest inter) → US(farther inter).
		switch updater.lastDomain {
		case "de-002.levoile.dev", "gb-001.levoile.dev", "gb-002.levoile.dev":
			return errors.New("simulated failure")
		}
		return nil
	}

	fm := NewFailoverManager(disc, updater, connectFn)
	fm.SetCurrentRelay("relay-de-01")

	result, err := fm.HandleFailover(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result == nil {
		t.Fatalf("expected non-nil result")
	}
	if result.NewCountry != "us" {
		t.Errorf("expected NewCountry=us (after DE+GB exhausted), got %s", result.NewCountry)
	}
	// Cascade order must be DE alt first, then GB (lower median), then US.
	// DE-02 must appear before any GB/US, and the first GB domain must appear
	// before the first US domain.
	if len(attempted) < 4 {
		t.Fatalf("expected at least 4 attempts, got %d: %v", len(attempted), attempted)
	}
	if attempted[0] != "de-002.levoile.dev" {
		t.Errorf("expected first attempt on intra-country de-002, got %s", attempted[0])
	}
	// Find first GB and first US indexes.
	firstGB, firstUS := -1, -1
	for i, d := range attempted {
		if d == "gb-001.levoile.dev" && firstGB == -1 {
			firstGB = i
		}
		if d == "us-001.levoile.dev" && firstUS == -1 {
			firstUS = i
		}
	}
	if firstGB == -1 || firstUS == -1 {
		t.Fatalf("expected both GB and US to be attempted, got %v", attempted)
	}
	if firstGB >= firstUS {
		t.Errorf("expected GB to be tried before US (median-sorted), got attempts=%v", attempted)
	}
}

func TestFailoverManager_SetPreferredCountry_UsedAsFallback(t *testing.T) {
	// Relays with no country hint in IDs/domains — the manager must use the
	// explicit preferredCountry to decide which pool is "intra".
	relays := []RelayEntry{
		{ID: "node-0", Domain: "node-0.levoile.dev", PublicKey: "k0"},
		{ID: "node-1", Domain: "node-1.levoile.dev", PublicKey: "k1"},
	}
	// Force these nodes into the "de" bucket by tweaking their IDs to match
	// the extraction regex used by RelaysByCountry.
	relays[0].ID = "relay-de-42"
	relays[1].ID = "relay-de-43"
	disc := newFailoverTestDiscoverer(relays, nil)

	updater := &mockRelayUpdater{}
	connectFn := func(ctx context.Context) error { return nil }

	fm := NewFailoverManager(disc, updater, connectFn)
	// Current relay has no country in its ID — manager must rely on preferredCountry.
	fm.SetCurrentRelay("unknown-id-with-no-country")
	fm.SetPreferredCountry("de")

	result, err := fm.HandleFailover(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result == nil {
		t.Fatalf("expected non-nil result")
	}
	if result.CrossCountry {
		t.Errorf("expected intra-country via preferredCountry fallback, got CrossCountry=true")
	}
	if result.NewCountry != "de" {
		t.Errorf("expected NewCountry=de, got %s", result.NewCountry)
	}
}
