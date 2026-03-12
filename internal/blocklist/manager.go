package blocklist

import (
	"context"
	"errors"
	"net/http"
	"sync"
	"time"
)

// ErrManagerAlreadyRunning is returned when Start is called on an already-running Manager.
var ErrManagerAlreadyRunning = errors.New("blocklist: manager already running")

// Manager downloads and maintains an in-memory DNS blocklist.
// It performs an initial download on Start and refreshes at the configured interval.
// The in-memory map is replaced atomically on each successful refresh.
// On download failure the existing list remains active.
type Manager struct {
	httpClient *http.Client
	url        string
	interval   time.Duration

	mu         sync.RWMutex
	domains    map[string]struct{}
	lastUpdate time.Time
	running    bool
	cancel     context.CancelFunc
	done       chan struct{}
}

// NewManager creates a Manager with a 30-second HTTP timeout and the given refresh interval.
// If interval is zero it defaults to 24 hours.
func NewManager(interval time.Duration) *Manager {
	if interval == 0 {
		interval = 24 * time.Hour
	}
	return &Manager{
		httpClient: &http.Client{Timeout: 30 * time.Second},
		url:        blocklistURL,
		interval:   interval,
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

// refresh downloads and parses the blocklist, then atomically swaps the in-memory map.
// On error it silently retains the existing list.
func (m *Manager) refresh(ctx context.Context) {
	data, err := downloadFrom(ctx, m.httpClient, m.url)
	if err != nil {
		return
	}
	newDomains := parse(data)
	if len(newDomains) == 0 {
		return
	}

	m.mu.Lock()
	m.domains = newDomains
	m.lastUpdate = time.Now()
	m.mu.Unlock()
}
