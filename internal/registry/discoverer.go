package registry

import (
	"context"
	"sort"
	"sync"
	"time"
)

// DiscovererOption configures a Discoverer.
type DiscovererOption func(*Discoverer)

// WithLatencyChecker sets a latency checker for relay selection by latency.
func WithLatencyChecker(checker *LatencyChecker) DiscovererOption {
	return func(d *Discoverer) {
		d.latencyChecker = checker
	}
}

// Discoverer orchestrates relay discovery: online fetch → cache fallback → default relay.
type Discoverer struct {
	client         *Client
	cache          *Cache
	defaultRelay   RelayEntry
	latencyChecker *LatencyChecker

	mu             sync.RWMutex
	relays         []RelayEntry
	lastLatencies  map[string]time.Duration // relayID → last measured latency
	stopCh         chan struct{}
	once           sync.Once
}

// NewDiscoverer creates a discoverer with the given client, cache, and fallback relay.
func NewDiscoverer(client *Client, cache *Cache, defaultRelay RelayEntry, opts ...DiscovererOption) *Discoverer {
	d := &Discoverer{
		client:       client,
		cache:        cache,
		defaultRelay: defaultRelay,
		relays:       []RelayEntry{defaultRelay},
		stopCh:       make(chan struct{}),
	}
	for _, opt := range opts {
		opt(d)
	}
	return d
}

// Discover attempts to fetch relays online, falls back to cache, then to the default relay.
// When a LatencyChecker is configured and more than one relay is available, relays are
// sorted by latency. If latency measurement fails, cached latency rankings are used.
//
// NOTE: The error return is always nil because the fallback cascade (online → cache →
// default relay) guarantees at least the default relay is returned. The error is kept
// in the signature for forward compatibility if stricter modes are added later.
func (d *Discoverer) Discover(ctx context.Context) ([]RelayEntry, error) {
	// Try online fetch first.
	relays, err := d.client.Fetch(ctx)
	if err == nil && len(relays) > 0 {
		// Save to cache (best-effort).
		_ = d.cache.Save(relays, d.client.MasterPublicKeyBase64())

		// Measure latencies and sort if checker is available and >1 relay.
		relays = d.sortByLatency(ctx, relays)

		d.setRelays(relays)
		return relays, nil
	}

	// Fallback: try loading from cache.
	cached, _, cacheErr := d.cache.Load()
	if cacheErr == nil && len(cached) > 0 {
		// Re-verify cached entries using the trusted master key from the client,
		// NOT the key stored in the cache file (which could be attacker-controlled).
		reg := &Registry{
			MasterPublicKey: d.client.MasterPublicKeyBase64(),
			Relays:          cached,
		}
		verified, verifyErr := reg.VerifyAll()
		if verifyErr == nil && len(verified) > 0 {
			// Try latency sort on cached relays too.
			verified = d.sortByLatency(ctx, verified)
			d.setRelays(verified)
			return verified, nil
		}
	}

	// Ultimate fallback: default relay (degraded but functional).
	fallback := []RelayEntry{d.defaultRelay}
	d.setRelays(fallback)
	return fallback, nil
}

// sortByLatency measures and sorts relays by latency if a checker is configured.
// Falls back to cached latency rankings if measurement fails.
// Returns the original order if no checker or only one relay.
func (d *Discoverer) sortByLatency(ctx context.Context, relays []RelayEntry) []RelayEntry {
	if d.latencyChecker == nil || len(relays) <= 1 {
		return relays
	}

	results := d.latencyChecker.MeasureAll(ctx, relays)

	// Check if at least one relay is reachable.
	hasReachable := false
	for _, r := range results {
		if r.Reachable {
			hasReachable = true
			break
		}
	}

	if hasReachable {
		// Save latency rankings to cache (best-effort).
		_ = d.cache.SaveLatencies(results)

		// Store latencies for IPC consumers (e.g. desktop status display).
		latMap := make(map[string]time.Duration, len(results))
		for _, r := range results {
			if r.Reachable {
				latMap[r.Relay.ID] = r.Latency
			}
		}
		d.mu.Lock()
		d.lastLatencies = latMap
		d.mu.Unlock()

		return SortByLatency(results)
	}

	// All measurements failed — try cached rankings.
	return d.sortByLatencyCache(relays)
}

// sortByLatencyCache sorts relays using cached latency rankings as fallback.
func (d *Discoverer) sortByLatencyCache(relays []RelayEntry) []RelayEntry {
	cached, err := d.cache.LoadLatencies()
	if err != nil || len(cached) == 0 {
		return relays // No cached rankings — keep original order (Added DESC).
	}

	// Build a latency map from cache.
	latencyMap := make(map[string]time.Duration, len(cached))
	for _, cl := range cached {
		dur, parseErr := time.ParseDuration(cl.Latency)
		if parseErr == nil {
			latencyMap[cl.RelayID] = dur
		}
	}

	if len(latencyMap) == 0 {
		return relays
	}

	// Partition into known and unknown latency relays.
	type ranked struct {
		entry   RelayEntry
		latency time.Duration
	}
	var known []ranked
	var unknown []RelayEntry
	for _, r := range relays {
		if lat, ok := latencyMap[r.ID]; ok {
			known = append(known, ranked{entry: r, latency: lat})
		} else {
			unknown = append(unknown, r)
		}
	}

	// Sort known by latency ascending.
	sort.Slice(known, func(i, j int) bool {
		return known[i].latency < known[j].latency
	})

	result := make([]RelayEntry, 0, len(relays))
	for _, k := range known {
		result = append(result, k.entry)
	}
	result = append(result, unknown...)
	return result
}

// Start launches periodic refresh in the background.
// If Discover has not been called beforehand, Start performs an initial discovery.
func (d *Discoverer) Start(ctx context.Context) error {
	// Only do initial discover if relays are still the default (no prior Discover call).
	d.mu.RLock()
	needsInitial := len(d.relays) == 1 && d.relays[0].ID == d.defaultRelay.ID
	d.mu.RUnlock()
	if needsInitial {
		_, _ = d.Discover(ctx)
	}

	go d.refreshLoop(ctx)
	return nil
}

// Stop signals the refresh goroutine to exit. Safe to call multiple times.
func (d *Discoverer) Stop() {
	d.once.Do(func() {
		close(d.stopCh)
	})
}

// Relays returns a thread-safe copy of the current relay list.
func (d *Discoverer) Relays() []RelayEntry {
	d.mu.RLock()
	defer d.mu.RUnlock()
	result := make([]RelayEntry, len(d.relays))
	copy(result, d.relays)
	return result
}

// Primary returns the first (best) relay, or the default if none available.
func (d *Discoverer) Primary() RelayEntry {
	d.mu.RLock()
	defer d.mu.RUnlock()
	if len(d.relays) > 0 {
		return d.relays[0]
	}
	return d.defaultRelay
}

func (d *Discoverer) setRelays(relays []RelayEntry) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.relays = relays
}

// LatencyFor returns the last measured latency for the given relay ID.
// Returns 0 if no measurement is available.
func (d *Discoverer) LatencyFor(relayID string) time.Duration {
	d.mu.RLock()
	defer d.mu.RUnlock()
	if d.lastLatencies == nil {
		return 0
	}
	return d.lastLatencies[relayID]
}

func (d *Discoverer) refreshLoop(ctx context.Context) {
	t := time.NewTicker(d.client.RefreshInterval())
	defer t.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-d.stopCh:
			return
		case <-t.C:
			// Re-discover and re-measure latencies.
			// Relays are reordered internally but the active relay is NOT changed
			// (sticky session — AC5). The FailoverManager's currentRelayID is
			// unaffected by relay list reordering.
			_, _ = d.Discover(ctx)
		}
	}
}
