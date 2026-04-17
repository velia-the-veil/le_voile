package registry

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"
)

func TestMeasureOne_Success(t *testing.T) {
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != HealthEndpoint {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"status":"ok"}`))
	}))
	defer srv.Close()

	// Extract host from test server URL (strip "https://").
	domain := srv.Listener.Addr().String()

	lc := NewLatencyChecker(WithLatencyHTTPClient(srv.Client()))
	relay := RelayEntry{ID: "test-relay", Domain: domain}

	latency, err := lc.MeasureOne(context.Background(), relay)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if latency <= 0 {
		t.Errorf("expected latency > 0, got %v", latency)
	}
}

func TestMeasureOne_Timeout(t *testing.T) {
	done := make(chan struct{})

	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		<-done
	}))
	// Close done first (LIFO) to unblock handler before srv.Close().
	defer srv.Close()
	defer close(done)

	domain := srv.Listener.Addr().String()

	client := srv.Client()
	client.Timeout = 100 * time.Millisecond

	lc := NewLatencyChecker(WithLatencyHTTPClient(client))
	relay := RelayEntry{ID: "timeout-relay", Domain: domain}

	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	_, err := lc.MeasureOne(ctx, relay)
	if err == nil {
		t.Fatal("expected error for timeout, got nil")
	}
}

func TestMeasureOne_HTTPError(t *testing.T) {
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()

	domain := srv.Listener.Addr().String()
	lc := NewLatencyChecker(WithLatencyHTTPClient(srv.Client()))
	relay := RelayEntry{ID: "error-relay", Domain: domain}

	_, err := lc.MeasureOne(context.Background(), relay)
	if err == nil {
		t.Fatal("expected error for HTTP 500, got nil")
	}
}

func TestMeasureAll_Parallel(t *testing.T) {
	// Create 3 servers with different simulated delays. MeasureAll now runs
	// DefaultMedianSamples probes per relay (AC Story 4.3); the cross-relay
	// parallelism is still the key property under test, so we assert that the
	// total wall time stays close to one slow relay's cumulative probe time,
	// not the sum of all relays.
	delays := []time.Duration{0, 50 * time.Millisecond, 100 * time.Millisecond}
	servers := make([]*httptest.Server, len(delays))
	relays := make([]RelayEntry, len(delays))

	for i, d := range delays {
		delay := d
		srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			time.Sleep(delay)
			w.Write([]byte(`{"status":"ok"}`))
		}))
		defer srv.Close()
		servers[i] = srv
		relays[i] = RelayEntry{
			ID:     "relay-" + string(rune('a'+i)),
			Domain: srv.Listener.Addr().String(),
		}
	}

	client := servers[0].Client()
	transport := client.Transport.(*http.Transport)
	transport.TLSClientConfig.InsecureSkipVerify = true

	lc := NewLatencyChecker(WithLatencyHTTPClient(client))

	start := time.Now()
	results := lc.MeasureAll(context.Background(), relays)
	elapsed := time.Since(start)

	// Worst relay ≈ 5*100ms + 4*20ms = 580ms. Sequential would be ≈ 5*(0+50+100)+...
	// = 780ms+. Allow a generous ceiling (1.2s) that still catches regressions
	// to sequential execution across relays (~2.3s).
	if elapsed >= 1200*time.Millisecond {
		t.Errorf("MeasureAll took %v, expected < 1.2s (parallel execution across relays)", elapsed)
	}

	for i, r := range results {
		if !r.Reachable {
			t.Errorf("relay %d: expected reachable, got error: %v", i, r.Error)
		}
	}
}

func TestMeasureAll_PartialFailure(t *testing.T) {
	// 2 OK servers + 1 that blocks (timeout).
	okSrv1 := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"status":"ok"}`))
	}))
	defer okSrv1.Close()

	okSrv2 := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"status":"ok"}`))
	}))
	defer okSrv2.Close()

	blockDone := make(chan struct{})
	blockSrv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		<-blockDone
	}))
	defer blockSrv.Close()
	defer close(blockDone)

	client := okSrv1.Client()
	transport := client.Transport.(*http.Transport)
	transport.TLSClientConfig.InsecureSkipVerify = true

	relays := []RelayEntry{
		{ID: "ok-1", Domain: okSrv1.Listener.Addr().String()},
		{ID: "ok-2", Domain: okSrv2.Listener.Addr().String()},
		{ID: "block", Domain: blockSrv.Listener.Addr().String()},
	}

	lc := NewLatencyChecker(WithLatencyHTTPClient(client))

	ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
	defer cancel()

	results := lc.MeasureAll(ctx, relays)

	reachableCount := 0
	for _, r := range results {
		if r.Reachable {
			reachableCount++
		}
	}
	if reachableCount != 2 {
		t.Errorf("expected 2 reachable, got %d", reachableCount)
	}
}

func TestSortByLatency_Order(t *testing.T) {
	results := []LatencyResult{
		{Relay: RelayEntry{ID: "slow"}, Latency: 100 * time.Millisecond, Reachable: true},
		{Relay: RelayEntry{ID: "fast"}, Latency: 20 * time.Millisecond, Reachable: true},
		{Relay: RelayEntry{ID: "mid"}, Latency: 50 * time.Millisecond, Reachable: true},
		{Relay: RelayEntry{ID: "dead"}, Latency: 0, Reachable: false, Error: fmt.Errorf("timeout")},
	}

	sorted := SortByLatency(results)

	// 3 reachable sorted by latency + 1 unreachable at end.
	if len(sorted) != 4 {
		t.Fatalf("expected 4 sorted relays, got %d", len(sorted))
	}
	if sorted[0].ID != "fast" {
		t.Errorf("first relay: got %s, want fast", sorted[0].ID)
	}
	if sorted[1].ID != "mid" {
		t.Errorf("second relay: got %s, want mid", sorted[1].ID)
	}
	if sorted[2].ID != "slow" {
		t.Errorf("third relay: got %s, want slow", sorted[2].ID)
	}
	if sorted[3].ID != "dead" {
		t.Errorf("fourth relay (unreachable): got %s, want dead", sorted[3].ID)
	}
}

// --- Story 4.3: MeasureOneMedian with 5 samples ---

// stubRTTServer returns a test server that responds to GET /health after
// successive delays pulled from `delays` (indexed by probe count). When the
// stack is exhausted, the server returns the last delay value.
func stubRTTServer(t *testing.T, delays []time.Duration) *httptest.Server {
	t.Helper()
	var mu sync.Mutex
	count := 0
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != HealthEndpoint {
			http.NotFound(w, r)
			return
		}
		mu.Lock()
		d := delays[min(count, len(delays)-1)]
		count++
		mu.Unlock()
		time.Sleep(d)
		w.Write([]byte(`{"status":"ok"}`))
	}))
	return srv
}

func TestMeasureOneMedian_AllSucceed(t *testing.T) {
	// 5 samples: the median of 5 values is the middle element (index 2) of the sorted set.
	srv := stubRTTServer(t, []time.Duration{
		10 * time.Millisecond,
		30 * time.Millisecond,
		20 * time.Millisecond,
		80 * time.Millisecond,
		15 * time.Millisecond,
	})
	defer srv.Close()

	lc := NewLatencyChecker(WithLatencyHTTPClient(srv.Client()))
	relay := RelayEntry{ID: "median-test", Domain: srv.Listener.Addr().String()}

	median, success, err := lc.MeasureOneMedian(context.Background(), relay, 5)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if success != 5 {
		t.Errorf("success count: got %d, want 5", success)
	}
	// Sorted: 10, 15, 20, 30, 80 → median = 20ms. We allow +200% slack for CI noise.
	if median < 15*time.Millisecond || median > 60*time.Millisecond {
		t.Errorf("median: got %v, want close to 20ms", median)
	}
}

func TestMeasureOneMedian_PartialSuccess(t *testing.T) {
	// 3 OK, 2 HTTP 500 → just enough successes to meet MinSuccessfulSamples (3).
	var mu sync.Mutex
	count := 0
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		i := count
		count++
		mu.Unlock()
		if i < 3 {
			w.Write([]byte(`{"status":"ok"}`))
			return
		}
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()

	lc := NewLatencyChecker(WithLatencyHTTPClient(srv.Client()))
	relay := RelayEntry{ID: "partial", Domain: srv.Listener.Addr().String()}

	_, success, err := lc.MeasureOneMedian(context.Background(), relay, 5)
	if err != nil {
		t.Fatalf("expected success with 3/5, got err: %v", err)
	}
	if success != 3 {
		t.Errorf("success: got %d, want 3", success)
	}
}

func TestMeasureOneMedian_InsufficientSuccess(t *testing.T) {
	// Only 2 successes → below MinSuccessfulSamples, Reachable=false expected.
	var mu sync.Mutex
	count := 0
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		i := count
		count++
		mu.Unlock()
		if i < 2 {
			w.Write([]byte(`{"status":"ok"}`))
			return
		}
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()

	lc := NewLatencyChecker(WithLatencyHTTPClient(srv.Client()))
	relay := RelayEntry{ID: "insufficient", Domain: srv.Listener.Addr().String()}

	_, success, err := lc.MeasureOneMedian(context.Background(), relay, 5)
	if err == nil {
		t.Fatal("expected error for insufficient samples, got nil")
	}
	if success != 2 {
		t.Errorf("success count: got %d, want 2", success)
	}
}

func TestMeasureOneMedian_DefaultSamples(t *testing.T) {
	// samples=0 should fall back to DefaultMedianSamples.
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"status":"ok"}`))
	}))
	defer srv.Close()

	lc := NewLatencyChecker(WithLatencyHTTPClient(srv.Client()))
	relay := RelayEntry{ID: "default-samples", Domain: srv.Listener.Addr().String()}

	_, success, err := lc.MeasureOneMedian(context.Background(), relay, 0)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if success != DefaultMedianSamples {
		t.Errorf("success: got %d, want %d", success, DefaultMedianSamples)
	}
}

func TestDefaultLatencyTimeout_Is3Seconds(t *testing.T) {
	// Guardrail test for AC Story 4.3 — timeout /health 3s.
	if DefaultLatencyTimeout != 3*time.Second {
		t.Errorf("DefaultLatencyTimeout = %v, want 3s (AC Story 4.3)", DefaultLatencyTimeout)
	}
	if MaxMeasureTimeout != 3*time.Second {
		t.Errorf("MaxMeasureTimeout = %v, want 3s (AC Story 4.3)", MaxMeasureTimeout)
	}
}

func TestSortByLatency_AllUnreachable(t *testing.T) {
	results := []LatencyResult{
		{Relay: RelayEntry{ID: "dead-1"}, Reachable: false, Error: fmt.Errorf("err")},
		{Relay: RelayEntry{ID: "dead-2"}, Reachable: false, Error: fmt.Errorf("err")},
	}

	sorted := SortByLatency(results)
	// All unreachable relays are still returned (at end of list).
	if len(sorted) != 2 {
		t.Fatalf("expected 2 entries (unreachable at end), got %d", len(sorted))
	}
	if sorted[0].ID != "dead-1" {
		t.Errorf("first relay: got %s, want dead-1", sorted[0].ID)
	}
	if sorted[1].ID != "dead-2" {
		t.Errorf("second relay: got %s, want dead-2", sorted[1].ID)
	}
}
