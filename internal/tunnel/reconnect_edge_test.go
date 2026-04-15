package tunnel

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"
)

func TestReconnector_KillSwitchActivateFailure(t *testing.T) {
	ks := &mockKillSwitch{
		activateErr: errors.New("activate failed"),
	}
	updates := make(chan ConnState, 1)
	var connectCount atomic.Int64

	connectFn := func(_ context.Context) error {
		connectCount.Add(1)
		return nil
	}

	r := NewReconnector(updates, connectFn, ks)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	done := make(chan error, 1)
	go func() {
		done <- r.Start(ctx)
	}()

	// Send disconnected state.
	updates <- StateDisconnected

	// Wait for reconnection attempt.
	time.Sleep(500 * time.Millisecond)

	r.Stop()
	<-done

	// Kill switch Activate was called (with retry on failure).
	if ks.activateCount.Load() < 2 {
		t.Errorf("kill switch activated %d times, want >= 2 (original + retry)", ks.activateCount.Load())
	}

	// Connect should still be attempted despite kill switch activation failure.
	if connectCount.Load() < 1 {
		t.Errorf("connectFn called %d times, want >= 1", connectCount.Load())
	}
}

func TestReconnector_RaceBetweenStopAndReconnection(t *testing.T) {
	ks := &mockKillSwitch{}
	updates := make(chan ConnState, 1)

	// Connect always fails to keep reconnecting.
	connectFn := func(ctx context.Context) error {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(50 * time.Millisecond):
			return errors.New("connection refused")
		}
	}

	r := NewReconnector(updates, connectFn, ks)

	ctx := context.Background()
	done := make(chan error, 1)
	go func() {
		done <- r.Start(ctx)
	}()

	// Trigger reconnection.
	updates <- StateDisconnected

	// Let it start reconnecting.
	time.Sleep(200 * time.Millisecond)

	// Stop while actively reconnecting — should not deadlock or panic.
	stopDone := make(chan struct{})
	go func() {
		r.Stop()
		close(stopDone)
	}()

	select {
	case <-stopDone:
		// Good — Stop returned.
	case <-time.After(5 * time.Second):
		t.Fatal("Stop() did not return within 5 seconds — potential deadlock")
	}

	<-done
}

func TestNextBackoff_MaxCap(t *testing.T) {
	// Verify that backoff never exceeds MaxBackoff.
	backoff := InitialBackoff
	for i := 0; i < 20; i++ {
		backoff = nextBackoff(backoff)
		if backoff > MaxBackoff {
			t.Errorf("backoff %v exceeds MaxBackoff %v at iteration %d", backoff, MaxBackoff, i)
		}
	}

	// After many iterations, should be exactly MaxBackoff.
	if backoff != MaxBackoff {
		t.Errorf("backoff = %v after 20 iterations, want %v", backoff, MaxBackoff)
	}
}

func TestNextBackoff_SubSecondInput(t *testing.T) {
	// Verify behavior with sub-second durations.
	got := nextBackoff(500 * time.Millisecond)
	want := 1 * time.Second
	if got != want {
		t.Errorf("nextBackoff(500ms) = %v, want %v", got, want)
	}
	// Initial backoff doubles from 100ms to 200ms.
	if got2 := nextBackoff(InitialBackoff); got2 != 200*time.Millisecond {
		t.Errorf("nextBackoff(InitialBackoff) = %v, want 200ms", got2)
	}
}

func TestReconnector_MultipleDisconnects_OnlyOneReconnection(t *testing.T) {
	// Validates the reconnecting-guard semantics: while a handleDisconnect
	// cycle is in flight, subsequent StateDisconnected notifications must be
	// dropped. Uses a gated connectFn so the first cycle stays busy until the
	// test releases it — this prevents flakiness from the 100ms InitialBackoff
	// allowing multiple cycles to squeak through.
	ks := &mockKillSwitch{}
	updates := make(chan ConnState, 10)
	var connectCount atomic.Int64

	release := make(chan struct{})
	connectFn := func(ctx context.Context) error {
		connectCount.Add(1)
		select {
		case <-release:
		case <-ctx.Done():
		}
		return nil
	}

	r := NewReconnector(updates, connectFn, ks)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	done := make(chan error, 1)
	go func() {
		done <- r.Start(ctx)
	}()

	// Send multiple disconnected states rapidly. The first will enter
	// handleDisconnect and block on `release`; the rest MUST be ignored by
	// the reconnecting guard (not merely delayed by backoff).
	for i := 0; i < 5; i++ {
		updates <- StateDisconnected
	}

	// Give the Start loop ample time to drain and attempt to dispatch. If
	// the guard is broken, we'd see connectCount climb to 5 here.
	time.Sleep(800 * time.Millisecond)

	if got := connectCount.Load(); got != 1 {
		t.Errorf("connectFn called %d times while 1st cycle in flight, want exactly 1 (dedup)", got)
	}

	// Release the in-flight cycle and stop.
	close(release)
	time.Sleep(200 * time.Millisecond)
	r.Stop()
	<-done
}

func TestReconnector_DeactivateFailure_RetryOnce(t *testing.T) {
	// Override Deactivate to fail first time, succeed second.
	failOnce := &failOnceKillSwitch{deactivateFailCount: 1}

	updates := make(chan ConnState, 1)
	connectFn := func(_ context.Context) error { return nil }

	r := NewReconnector(updates, connectFn, failOnce)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	done := make(chan error, 1)
	go func() {
		done <- r.Start(ctx)
	}()

	updates <- StateDisconnected
	time.Sleep(500 * time.Millisecond)

	r.Stop()
	<-done

	// failOnceKillSwitch tracks deactivate calls — should have been called >= 2 times (retry).
	if failOnce.deactivateCount.Load() < 2 {
		t.Errorf("deactivate called %d times, want >= 2 (original + retry)", failOnce.deactivateCount.Load())
	}
}

// failOnceKillSwitch fails the first N Deactivate calls, then succeeds.
type failOnceKillSwitch struct {
	activateCount       atomic.Int64
	deactivateCount     atomic.Int64
	deactivateFailCount int64
}

func (f *failOnceKillSwitch) Activate(_ context.Context) error {
	f.activateCount.Add(1)
	return nil
}

func (f *failOnceKillSwitch) Deactivate(_ context.Context) error {
	n := f.deactivateCount.Add(1)
	if n <= f.deactivateFailCount {
		return errors.New("deactivate failed")
	}
	return nil
}

func (f *failOnceKillSwitch) IsActive() bool {
	return f.activateCount.Load() > f.deactivateCount.Load()
}
