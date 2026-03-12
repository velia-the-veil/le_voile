package blocklist

import (
	"context"
	"net/http"
	"net/http/httptest"
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

	// Wait for initial download to complete.
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if m.IsReady() {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}

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

	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if m.IsReady() {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}

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

	// Wait for first download (list A).
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if m.IsBlocked("list-a.com") {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if !m.IsBlocked("list-a.com") {
		t.Fatal("list-a.com should be blocked after first download")
	}

	// Wait for second download (list B).
	deadline = time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if m.IsBlocked("list-b.com") {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if !m.IsBlocked("list-b.com") {
		t.Fatal("list-b.com should be blocked after second download (atomic swap)")
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

	// Wait for first download.
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if m.IsBlocked("original.com") {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if !m.IsBlocked("original.com") {
		t.Fatal("original.com should be blocked after first download")
	}

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
