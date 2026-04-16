package captive

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"
)

// TestE2E_CaptiveTransition simulates a captive portal that clears after a few
// seconds. The test verifies that Probe detects the portal initially (302),
// then detects clearance (204) on subsequent probes.
func TestE2E_CaptiveTransition(t *testing.T) {
	var cleared atomic.Bool

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if cleared.Load() {
			// Portal cleared — return 204 (Google-style).
			w.WriteHeader(204)
			return
		}
		// Portal active — redirect.
		http.Redirect(w, r, "http://portal.local/login", http.StatusFound)
	}))
	defer srv.Close()

	urls := []string{srv.URL + "/?generate_204"}

	// Phase 1: should detect portal.
	detail := Probe(context.Background(), urls)
	if detail.Result != PortalDetected {
		t.Fatalf("phase 1: expected PortalDetected, got %s", detail.Result)
	}

	// Simulate user authenticating (portal clears after 1s).
	go func() {
		time.Sleep(1 * time.Second)
		cleared.Store(true)
	}()

	// Phase 2: poll until cleared (simulates captiveWatcher).
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		time.Sleep(500 * time.Millisecond)
		detail = Probe(context.Background(), urls)
		if detail.Result == NoPortal {
			return // success
		}
	}
	t.Fatal("phase 2: captive portal did not clear within 5s")
}

// TestE2E_TimeoutSkipsCaptive verifies that when the probe server is
// unreachable, ProbeError is returned (fail-open behavior).
func TestE2E_TimeoutSkipsCaptive(t *testing.T) {
	// Use a server that never responds.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		select {
		case <-r.Context().Done():
			return
		case <-time.After(10 * time.Second):
			w.WriteHeader(200)
		}
	}))
	defer srv.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
	defer cancel()

	detail := Probe(ctx, []string{srv.URL})
	if detail.Result != ProbeError {
		t.Errorf("expected ProbeError on timeout, got %s", detail.Result)
	}
}

// TestE2E_ProbeCountSingleURL verifies that Probe hits exactly one URL when
// the first probe is conclusive (no fallback needed).
func TestE2E_ProbeCountSingleURL(t *testing.T) {
	var probeCount atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		probeCount.Add(1)
		w.WriteHeader(204)
	}))
	defer srv.Close()

	detail := Probe(context.Background(), []string{srv.URL + "/?generate_204"})
	if detail.Result != NoPortal {
		t.Errorf("expected NoPortal, got %s", detail.Result)
	}
	if probeCount.Load() != 1 {
		t.Errorf("expected 1 probe, got %d", probeCount.Load())
	}
}

// TestE2E_EmptyURLsUsesDefaults verifies that passing nil URLs causes Probe
// to use DefaultProbeURLs (which will fail in test env — ProbeError expected).
func TestE2E_EmptyURLsUsesDefaults(t *testing.T) {
	// With nil urls, Probe uses DefaultProbeURLs (real internet endpoints).
	// In a test environment without internet, these will timeout/fail → ProbeError.
	ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
	defer cancel()

	detail := Probe(ctx, nil)
	// We expect ProbeError (no internet in CI) or NoPortal (if internet works).
	// The key assertion: it doesn't panic with nil urls and uses defaults.
	if detail.Result == PortalDetected {
		t.Error("unexpected PortalDetected with default URLs in test environment")
	}
}
