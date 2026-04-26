package relay

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"log"
	"net/http"
	"strings"
	"sync/atomic"
	"time"
)

const (
	defaultBlocklistRefresh = 24 * time.Hour
	defaultBlocklistTimeout = 30 * time.Second
)

// Blocklist holds an in-memory set of blocked domain names sourced from
// a StevenBlack/hosts-format URL. The underlying map is swapped atomically
// so concurrent readers are never blocked (AC5, AC6).
//
// No domain name is ever written to logs (NFR22a).
type Blocklist struct {
	entries         atomic.Pointer[map[string]struct{}]
	upstreamURL     string
	client          *http.Client
	refreshInterval time.Duration
}

// NewBlocklist creates a Blocklist. If client is nil a default with 30s
// timeout is used. If refreshInterval is 0, 24h is used.
func NewBlocklist(url string, client *http.Client, refreshInterval time.Duration) *Blocklist {
	if client == nil {
		client = &http.Client{Timeout: defaultBlocklistTimeout}
	}
	if refreshInterval <= 0 {
		refreshInterval = defaultBlocklistRefresh
	}
	b := &Blocklist{
		upstreamURL:     url,
		client:          client,
		refreshInterval: refreshInterval,
	}
	empty := make(map[string]struct{})
	b.entries.Store(&empty)
	return b
}

// Load downloads the blocklist, parses it, and atomically swaps the map.
// On failure the previous map is retained (AC6).
func (b *Blocklist) Load(ctx context.Context) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, b.upstreamURL, nil)
	if err != nil {
		return err
	}
	resp, err := b.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return &BlocklistLoadError{StatusCode: resp.StatusCode}
	}

	m := parseHostsFile(resp.Body)
	b.entries.Store(&m)
	return nil
}

// Start runs a synchronous initial Load, then refreshes every interval.
// Blocks until the initial load completes (success or failure).
// Returns when ctx is cancelled.
func (b *Blocklist) Start(ctx context.Context) {
	if err := b.Load(ctx); err != nil {
		log.Printf("blocklist: initial load failed (%d entries kept)", b.Len())
	} else {
		log.Printf("blocklist: loaded %d entries", b.Len())
	}

	ticker := time.NewTicker(b.refreshInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if err := b.Load(ctx); err != nil {
				log.Printf("blocklist: refresh failed (%d entries kept)", b.Len())
			} else {
				log.Printf("blocklist: refreshed %d entries", b.Len())
			}
		}
	}
}

// IsBlocked returns true if the FQDN (or any of its parent domains) is
// in the blocklist. Lookup walks up the label hierarchy so blocking
// "tracker.com" also blocks "ads.tracker.com" (AC4).
func (b *Blocklist) IsBlocked(fqdn string) bool {
	fqdn = strings.ToLower(strings.TrimSuffix(fqdn, "."))
	m := *b.entries.Load()
	// Walk up labels: a.b.c.d → a.b.c.d, b.c.d, c.d, d
	for domain := fqdn; domain != ""; {
		if _, ok := m[domain]; ok {
			return true
		}
		idx := strings.IndexByte(domain, '.')
		if idx < 0 {
			break
		}
		domain = domain[idx+1:]
	}
	return false
}

// Len returns the current number of entries.
func (b *Blocklist) Len() int {
	return len(*b.entries.Load())
}

// parseHostsFile parses a StevenBlack/hosts-format file. Lines of the form
// "0.0.0.0 domain" or "127.0.0.1 domain" are extracted; comments and blanks
// are skipped. Domains are lowercased with trailing dots stripped.
func parseHostsFile(r io.Reader) map[string]struct{} {
	m := make(map[string]struct{}, 200000) // pre-size for ~150-200k entries
	scanner := bufio.NewScanner(r)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || line[0] == '#' {
			continue
		}
		// Strip inline comments.
		if idx := strings.IndexByte(line, '#'); idx >= 0 {
			line = strings.TrimSpace(line[:idx])
		}
		fields := strings.Fields(line)
		if len(fields) < 2 {
			continue
		}
		ip := fields[0]
		if ip != "0.0.0.0" && ip != "127.0.0.1" {
			continue
		}
		domain := strings.ToLower(strings.TrimSuffix(fields[1], "."))
		if domain == "" || domain == "localhost" || domain == "localhost.localdomain" {
			continue
		}
		m[domain] = struct{}{}
	}
	return m
}

// BlocklistLoadError is returned when the upstream returns a non-200 status.
type BlocklistLoadError struct {
	StatusCode int
}

func (e *BlocklistLoadError) Error() string {
	return fmt.Sprintf("blocklist: upstream returned HTTP %d", e.StatusCode)
}
