package registry

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"sync"
	"time"
)

// ErrNoAlternativeRelay is returned when no alternative relay is available for failover.
var ErrNoAlternativeRelay = errors.New("registry: failover: no alternative relay available")

// RelayUpdater is the interface for updating the tunnel client's relay target.
// Implemented by tunnel.Client.
type RelayUpdater interface {
	UpdateRelay(domain string, pubKeyBase64 string) error
}

// FailoverResult describes the outcome of a successful failover. The service
// layer inspects CrossCountry to decide whether to surface a UI alert
// (inter-country switches are user-visible; intra-country failovers are silent).
type FailoverResult struct {
	NewCountry   string // ISO2 code of the country hosting the new relay, empty if unknown
	NewRelayID   string // ID of the relay the tunnel is now pointing at
	CrossCountry bool   // true when the failover crossed a country boundary
}

// FailoverManager handles automatic failover to the next relay in the latency
// ranking, prioritising intra-country alternatives (Story 4.4). When the
// current country's pool is exhausted it cascades to other countries sorted
// by median latency. The kill switch remains active for the entire sequence;
// the Reconnector (not this manager) is responsible for gating deactivation
// on a successful reconnect.
type FailoverManager struct {
	discoverer    *Discoverer
	tunnelUpdater RelayUpdater
	connectFn     func(ctx context.Context) error

	mu               sync.RWMutex
	currentRelayID   string
	preferredCountry string // fallback when ExtractCountryCode cannot derive the country
}

// NewFailoverManager creates a failover manager.
func NewFailoverManager(discoverer *Discoverer, updater RelayUpdater, connectFn func(ctx context.Context) error) *FailoverManager {
	return &FailoverManager{
		discoverer:    discoverer,
		tunnelUpdater: updater,
		connectFn:     connectFn,
	}
}

// SetPreferredCountry records the ISO2 code the user explicitly chose. It is
// used as a fallback when the current relay's country cannot be derived from
// its ID/domain (e.g. legacy registries without the relay-{code}-{num} scheme).
func (fm *FailoverManager) SetPreferredCountry(code string) {
	fm.mu.Lock()
	defer fm.mu.Unlock()
	fm.preferredCountry = code
}

// HandleFailover attempts to switch to the next available relay and reconnect.
// It returns a FailoverResult describing the new relay when successful, or
// ErrNoAlternativeRelay wrapped with context when every alternative fails.
//
// Selection order:
//  1. Other relays in the current country (latency-sorted, round-robin offset)
//  2. Relays in other countries, sorted by median latency ascending
//
// On total failure the tunnel updater is restored to the original relay
// coordinates so the Reconnector's subsequent backoff keeps targeting the
// same relay instead of the last alternative tried.
func (fm *FailoverManager) HandleFailover(ctx context.Context) (*FailoverResult, error) {
	fm.mu.Lock()
	defer fm.mu.Unlock()

	relays := fm.discoverer.Relays()
	if len(relays) <= 1 {
		return nil, ErrNoAlternativeRelay
	}

	// Locate the current relay so we can restore it on total failure.
	var originalDomain, originalPubKey string
	var originalEntry RelayEntry
	for _, r := range relays {
		if r.ID == fm.currentRelayID {
			originalDomain = r.Domain
			originalPubKey = r.PublicKey
			originalEntry = r
			break
		}
	}

	// Derive the current country. Prefer the relay's ID/domain, fall back to
	// the explicit preference — essential when the current relay carries no
	// country hint (legacy mocks in tests use empty IDs).
	currentCountry := ExtractCountryCode(originalEntry.ID, originalEntry.Domain)
	if currentCountry == "" {
		currentCountry = fm.preferredCountry
	}

	byCountry := fm.discoverer.RelaysByCountry()

	// 1. Intra-country attempt.
	if currentCountry != "" {
		pool := byCountry[currentCountry]
		if result := fm.tryPool(ctx, pool, currentCountry, false); result != nil {
			return result, nil
		}
	} else {
		// No country metadata on the current relay and no preferredCountry
		// configured — preserve historical behaviour by treating the full
		// relay list as the intra pool. This keeps legacy setups (mocks,
		// single-country registries) working without forcing country hints.
		if result := fm.tryPool(ctx, relays, "", false); result != nil {
			return result, nil
		}
		// Skip the cascade: without a known country there is no meaningful
		// "inter-country" boundary.
		if originalDomain != "" {
			_ = fm.tunnelUpdater.UpdateRelay(originalDomain, originalPubKey)
		}
		return nil, fmt.Errorf("%w: all alternatives failed", ErrNoAlternativeRelay)
	}

	// 2. Inter-country cascade, sorted by median latency ascending.
	altCountries := fm.sortedAlternateCountries(byCountry, currentCountry)
	for _, code := range altCountries {
		pool := byCountry[code]
		if result := fm.tryPool(ctx, pool, code, true); result != nil {
			return result, nil
		}
	}

	// Restore original relay coordinates so backoff keeps targeting the right
	// relay instead of whichever failed alternative was last tried.
	if originalDomain != "" {
		_ = fm.tunnelUpdater.UpdateRelay(originalDomain, originalPubKey)
	}

	return nil, fmt.Errorf("%w: all alternatives failed", ErrNoAlternativeRelay)
}

// tryPool walks a relay pool, skipping the current relay, and returns a
// FailoverResult on the first successful reconnect. Returns nil when every
// relay in the pool fails (or the pool is empty).
//
// When country is empty (fallback flat-list path), the country is re-derived
// from the target relay's ID/domain so callers never receive an empty
// NewCountry when one could be extracted — this keeps the UI's active-country
// flag in sync after the manager's historical behaviour kicks in.
func (fm *FailoverManager) tryPool(ctx context.Context, pool []RelayEntry, country string, crossCountry bool) *FailoverResult {
	for _, next := range pool {
		if next.ID == fm.currentRelayID {
			continue
		}
		if err := fm.tunnelUpdater.UpdateRelay(next.Domain, next.PublicKey); err != nil {
			continue
		}
		if err := fm.connectFn(ctx); err != nil {
			continue
		}
		fm.currentRelayID = next.ID
		resolvedCountry := country
		if resolvedCountry == "" {
			resolvedCountry = ExtractCountryCode(next.ID, next.Domain)
		}
		return &FailoverResult{
			NewCountry:   resolvedCountry,
			NewRelayID:   next.ID,
			CrossCountry: crossCountry,
		}
	}
	return nil
}

// sortedAlternateCountries returns ISO2 codes (excluding currentCountry and
// the "unknown" bucket) ordered by the median latency of their relays,
// ascending. Countries without any measured latency fall back to lexical
// order after those with measurements.
func (fm *FailoverManager) sortedAlternateCountries(byCountry map[string][]RelayEntry, currentCountry string) []string {
	type entry struct {
		code   string
		median time.Duration
		known  bool
	}

	items := make([]entry, 0, len(byCountry))
	for code, pool := range byCountry {
		if code == currentCountry || code == "" || code == "unknown" {
			continue
		}
		if len(pool) == 0 {
			continue
		}
		median, known := medianLatency(fm.discoverer, pool)
		items = append(items, entry{code: code, median: median, known: known})
	}

	sort.Slice(items, func(i, j int) bool {
		if items[i].known != items[j].known {
			return items[i].known // known latencies first
		}
		if items[i].known {
			return items[i].median < items[j].median
		}
		return items[i].code < items[j].code
	})

	result := make([]string, len(items))
	for i, it := range items {
		result[i] = it.code
	}
	return result
}

// medianLatency computes the median of measured latencies for the given pool.
// Returns (0, false) when no relay in the pool has a known latency — the
// caller falls back to lexical ordering.
func medianLatency(d *Discoverer, pool []RelayEntry) (time.Duration, bool) {
	if d == nil {
		return 0, false
	}
	latencies := make([]time.Duration, 0, len(pool))
	for _, r := range pool {
		if lat := d.LatencyFor(r.ID); lat > 0 {
			latencies = append(latencies, lat)
		}
	}
	if len(latencies) == 0 {
		return 0, false
	}
	sort.Slice(latencies, func(i, j int) bool { return latencies[i] < latencies[j] })
	mid := len(latencies) / 2
	if len(latencies)%2 == 1 {
		return latencies[mid], true
	}
	return (latencies[mid-1] + latencies[mid]) / 2, true
}

// SetCurrentRelay sets the currently active relay ID.
func (fm *FailoverManager) SetCurrentRelay(relayID string) {
	fm.mu.Lock()
	defer fm.mu.Unlock()
	fm.currentRelayID = relayID
}

// CurrentRelayID returns the currently active relay ID.
func (fm *FailoverManager) CurrentRelayID() string {
	fm.mu.RLock()
	defer fm.mu.RUnlock()
	return fm.currentRelayID
}
