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
//
// DefaultLatencyTimeout / MaxMeasureTimeout are tuned to 3s (AC Story 4.3 —
// timeout /health 3s) so that a single slow relay cannot block the entire
// sort cycle and so the UX-facing country switch stays responsive.
const (
	HealthEndpoint        = "/health"
	DefaultLatencyTimeout = 3 * time.Second
	MaxMeasureTimeout     = 3 * time.Second
	maxHealthBodySize     = 1 << 20 // 1 MB limit for health response body

	// DefaultMedianSamples is the number of /health probes aggregated for the
	// median RTT (AC Story 4.3). Fewer than MinSuccessfulSamples out of these
	// must succeed for a relay to be considered reachable.
	DefaultMedianSamples   = 5
	MinSuccessfulSamples   = 3
	medianSampleGap        = 20 * time.Millisecond // pacing between samples
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

// MeasureOneMedian probes /health `samples` times (default DefaultMedianSamples)
// and returns the median RTT computed over the successful probes. Per-probe
// timeout honours DefaultLatencyTimeout (AC Story 4.3 — 3s). The relay is
// considered Reachable only if at least MinSuccessfulSamples probes succeed.
//
// Returns (median, successCount, err). On insufficient successes the error is
// wrapped with the relay ID; callers should NOT log the relay domain or any
// client IP to comply with NFR20.
func (lc *LatencyChecker) MeasureOneMedian(ctx context.Context, relay RelayEntry, samples int) (time.Duration, int, error) {
	if samples <= 0 {
		samples = DefaultMedianSamples
	}

	rtts := make([]time.Duration, 0, samples)
probeLoop:
	for i := 0; i < samples; i++ {
		if i > 0 {
			// Small pacing between probes so transient blips don't all land in
			// the same TCP congestion window. Bail out of the enclosing loop
			// (not just the select) when the parent context is cancelled so
			// we don't burn the remaining iterations on guaranteed failures.
			select {
			case <-ctx.Done():
				break probeLoop
			case <-time.After(medianSampleGap):
			}
		}
		d, err := lc.MeasureOne(ctx, relay)
		if err == nil {
			rtts = append(rtts, d)
		}
	}

	if len(rtts) < MinSuccessfulSamples {
		return 0, len(rtts), fmt.Errorf("registry: latency: %s: insufficient successful samples (%d/%d)",
			relay.ID, len(rtts), samples)
	}

	sort.Slice(rtts, func(i, j int) bool { return rtts[i] < rtts[j] })
	return rtts[len(rtts)/2], len(rtts), nil
}

// MeasureAll measures latencies for all relays in parallel with a global timeout.
// Each relay is probed DefaultMedianSamples times and the median RTT is retained
// (AC Story 4.3). A relay is marked Reachable only if enough probes succeed.
func (lc *LatencyChecker) MeasureAll(ctx context.Context, relays []RelayEntry) []LatencyResult {
	ctx, cancel := context.WithTimeout(ctx, MaxMeasureTimeout*time.Duration(DefaultMedianSamples))
	defer cancel()

	results := make([]LatencyResult, len(relays))
	var wg sync.WaitGroup
	for i, relay := range relays {
		wg.Add(1)
		go func(idx int, r RelayEntry) {
			defer wg.Done()
			median, _, err := lc.MeasureOneMedian(ctx, r, DefaultMedianSamples)
			results[idx] = LatencyResult{
				Relay:     r,
				Latency:   median,
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
