package blocklist

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"sync/atomic"
	"testing"
	"time"
)

// newManagerWithURL creates a Manager that fetches from a custom URL (used in tests only).
func newManagerWithURL(interval time.Duration, url string) *Manager {
	m := NewManager(interval)
	m.url = url
	return m
}

// newTestServer creates an httptest.Server that serves the given hosts content.
func newTestServer(content string) *httptest.Server {
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(content))
	}))
}

// newTestServerFunc creates an httptest.Server with a custom handler.
func newTestServerFunc(fn http.HandlerFunc) *httptest.Server {
	return httptest.NewServer(fn)
}

// waitReady polls until the manager reports ready or the timeout expires.
func waitReady(t *testing.T, m *Manager, timeout time.Duration) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if m.IsReady() {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("timed out waiting for manager to become ready")
}

// waitBlocked polls until the given domain is blocked or the timeout expires.
func waitBlocked(t *testing.T, m *Manager, domain string, timeout time.Duration) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if m.IsBlocked(domain) {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for %s to be blocked", domain)
}

func TestManager_InitialDownload(t *testing.T) {
	srv := newTestServer("0.0.0.0 ads.example.com\n0.0.0.0 tracker.io\n")
	defer srv.Close()

	m := newManagerWithURL(time.Hour, srv.URL)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := m.Start(ctx); err != nil {
		t.Fatalf("Start failed: %v", err)
	}
	defer m.Stop()

	waitReady(t, m, 3*time.Second)

	if !m.IsBlocked("ads.example.com") {
		t.Error("expected ads.example.com to be blocked")
	}
	if !m.IsBlocked("tracker.io") {
		t.Error("expected tracker.io to be blocked")
	}
}

func TestManager_IsBlocked_NotInList(t *testing.T) {
	srv := newTestServer("0.0.0.0 ads.example.com\n")
	defer srv.Close()

	m := newManagerWithURL(time.Hour, srv.URL)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := m.Start(ctx); err != nil {
		t.Fatalf("Start failed: %v", err)
	}
	defer m.Stop()

	waitReady(t, m, 3*time.Second)

	if m.IsBlocked("clean.example.com") {
		t.Error("clean.example.com should not be blocked")
	}
}

func TestManager_AtomicSwap(t *testing.T) {
	var callCount int32
	listA := "0.0.0.0 list-a.com\n"
	listB := "0.0.0.0 list-b.com\n"

	srv := newTestServerFunc(func(w http.ResponseWriter, r *http.Request) {
		count := atomic.AddInt32(&callCount, 1)
		w.WriteHeader(http.StatusOK)
		if count == 1 {
			_, _ = w.Write([]byte(listA))
		} else {
			_, _ = w.Write([]byte(listB))
		}
	})
	defer srv.Close()

	// Short interval to trigger a second download quickly.
	m := newManagerWithURL(50*time.Millisecond, srv.URL)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := m.Start(ctx); err != nil {
		t.Fatalf("Start failed: %v", err)
	}
	defer m.Stop()

	waitBlocked(t, m, "list-a.com", 3*time.Second)

	waitBlocked(t, m, "list-b.com", 3*time.Second)

	// Verify atomic REPLACEMENT (not merge): list-a.com must no longer be blocked.
	if m.IsBlocked("list-a.com") {
		t.Error("list-a.com should no longer be blocked after atomic swap to list B")
	}
}

func TestManager_FallbackOnError(t *testing.T) {
	var callCount int32
	// secondDone is closed when the second (failing) request has been handled.
	secondDone := make(chan struct{})

	srv := newTestServerFunc(func(w http.ResponseWriter, r *http.Request) {
		count := atomic.AddInt32(&callCount, 1)
		if count == 1 {
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte("0.0.0.0 original.com\n"))
		} else {
			w.WriteHeader(http.StatusInternalServerError)
			if count == 2 {
				close(secondDone)
			}
		}
	})
	defer srv.Close()

	m := newManagerWithURL(50*time.Millisecond, srv.URL)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := m.Start(ctx); err != nil {
		t.Fatalf("Start failed: %v", err)
	}
	defer m.Stop()

	waitBlocked(t, m, "original.com", 3*time.Second)

	// Wait for the second (failing) request to complete via channel — no sleep.
	select {
	case <-secondDone:
	case <-time.After(3 * time.Second):
		t.Fatal("second download attempt did not occur in time")
	}

	// Original list should still be active after failed refresh.
	if !m.IsBlocked("original.com") {
		t.Error("original.com should still be blocked after failed refresh (fallback)")
	}
}

func TestManager_StartStop(t *testing.T) {
	srv := newTestServer("0.0.0.0 ads.example.com\n")
	defer srv.Close()

	m := newManagerWithURL(time.Hour, srv.URL)
	ctx := context.Background()

	if err := m.Start(ctx); err != nil {
		t.Fatalf("Start failed: %v", err)
	}

	done := make(chan struct{})
	go func() {
		m.Stop()
		close(done)
	}()

	select {
	case <-done:
		// OK — goroutine exited cleanly.
	case <-time.After(3 * time.Second):
		t.Fatal("Stop() timed out — goroutine may be orphaned")
	}
}

func TestManager_StartAlreadyRunning(t *testing.T) {
	srv := newTestServer("0.0.0.0 ads.example.com\n")
	defer srv.Close()

	m := newManagerWithURL(time.Hour, srv.URL)
	ctx := context.Background()

	if err := m.Start(ctx); err != nil {
		t.Fatalf("first Start failed: %v", err)
	}
	defer m.Stop()

	if err := m.Start(ctx); err != ErrManagerAlreadyRunning {
		t.Errorf("expected ErrManagerAlreadyRunning, got: %v", err)
	}
}

func TestManager_IsReady_FalseBeforeDownload(t *testing.T) {
	srv := newTestServer("0.0.0.0 ads.example.com\n")
	defer srv.Close()

	m := newManagerWithURL(time.Hour, srv.URL)
	if m.IsReady() {
		t.Error("IsReady() should be false before any download")
	}
}

// TestManager_CacheHydratesBeforeNetwork verifies that a Manager given a
// cache path containing a recent StevenBlack-formatted file populates its
// in-memory map from disk before the first HTTP fetch completes. This is
// the core guarantee that eliminates the 5–30 s first-toggle delay.
func TestManager_CacheHydratesBeforeNetwork(t *testing.T) {
	cachePath := t.TempDir() + "/cache.txt"
	if err := os.WriteFile(cachePath, []byte("0.0.0.0 cached.example\n"), 0o644); err != nil {
		t.Fatalf("seed cache: %v", err)
	}

	// Server that blocks until either the test signals via `block` or the
	// per-request ctx is cancelled (so srv.Close can tear down cleanly).
	// The cache-hydration path is the ONLY way IsBlocked can report true
	// within the test window; if we ever observe domains on the wire, the
	// hydration regressed.
	block := make(chan struct{})
	srv := newTestServerFunc(func(w http.ResponseWriter, r *http.Request) {
		select {
		case <-block:
		case <-r.Context().Done():
		}
	})
	// LIFO defer order: close(block) runs FIRST, unblocking the handler,
	// then srv.Close() can drain in-flight connections without hanging.
	defer srv.Close()
	defer close(block)

	m := NewManagerWithCache(time.Hour, cachePath)
	m.url = srv.URL

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	if err := m.Start(ctx); err != nil {
		t.Fatalf("Start failed: %v", err)
	}
	defer m.Stop()

	waitBlocked(t, m, "cached.example", 2*time.Second)
}

// TestManager_RefreshWritesCache verifies that a successful network
// refresh persists the raw payload to disk so the next Start can hydrate
// instantly without re-downloading.
func TestManager_RefreshWritesCache(t *testing.T) {
	cachePath := t.TempDir() + "/cache.txt"
	payload := "0.0.0.0 fresh.example\n"
	srv := newTestServer(payload)
	defer srv.Close()

	m := NewManagerWithCache(time.Hour, cachePath)
	m.url = srv.URL

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := m.Start(ctx); err != nil {
		t.Fatalf("Start failed: %v", err)
	}
	defer m.Stop()

	waitBlocked(t, m, "fresh.example", 2*time.Second)

	data, err := os.ReadFile(cachePath)
	if err != nil {
		t.Fatalf("cache not written: %v", err)
	}
	if string(data) != payload {
		t.Errorf("cache contents = %q, want %q", string(data), payload)
	}
}

func TestManager_ContextCancelled(t *testing.T) {
	srv := newTestServer("0.0.0.0 ads.example.com\n")
	defer srv.Close()

	m := newManagerWithURL(time.Hour, srv.URL)
	ctx, cancel := context.WithCancel(context.Background())

	if err := m.Start(ctx); err != nil {
		t.Fatalf("Start failed: %v", err)
	}

	cancel()

	done := make(chan struct{})
	go func() {
		m.Stop()
		close(done)
	}()

	select {
	case <-done:
		// OK — clean shutdown after context cancellation.
	case <-time.After(3 * time.Second):
		t.Fatal("Stop() timed out after context cancellation")
	}
}
