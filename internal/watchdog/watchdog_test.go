package watchdog

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"
)

func TestWatchdog_DetectsInconsistency(t *testing.T) {
	var fixCount atomic.Int64
	var fixAddr atomic.Value

	checker := func(_ context.Context) (string, error) {
		return "8.8.8.8", nil // inconsistent
	}

	fixer := func(_ context.Context, addr string) error {
		fixCount.Add(1)
		fixAddr.Store(addr)
		return nil
	}

	w := NewWatchdog("127.0.0.1", checker, fixer)
	w.interval = 100 * time.Millisecond

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- w.Start(ctx) }()

	time.Sleep(350 * time.Millisecond)
	cancel()
	<-done

	if fixCount.Load() < 2 {
		t.Errorf("fixer called %d times, want >= 2", fixCount.Load())
	}

	if addr, ok := fixAddr.Load().(string); !ok || addr != "127.0.0.1" {
		t.Errorf("fixer called with %q, want %q", addr, "127.0.0.1")
	}
}

func TestWatchdog_NoFixWhenConsistent(t *testing.T) {
	var fixCount atomic.Int64

	checker := func(_ context.Context) (string, error) {
		return "127.0.0.1", nil // consistent
	}

	fixer := func(_ context.Context, _ string) error {
		fixCount.Add(1)
		return nil
	}

	w := NewWatchdog("127.0.0.1", checker, fixer)
	w.interval = 100 * time.Millisecond

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- w.Start(ctx) }()

	time.Sleep(350 * time.Millisecond)
	cancel()
	<-done

	if fixCount.Load() != 0 {
		t.Errorf("fixer called %d times, want 0 (resolver was consistent)", fixCount.Load())
	}
}

func TestWatchdog_GracefulStop(t *testing.T) {
	checker := func(_ context.Context) (string, error) {
		return "127.0.0.1", nil
	}
	fixer := func(_ context.Context, _ string) error { return nil }

	w := NewWatchdog("127.0.0.1", checker, fixer)
	w.interval = 100 * time.Millisecond

	done := make(chan error, 1)
	go func() { done <- w.Start(context.Background()) }()

	time.Sleep(200 * time.Millisecond)
	w.Stop()

	select {
	case err := <-done:
		if err != nil {
			t.Errorf("Start returned error: %v", err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("watchdog did not stop within timeout")
	}
}

func TestWatchdog_DoubleStart(t *testing.T) {
	checker := func(_ context.Context) (string, error) {
		return "127.0.0.1", nil
	}
	fixer := func(_ context.Context, _ string) error { return nil }

	w := NewWatchdog("127.0.0.1", checker, fixer)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	done := make(chan error, 1)
	go func() { done <- w.Start(ctx) }()

	time.Sleep(100 * time.Millisecond)

	err := w.Start(ctx)
	if !errors.Is(err, ErrWatchdogAlreadyRunning) {
		t.Errorf("second Start = %v, want ErrWatchdogAlreadyRunning", err)
	}

	cancel()
	<-done
}

func TestWatchdog_CheckerError(t *testing.T) {
	var fixCount atomic.Int64

	checker := func(_ context.Context) (string, error) {
		return "", errors.New("check failed")
	}

	fixer := func(_ context.Context, _ string) error {
		fixCount.Add(1)
		return nil
	}

	w := NewWatchdog("127.0.0.1", checker, fixer)
	w.interval = 100 * time.Millisecond

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- w.Start(ctx) }()

	time.Sleep(350 * time.Millisecond)
	cancel()
	<-done

	if fixCount.Load() != 0 {
		t.Errorf("fixer called %d times, want 0 (checker errors should be skipped)", fixCount.Load())
	}
}

func TestWatchdog_VerifyAndRestore(t *testing.T) {
	tests := []struct {
		name        string
		current     string
		expected    string
		checkerErr  error
		fixerErr    error
		wantErr     bool
		wantFixCall bool
	}{
		{
			name:        "consistent resolver, no fix needed",
			current:     "127.0.0.1",
			expected:    "127.0.0.1",
			wantFixCall: false,
		},
		{
			name:        "inconsistent resolver, fix applied",
			current:     "8.8.8.8",
			expected:    "127.0.0.1",
			wantFixCall: true,
		},
		{
			name:       "checker error",
			checkerErr: errors.New("failed"),
			expected:   "127.0.0.1",
			wantErr:    true,
		},
		{
			name:        "fixer error",
			current:     "8.8.8.8",
			expected:    "127.0.0.1",
			fixerErr:    errors.New("failed"),
			wantErr:     true,
			wantFixCall: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var fixCalled bool

			checker := func(_ context.Context) (string, error) {
				return tt.current, tt.checkerErr
			}
			fixer := func(_ context.Context, _ string) error {
				fixCalled = true
				return tt.fixerErr
			}

			w := NewWatchdog("127.0.0.1", checker, fixer)

			err := w.VerifyAndRestore(context.Background(), tt.expected)

			if (err != nil) != tt.wantErr {
				t.Errorf("VerifyAndRestore error = %v, wantErr %v", err, tt.wantErr)
			}
			if fixCalled != tt.wantFixCall {
				t.Errorf("fixer called = %v, want %v", fixCalled, tt.wantFixCall)
			}
		})
	}
}
