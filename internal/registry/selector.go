package registry

import (
	"sync"
)

// countrySelector tracks the round-robin cursor for each country.
// The cursor is kept in RAM only (AC Story 4.3) and reset when the underlying
// relay list for a country changes (detected via snapshot of ordered IDs).
type countrySelector struct {
	mu        sync.Mutex
	cursors   map[string]int
	snapshots map[string][]string
}

func newCountrySelector() *countrySelector {
	return &countrySelector{
		cursors:   make(map[string]int),
		snapshots: make(map[string][]string),
	}
}

// pick returns the next relay for the given country using strict round-robin.
// The cursor is reset to 0 when the pool composition changes.
func (cs *countrySelector) pick(country string, pool []RelayEntry) RelayEntry {
	cs.mu.Lock()
	defer cs.mu.Unlock()

	ids := make([]string, len(pool))
	for i, r := range pool {
		ids[i] = r.ID
	}

	if !sameIDs(cs.snapshots[country], ids) {
		cs.snapshots[country] = ids
		cs.cursors[country] = 0
	}

	idx := cs.cursors[country] % len(pool)
	cs.cursors[country] = (cs.cursors[country] + 1) % len(pool)
	return pool[idx]
}

func sameIDs(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// SelectRelay returns the next relay for the requested country using strict
// round-robin (AC Story 4.3). The cursor is kept in RAM only and reset when
// the relay pool for the country changes (e.g. after a latency re-sort).
//
// Returns ErrUnknownCountry if the country is not in CountryMetaMap, or
// ErrNoRelaysForCountry if no relay is currently known for that country.
func (d *Discoverer) SelectRelay(country string) (RelayEntry, error) {
	if _, ok := CountryMetaMap[country]; !ok {
		return RelayEntry{}, ErrUnknownCountry
	}
	pool := d.RelaysByCountry()[country]
	if len(pool) == 0 {
		return RelayEntry{}, ErrNoRelaysForCountry
	}
	return d.selector.pick(country, pool), nil
}
