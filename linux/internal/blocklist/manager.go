//go:build linux

package blocklist

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// ErrManagerAlreadyRunning is returned when Start is called on an already-running Manager.
var ErrManagerAlreadyRunning = errors.New("blocklist: manager already running")

// cacheMaxAge caps how stale the on-disk cache can be before we ignore it
// at Start time. The refresh loop keeps updating the cache in the
// background regardless, so this only matters for the "first enable after
// a long dormant period" path. 7 days is long enough that a user who
// re-enables the blocklist after a holiday still gets instant activation,
// but short enough that they don't keep blocking hosts that legitimately
// changed.
const cacheMaxAge = 7 * 24 * time.Hour

// Manager downloads and maintains an in-memory DNS blocklist.
// It performs an initial download on Start and refreshes at the configured interval.
// The in-memory map is replaced atomically on each successful refresh.
// On download failure the existing list remains active and the error is tracked.
type Manager struct {
	httpClient *http.Client
	url        string
	interval   time.Duration
	cachePath  string // empty disables disk caching
	logger     io.Writer // error output writer; nil disables logging

	mu              sync.RWMutex
	domains         map[string]struct{}
	lastUpdate      time.Time
	lastError       string
	consecutiveFails int
	running         bool
	cancel          context.CancelFunc
	done            chan struct{}
}

// NewManager creates a Manager with a 30-second HTTP timeout and the given refresh interval.
// If interval is zero it defaults to 24 hours. Disk caching is disabled.
func NewManager(interval time.Duration) *Manager {
	return NewManagerWithCache(interval, "")
}

// NewManagerWithCache is NewManager with an explicit on-disk cache path.
// When cachePath is non-empty, Start loads the cache before the first
// network refresh so the blocklist becomes active within milliseconds
// instead of the 5–30 s the initial StevenBlack download takes over the
// tunnel. refresh() persists every successful download back to the same
// path via atomic temp-file rename.
func NewManagerWithCache(interval time.Duration, cachePath string) *Manager {
	return NewManagerWithCacheAndClient(interval, cachePath, nil)
}

// NewManagerWithCacheAndClient is NewManagerWithCache with a caller-provided
// http.Client. On Windows with TUN + WFP killswitch active, the default
// transport routes the ~800 KB StevenBlack GET through levoile0 → relay
// /tunnel → NAT, which takes ~30 s on cold-start. Passing a client whose
// Transport.Proxy points at the local HTTP CONNECT proxy (which tunnels via
// the relay /connect endpoint, same path as browser traffic) restores the
// 1–3 s download the service had before the TUN pump landed.
// A nil client falls back to &http.Client{Timeout: 30 * time.Second}.
func NewManagerWithCacheAndClient(interval time.Duration, cachePath string, client *http.Client) *Manager {
	if interval == 0 {
		interval = 24 * time.Hour
	}
	if client == nil {
		client = &http.Client{Timeout: 30 * time.Second}
	}
	return &Manager{
		httpClient: client,
		url:        blocklistURL,
		interval:   interval,
		cachePath:  cachePath,
	}
}

// Start begins the background refresh loop. It downloads immediately, then at each interval tick.
// Returns ErrManagerAlreadyRunning if already started.
func (m *Manager) Start(ctx context.Context) error {
	m.mu.Lock()
	if m.running {
		m.mu.Unlock()
		return ErrManagerAlreadyRunning
	}
	ctx, m.cancel = context.WithCancel(ctx)
	m.done = make(chan struct{})
	m.running = true
	m.mu.Unlock()

	go func() {
		defer func() {
			m.mu.Lock()
			m.running = false
			m.cancel = nil
			close(m.done)
			m.mu.Unlock()
		}()

		// Try to hydrate the in-memory map from the on-disk cache before
		// hitting the network. On a warm install this makes the blocklist
		// active within milliseconds instead of the 5–30 s required to
		// fetch StevenBlack/hosts through the VPN tunnel. Missing,
		// unreadable, or stale cache falls through silently to the
		// network path.
		m.loadCache()

		// Initial download immediately.
		m.refresh(ctx)

		ticker := time.NewTicker(m.interval)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				m.refresh(ctx)
			}
		}
	}()

	return nil
}

// loadCache populates domains/lastUpdate from cachePath if the file exists
// and is no more than cacheMaxAge old. Best-effort: any error path leaves
// the in-memory state untouched so the refresh loop can still hydrate from
// the network.
func (m *Manager) loadCache() {
	if m.cachePath == "" {
		return
	}
	info, err := os.Stat(m.cachePath)
	if err != nil {
		return
	}
	if time.Since(info.ModTime()) > cacheMaxAge {
		return
	}
	data, err := os.ReadFile(m.cachePath)
	if err != nil {
		return
	}
	domains := parse(data)
	if len(domains) == 0 {
		return
	}
	m.mu.Lock()
	m.domains = domains
	m.lastUpdate = info.ModTime()
	m.mu.Unlock()
	if m.logger != nil {
		fmt.Fprintf(m.logger, "blocklist: loaded %d domains from cache (mtime=%s)\n", len(domains), info.ModTime().Format(time.RFC3339))
	}
}

// saveCache writes the fresh blocklist payload to cachePath atomically.
// Best-effort: errors are logged but never returned — losing the cache is
// non-fatal, the list is still live in memory and the next start will
// re-download.
func (m *Manager) saveCache(data []byte) {
	if m.cachePath == "" {
		return
	}
	if err := os.MkdirAll(filepath.Dir(m.cachePath), 0o755); err != nil {
		if m.logger != nil {
			fmt.Fprintf(m.logger, "blocklist: cache mkdir: %v\n", err)
		}
		return
	}
	tmp := m.cachePath + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		if m.logger != nil {
			fmt.Fprintf(m.logger, "blocklist: cache write tmp: %v\n", err)
		}
		return
	}
	if err := os.Rename(tmp, m.cachePath); err != nil {
		_ = os.Remove(tmp)
		if m.logger != nil {
			fmt.Fprintf(m.logger, "blocklist: cache rename: %v\n", err)
		}
	}
}

// Stop halts the refresh loop and waits for the background goroutine to exit.
// Safe to call even if Start was never called.
func (m *Manager) Stop() {
	m.mu.Lock()
	cancel := m.cancel
	done := m.done
	m.mu.Unlock()

	if cancel != nil {
		cancel()
	}
	if done != nil {
		<-done
	}
}

// IsBlocked reports whether domain is in the current blocklist. Thread-safe.
// Uses a read lock to allow concurrent lookups without contention.
func (m *Manager) IsBlocked(domain string) bool {
	m.mu.RLock()
	defer m.mu.RUnlock()
	_, ok := m.domains[domain]
	return ok
}

// IsReady reports whether the blocklist has been loaded at least once. Thread-safe.
func (m *Manager) IsReady() bool {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return !m.lastUpdate.IsZero()
}

// LastError returns the last refresh error message (empty if last refresh succeeded). Thread-safe.
func (m *Manager) LastError() string {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.lastError
}

// SetLogger sets the error output writer for refresh failure logging.
func (m *Manager) SetLogger(w io.Writer) {
	m.logger = w
}

// refresh downloads and parses the blocklist, then atomically swaps the in-memory map.
// On error it retains the existing list and logs the failure.
func (m *Manager) refresh(ctx context.Context) {
	data, err := downloadFrom(ctx, m.httpClient, m.url)
	if err != nil {
		m.mu.Lock()
		m.consecutiveFails++
		m.lastError = err.Error()
		fails := m.consecutiveFails
		m.mu.Unlock()
		if m.logger != nil {
			fmt.Fprintf(m.logger, "blocklist: refresh failed (%d consecutive): %v\n", fails, err)
		}
		return
	}
	newDomains := parse(data)
	if len(newDomains) == 0 {
		m.mu.Lock()
		m.consecutiveFails++
		m.lastError = "parsed 0 domains from response"
		fails := m.consecutiveFails
		m.mu.Unlock()
		if m.logger != nil {
			fmt.Fprintf(m.logger, "blocklist: refresh produced 0 domains (%d consecutive)\n", fails)
		}
		return
	}

	m.mu.Lock()
	m.domains = newDomains
	m.lastUpdate = time.Now()
	m.consecutiveFails = 0
	m.lastError = ""
	m.mu.Unlock()

	// Persist the raw payload (not the parsed map) so a future Start can
	// rehydrate instantly without re-downloading. Done outside the lock
	// because file I/O has no correctness dependency on the in-memory
	// state we just committed.
	m.saveCache(data)
}
