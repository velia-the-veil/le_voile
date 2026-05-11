package registry

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/BurntSushi/toml"
)

// ErrCacheNotFound is returned when the cache file does not exist.
var ErrCacheNotFound = errors.New("registry: cache file not found")

// CachedRelay is the TOML representation of a relay entry.
type CachedRelay struct {
	ID        string    `toml:"id"`
	Domain    string    `toml:"domain"`
	PublicKey string    `toml:"public_key"`
	Signature string    `toml:"signature"`
	Added     time.Time `toml:"added"`
}

// CachedLatency is the TOML representation of a latency ranking entry.
type CachedLatency struct {
	RelayID    string    `toml:"relay_id"`
	Latency    string    `toml:"latency"`
	MeasuredAt time.Time `toml:"measured_at"`
}

// CachedRegistry is the TOML representation of the cached registry.
type CachedRegistry struct {
	MasterPublicKey string          `toml:"master_public_key"`
	Updated         time.Time       `toml:"updated"`
	Relays          []CachedRelay   `toml:"relays"`
	LatencyRankings []CachedLatency `toml:"latency_rankings"`
}

// Cache manages a local TOML file cache of discovered relays.
type Cache struct {
	path string
}

// NewCache creates a cache at the given file path.
func NewCache(path string) *Cache {
	return &Cache{path: path}
}

// Save writes relay entries to the cache file atomically (write to .tmp then rename).
// Preserves the existing latency_rankings section if present.
func (c *Cache) Save(entries []RelayEntry, masterPubKey string) error {
	// Read existing cache to preserve latency_rankings section.
	var existing CachedRegistry
	toml.DecodeFile(c.path, &existing) // ignore error — file may not exist yet

	cached := CachedRegistry{
		MasterPublicKey: masterPubKey,
		Updated:         time.Now(),
		LatencyRankings: existing.LatencyRankings,
	}
	for _, e := range entries {
		cached.Relays = append(cached.Relays, CachedRelay{
			ID:        e.ID,
			Domain:    e.Domain,
			PublicKey: e.PublicKey,
			Signature: e.Signature,
			Added:     e.Added,
		})
	}

	dir := filepath.Dir(c.path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return fmt.Errorf("registry: cache save: %w", err)
	}

	tmpPath := c.path + ".tmp"
	// #nosec G304 -- c.path est défini par le constructeur Cache (paramètre du
	// code appelant, pas user input). Le ".tmp" est concaténé en interne pour
	// écriture atomique. Aucun path traversal possible.
	f, err := os.Create(tmpPath)
	if err != nil {
		return fmt.Errorf("registry: cache save: %w", err)
	}

	if err := toml.NewEncoder(f).Encode(cached); err != nil {
		f.Close()
		os.Remove(tmpPath)
		return fmt.Errorf("registry: cache save: %w", err)
	}
	if err := f.Close(); err != nil {
		os.Remove(tmpPath)
		return fmt.Errorf("registry: cache save: %w", err)
	}

	if err := os.Rename(tmpPath, c.path); err != nil {
		os.Remove(tmpPath)
		return fmt.Errorf("registry: cache save: %w", err)
	}
	return nil
}

// Load reads the cache file and returns relay entries and the master public key.
// Returns ErrCacheNotFound if the file does not exist.
func (c *Cache) Load() ([]RelayEntry, string, error) {
	var cached CachedRegistry
	if _, err := toml.DecodeFile(c.path, &cached); err != nil {
		if os.IsNotExist(err) {
			return nil, "", ErrCacheNotFound
		}
		return nil, "", fmt.Errorf("registry: cache load: %w", err)
	}

	entries := make([]RelayEntry, len(cached.Relays))
	for i, cr := range cached.Relays {
		entries[i] = RelayEntry{
			ID:        cr.ID,
			Domain:    cr.Domain,
			PublicKey: cr.PublicKey,
			Signature: cr.Signature,
			Added:     cr.Added,
		}
	}
	return entries, cached.MasterPublicKey, nil
}

// SaveLatencies reads the existing cache, updates the latency_rankings section,
// and writes the file atomically.
func (c *Cache) SaveLatencies(rankings []LatencyResult) error {
	var cached CachedRegistry
	if _, err := toml.DecodeFile(c.path, &cached); err != nil {
		if !os.IsNotExist(err) {
			return fmt.Errorf("registry: cache save latencies: %w", err)
		}
		// No existing cache — start fresh (only latency section).
	}

	now := time.Now()
	cached.LatencyRankings = make([]CachedLatency, 0, len(rankings))
	for _, r := range rankings {
		if !r.Reachable {
			continue
		}
		cached.LatencyRankings = append(cached.LatencyRankings, CachedLatency{
			RelayID:    r.Relay.ID,
			Latency:    r.Latency.String(),
			MeasuredAt: now,
		})
	}

	dir := filepath.Dir(c.path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return fmt.Errorf("registry: cache save latencies: %w", err)
	}

	tmpPath := c.path + ".tmp"
	// #nosec G304 -- idem ligne 78 : c.path interne, pas user input.
	f, err := os.Create(tmpPath)
	if err != nil {
		return fmt.Errorf("registry: cache save latencies: %w", err)
	}

	if err := toml.NewEncoder(f).Encode(cached); err != nil {
		f.Close()
		os.Remove(tmpPath)
		return fmt.Errorf("registry: cache save latencies: %w", err)
	}
	if err := f.Close(); err != nil {
		os.Remove(tmpPath)
		return fmt.Errorf("registry: cache save latencies: %w", err)
	}

	if err := os.Rename(tmpPath, c.path); err != nil {
		os.Remove(tmpPath)
		return fmt.Errorf("registry: cache save latencies: %w", err)
	}
	return nil
}

// LoadLatencies returns the cached latency rankings.
// Returns an empty slice (no error) if no rankings are stored.
func (c *Cache) LoadLatencies() ([]CachedLatency, error) {
	var cached CachedRegistry
	if _, err := toml.DecodeFile(c.path, &cached); err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("registry: cache load latencies: %w", err)
	}
	return cached.LatencyRankings, nil
}
