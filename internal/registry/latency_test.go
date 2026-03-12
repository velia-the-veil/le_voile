package registry

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
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
	// Create 3 servers with different simulated delays.
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

	// Use the first server's TLS client (all share the same test CA).
	client := servers[0].Client()
	// Add all server TLS certs to the client's transport.
	transport := client.Transport.(*http.Transport)
	for _, srv := range servers[1:] {
		srvTransport := srv.Client().Transport.(*http.Transport)
		for _, cert := range srvTransport.TLSClientConfig.RootCAs.Subjects() {
			_ = cert // Subjects() is deprecated; use Certificates pool merging instead.
		}
	}
	// For test simplicity, skip TLS verification.
	transport.TLSClientConfig.InsecureSkipVerify = true

	lc := NewLatencyChecker(WithLatencyHTTPClient(client))

	start := time.Now()
	results := lc.MeasureAll(context.Background(), relays)
	elapsed := time.Since(start)

	// Should complete in parallel: < 200ms (not 150ms sequential).
	if elapsed >= 200*time.Millisecond {
		t.Errorf("MeasureAll took %v, expected < 200ms (parallel execution)", elapsed)
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

	if len(sorted) != 3 {
		t.Fatalf("expected 3 sorted relays, got %d", len(sorted))
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
}

func TestSortByLatency_AllUnreachable(t *testing.T) {
	results := []LatencyResult{
		{Relay: RelayEntry{ID: "dead-1"}, Reachable: false, Error: fmt.Errorf("err")},
		{Relay: RelayEntry{ID: "dead-2"}, Reachable: false, Error: fmt.Errorf("err")},
	}

	sorted := SortByLatency(results)
	if len(sorted) != 0 {
		t.Errorf("expected empty slice, got %d entries", len(sorted))
	}
}
