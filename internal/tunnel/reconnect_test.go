package tunnel

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"
)

// mockKillSwitch is a test double for KillSwitchController.
type mockKillSwitch struct {
	activateCount  atomic.Int64
	deactivateCount atomic.Int64
	activateErr    error
	deactivateErr  error
}

func (m *mockKillSwitch) Activate(_ context.Context) error {
	m.activateCount.Add(1)
	return m.activateErr
}

func (m *mockKillSwitch) Deactivate(_ context.Context) error {
	m.deactivateCount.Add(1)
	return m.deactivateErr
}

func (m *mockKillSwitch) IsActive() bool {
	return m.activateCount.Load() > m.deactivateCount.Load()
}

func TestReconnector_BackoffSequence(t *testing.T) {
	tests := []struct {
		name     string
		current  time.Duration
		expected time.Duration
	}{
		{"1s -> 2s", 1 * time.Second, 2 * time.Second},
		{"2s -> 4s", 2 * time.Second, 4 * time.Second},
		{"4s -> 8s", 4 * time.Second, 8 * time.Second},
		{"8s -> 16s", 8 * time.Second, 16 * time.Second},
		{"16s -> 30s (capped)", 16 * time.Second, 30 * time.Second},
		{"30s -> 30s (stays capped)", 30 * time.Second, 30 * time.Second},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := nextBackoff(tt.current)
			if got != tt.expected {
				t.Errorf("nextBackoff(%v) = %v, want %v", tt.current, got, tt.expected)
			}
		})
	}
}

func TestReconnector_SuccessfulReconnect(t *testing.T) {
	ks := &mockKillSwitch{}
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

	// Wait for reconnection to complete.
	time.Sleep(2 * time.Second)

	r.Stop()
	<-done

	if connectCount.Load() != 1 {
		t.Errorf("connectFn called %d times, want 1", connectCount.Load())
	}
	if ks.activateCount.Load() != 1 {
		t.Errorf("kill switch activated %d times, want 1", ks.activateCount.Load())
	}
	if ks.deactivateCount.Load() != 1 {
		t.Errorf("kill switch deactivated %d times, want 1", ks.deactivateCount.Load())
	}
}

func TestReconnector_BackoffOnFailures(t *testing.T) {
	ks := &mockKillSwitch{}
	updates := make(chan ConnState, 1)
	var connectCount atomic.Int64

	connectFn := func(_ context.Context) error {
		n := connectCount.Add(1)
		if n < 3 {
			return errors.New("connection refused")
		}
		return nil // succeed on 3rd attempt
	}

	r := NewReconnector(updates, connectFn, ks)

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	done := make(chan error, 1)
	go func() {
		done <- r.Start(ctx)
	}()

	updates <- StateDisconnected

	// Wait for 3 attempts: 1s wait + fail, 2s wait + fail, 4s wait + succeed = ~7s total.
	time.Sleep(8 * time.Second)

	r.Stop()
	<-done

	if connectCount.Load() < 3 {
		t.Errorf("connectFn called %d times, want >= 3", connectCount.Load())
	}
	if ks.activateCount.Load() != 1 {
		t.Errorf("kill switch activated %d times, want 1", ks.activateCount.Load())
	}
	if ks.deactivateCount.Load() != 1 {
		t.Errorf("kill switch deactivated %d times, want 1", ks.deactivateCount.Load())
	}
}

func TestReconnector_CancellationDuringBackoff(t *testing.T) {
	ks := &mockKillSwitch{}
	updates := make(chan ConnState, 1)

	connectFn := func(_ context.Context) error {
		return errors.New("always fail")
	}

	r := NewReconnector(updates, connectFn, ks)

	ctx, cancel := context.WithCancel(context.Background())

	done := make(chan error, 1)
	go func() {
		done <- r.Start(ctx)
	}()

	updates <- StateDisconnected

	// Let it attempt one reconnection, then cancel.
	time.Sleep(1500 * time.Millisecond)
	cancel()

	select {
	case err := <-done:
		if err != nil {
			t.Errorf("Start returned error: %v", err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("Start did not return after context cancellation")
	}
}

func TestReconnector_Stop(t *testing.T) {
	ks := &mockKillSwitch{}
	updates := make(chan ConnState, 1)
	connectFn := func(_ context.Context) error { return errors.New("fail") }

	r := NewReconnector(updates, connectFn, ks)

	ctx := context.Background()
	done := make(chan error, 1)
	go func() {
		done <- r.Start(ctx)
	}()

	updates <- StateDisconnected

	time.Sleep(500 * time.Millisecond)
	r.Stop()

	select {
	case err := <-done:
		if err != nil {
			t.Errorf("Start returned error: %v", err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("Start did not return after Stop")
	}
}

func TestReconnector_DoubleStart(t *testing.T) {
	ks := &mockKillSwitch{}
	updates := make(chan ConnState, 1)
	connectFn := func(_ context.Context) error { return nil }

	r := NewReconnector(updates, connectFn, ks)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	done := make(chan error, 1)
	go func() {
		done <- r.Start(ctx)
	}()

	// Let it initialize.
	time.Sleep(100 * time.Millisecond)

	// Second Start should return ErrReconnectInProgress.
	err := r.Start(ctx)
	if !errors.Is(err, ErrReconnectInProgress) {
		t.Errorf("second Start returned %v, want ErrReconnectInProgress", err)
	}

	cancel()
	<-done
}

func TestReconnector_ChannelClose(t *testing.T) {
	ks := &mockKillSwitch{}
	updates := make(chan ConnState)
	connectFn := func(_ context.Context) error { return nil }

	r := NewReconnector(updates, connectFn, ks)

	done := make(chan error, 1)
	go func() {
		done <- r.Start(context.Background())
	}()

	time.Sleep(100 * time.Millisecond)
	close(updates)

	select {
	case err := <-done:
		if err != nil {
			t.Errorf("Start returned error: %v", err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("Start did not return after channel close")
	}
}

func TestReconnector_FailoverAfterMaxRetries(t *testing.T) {
	updates := make(chan ConnState, 1)
	ks := &mockKillSwitch{}

	var connectCount atomic.Int64
	connectFn := func(_ context.Context) error {
		connectCount.Add(1)
		return errors.New("connect failed")
	}

	var failoverCalled atomic.Int64
	failoverFn := func(_ context.Context) error {
		failoverCalled.Add(1)
		return nil // failover succeeds
	}

	r := NewReconnector(updates, connectFn, ks, WithFailoverFn(failoverFn))

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	done := make(chan error, 1)
	go func() { done <- r.Start(ctx) }()

	// Trigger disconnect.
	updates <- StateDisconnected

	// Wait for failover to be called (after MaxRetriesBeforeFailover connect failures).
	deadline := time.After(15 * time.Second)
	for {
		select {
		case <-deadline:
			t.Fatalf("failover not called; connectCount=%d, failoverCalled=%d",
				connectCount.Load(), failoverCalled.Load())
		default:
		}
		if failoverCalled.Load() > 0 {
			break
		}
		time.Sleep(100 * time.Millisecond)
	}

	if connectCount.Load() < int64(MaxRetriesBeforeFailover) {
		t.Errorf("expected at least %d connect attempts before failover, got %d",
			MaxRetriesBeforeFailover, connectCount.Load())
	}

	cancel()
	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("Start did not return")
	}
}

func TestReconnector_NoFailoverIfConnectSucceeds(t *testing.T) {
	updates := make(chan ConnState, 1)
	ks := &mockKillSwitch{}

	var connectCount atomic.Int64
	connectFn := func(_ context.Context) error {
		n := connectCount.Add(1)
		if n >= 2 {
			return nil // succeed on 2nd attempt
		}
		return errors.New("connect failed")
	}

	var failoverCalled atomic.Int64
	failoverFn := func(_ context.Context) error {
		failoverCalled.Add(1)
		return nil
	}

	r := NewReconnector(updates, connectFn, ks, WithFailoverFn(failoverFn))

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	done := make(chan error, 1)
	go func() { done <- r.Start(ctx) }()

	updates <- StateDisconnected

	// Wait for reconnection to succeed (2nd attempt).
	time.Sleep(5 * time.Second)

	if failoverCalled.Load() != 0 {
		t.Errorf("failover should not be called when connect succeeds, called %d times",
			failoverCalled.Load())
	}

	cancel()
	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("Start did not return")
	}
}

func TestReconnector_FailoverFnNil_ContinuesBackoff(t *testing.T) {
	updates := make(chan ConnState, 1)
	ks := &mockKillSwitch{}

	var connectCount atomic.Int64
	connectFn := func(_ context.Context) error {
		n := connectCount.Add(1)
		if n >= 4 {
			return nil // succeed on 4th attempt (after 1s + 2s + 4s = 7s backoff)
		}
		return errors.New("connect failed")
	}

	// No failoverFn — nil: backoff continues past MaxRetriesBeforeFailover
	r := NewReconnector(updates, connectFn, ks)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	done := make(chan error, 1)
	go func() { done <- r.Start(ctx) }()

	updates <- StateDisconnected

	// Wait for reconnection to succeed after retries (needs > MaxRetriesBeforeFailover).
	deadline := time.After(20 * time.Second)
	for {
		select {
		case <-deadline:
			t.Fatalf("reconnect never succeeded; connectCount=%d", connectCount.Load())
		default:
		}
		if connectCount.Load() >= 4 {
			break
		}
		time.Sleep(200 * time.Millisecond)
	}

	// Verify connectCount > MaxRetriesBeforeFailover (no failover was triggered, kept retrying).
	if connectCount.Load() < int64(MaxRetriesBeforeFailover) {
		t.Errorf("expected at least %d retries, got %d", MaxRetriesBeforeFailover, connectCount.Load())
	}

	cancel()
	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("Start did not return")
	}
}
