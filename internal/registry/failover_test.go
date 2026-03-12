package registry

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
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

func TestFailoverManager_HandleFailover_Success(t *testing.T) {
	relays := []RelayEntry{
		{ID: "relay-0", Domain: "r0.example.com", PublicKey: "key0"},
		{ID: "relay-1", Domain: "r1.example.com", PublicKey: "key1"},
		{ID: "relay-2", Domain: "r2.example.com", PublicKey: "key2"},
	}

	disc := &Discoverer{}
	disc.relays = relays

	updater := &mockRelayUpdater{}
	var connectCalled atomic.Int32
	connectFn := func(ctx context.Context) error {
		connectCalled.Add(1)
		return nil
	}

	fm := NewFailoverManager(disc, updater, connectFn)
	fm.SetCurrentRelay("relay-0")

	err := fm.HandleFailover(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
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
}

func TestFailoverManager_HandleFailover_SkipsCurrent(t *testing.T) {
	relays := []RelayEntry{
		{ID: "relay-0", Domain: "r0.example.com", PublicKey: "key0"},
		{ID: "relay-1", Domain: "r1.example.com", PublicKey: "key1"},
	}

	disc := &Discoverer{}
	disc.relays = relays

	updater := &mockRelayUpdater{}
	connectFn := func(ctx context.Context) error { return nil }

	fm := NewFailoverManager(disc, updater, connectFn)
	fm.SetCurrentRelay("relay-0")

	err := fm.HandleFailover(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// Should skip relay-0 (current) and use relay-1.
	if fm.CurrentRelayID() != "relay-1" {
		t.Errorf("expected relay-1, got %s", fm.CurrentRelayID())
	}
}

func TestFailoverManager_HandleFailover_NoAlternative(t *testing.T) {
	relays := []RelayEntry{
		{ID: "relay-0", Domain: "r0.example.com", PublicKey: "key0"},
	}

	disc := &Discoverer{}
	disc.relays = relays

	updater := &mockRelayUpdater{}
	connectFn := func(ctx context.Context) error { return nil }

	fm := NewFailoverManager(disc, updater, connectFn)
	fm.SetCurrentRelay("relay-0")

	err := fm.HandleFailover(context.Background())
	if !errors.Is(err, ErrNoAlternativeRelay) {
		t.Errorf("expected ErrNoAlternativeRelay, got %v", err)
	}
}

func TestFailoverManager_HandleFailover_AllFail(t *testing.T) {
	relays := []RelayEntry{
		{ID: "relay-0", Domain: "r0.example.com", PublicKey: "key0"},
		{ID: "relay-1", Domain: "r1.example.com", PublicKey: "key1"},
		{ID: "relay-2", Domain: "r2.example.com", PublicKey: "key2"},
	}

	disc := &Discoverer{}
	disc.relays = relays

	updater := &mockRelayUpdater{}
	connectFn := func(ctx context.Context) error {
		return errors.New("connection failed")
	}

	fm := NewFailoverManager(disc, updater, connectFn)
	fm.SetCurrentRelay("relay-0")

	err := fm.HandleFailover(context.Background())
	if !errors.Is(err, ErrNoAlternativeRelay) {
		t.Errorf("expected ErrNoAlternativeRelay, got %v", err)
	}
	// Current relay should not have changed.
	if fm.CurrentRelayID() != "relay-0" {
		t.Errorf("expected relay-0 unchanged, got %s", fm.CurrentRelayID())
	}
	// Tunnel client coordinates must be restored to the original relay so that
	// the Reconnector's subsequent backoff retries target the correct relay.
	updater.mu.Lock()
	restoredDomain := updater.lastDomain
	restoredKey := updater.lastPubKey
	updater.mu.Unlock()
	if restoredDomain != "r0.example.com" {
		t.Errorf("expected updater restored to r0.example.com, got %s", restoredDomain)
	}
	if restoredKey != "key0" {
		t.Errorf("expected updater restored to key0, got %s", restoredKey)
	}
}

func TestFailoverManager_ThreadSafe(t *testing.T) {
	relays := []RelayEntry{
		{ID: "relay-0", Domain: "r0.example.com", PublicKey: "key0"},
		{ID: "relay-1", Domain: "r1.example.com", PublicKey: "key1"},
		{ID: "relay-2", Domain: "r2.example.com", PublicKey: "key2"},
	}

	disc := &Discoverer{}
	disc.relays = relays

	updater := &mockRelayUpdater{}
	connectFn := func(ctx context.Context) error { return nil }

	fm := NewFailoverManager(disc, updater, connectFn)
	fm.SetCurrentRelay("relay-0")

	var wg sync.WaitGroup
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_ = fm.HandleFailover(context.Background())
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
