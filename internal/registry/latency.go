package registry

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"sort"
	"sync"
	"time"
)

// Latency measurement constants.
const (
	HealthEndpoint        = "/health"
	DefaultLatencyTimeout = 5 * time.Second
	MaxMeasureTimeout     = 5 * time.Second
	maxHealthBodySize     = 1 << 20 // 1 MB limit for health response body
)

// LatencyResult holds the measurement outcome for a single relay.
type LatencyResult struct {
	Relay     RelayEntry
	Latency   time.Duration
	Reachable bool
	Error     error
}

// LatencyOption configures a LatencyChecker.
type LatencyOption func(*LatencyChecker)

// WithLatencyHTTPClient sets a custom HTTP client for latency measurements.
func WithLatencyHTTPClient(c *http.Client) LatencyOption {
	return func(lc *LatencyChecker) {
		lc.httpClient = c
	}
}

// LatencyChecker measures relay latencies via HTTP GET to /health.
type LatencyChecker struct {
	httpClient *http.Client
}

// NewLatencyChecker creates a latency checker with default settings.
func NewLatencyChecker(opts ...LatencyOption) *LatencyChecker {
	lc := &LatencyChecker{
		httpClient: &http.Client{
			Timeout: DefaultLatencyTimeout,
		},
	}
	for _, opt := range opts {
		opt(lc)
	}
	return lc
}

// MeasureOne measures the round-trip latency to a single relay's /health endpoint.
func (lc *LatencyChecker) MeasureOne(ctx context.Context, relay RelayEntry) (time.Duration, error) {
	if relay.Domain == "" {
		return 0, fmt.Errorf("registry: latency: %s: empty domain", relay.ID)
	}

	url := "https://" + relay.Domain + HealthEndpoint
	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return 0, fmt.Errorf("registry: latency: %s: %w", relay.ID, err)
	}

	start := time.Now()
	resp, err := lc.httpClient.Do(req)
	elapsed := time.Since(start)
	if err != nil {
		return 0, fmt.Errorf("registry: latency: %s: %w", relay.ID, err)
	}
	defer resp.Body.Close()

	// Drain body with size limit (protection against oversized responses).
	io.Copy(io.Discard, io.LimitReader(resp.Body, maxHealthBodySize))

	if resp.StatusCode != http.StatusOK {
		return 0, fmt.Errorf("registry: latency: %s: status %d", relay.ID, resp.StatusCode)
	}
	return elapsed, nil
}

// MeasureAll measures latencies for all relays in parallel with a global timeout.
func (lc *LatencyChecker) MeasureAll(ctx context.Context, relays []RelayEntry) []LatencyResult {
	ctx, cancel := context.WithTimeout(ctx, MaxMeasureTimeout)
	defer cancel()

	results := make([]LatencyResult, len(relays))
	var wg sync.WaitGroup
	for i, relay := range relays {
		wg.Add(1)
		go func(idx int, r RelayEntry) {
			defer wg.Done()
			latency, err := lc.MeasureOne(ctx, r)
			results[idx] = LatencyResult{
				Relay:     r,
				Latency:   latency,
				Reachable: err == nil,
				Error:     err,
			}
		}(i, relay)
	}
	wg.Wait()
	return results
}

// SortByLatency sorts results by latency ascending, with unreachable relays at the end.
// Reachable relays are sorted by latency; unreachable relays follow in original order.
func SortByLatency(results []LatencyResult) []RelayEntry {
	var reachable, unreachable []LatencyResult
	for _, r := range results {
		if r.Reachable {
			reachable = append(reachable, r)
		} else {
			unreachable = append(unreachable, r)
		}
	}

	sort.Slice(reachable, func(i, j int) bool {
		return reachable[i].Latency < reachable[j].Latency
	})

	sorted := make([]RelayEntry, 0, len(results))
	for _, r := range reachable {
		sorted = append(sorted, r.Relay)
	}
	for _, r := range unreachable {
		sorted = append(sorted, r.Relay)
	}
	return sorted
}
