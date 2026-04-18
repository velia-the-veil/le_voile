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
	activateCount   atomic.Int64
	deactivateCount atomic.Int64
	activateErr     error
	deactivateErr   error
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
		{"100ms -> 200ms", 100 * time.Millisecond, 200 * time.Millisecond},
		{"200ms -> 400ms", 200 * time.Millisecond, 400 * time.Millisecond},
		{"400ms -> 800ms", 400 * time.Millisecond, 800 * time.Millisecond},
		{"800ms -> 1600ms", 800 * time.Millisecond, 1600 * time.Millisecond},
		{"1600ms -> 3200ms", 1600 * time.Millisecond, 3200 * time.Millisecond},
		{"3200ms -> 6400ms", 3200 * time.Millisecond, 6400 * time.Millisecond},
		{"6400ms -> 12800ms", 6400 * time.Millisecond, 12800 * time.Millisecond},
		{"12800ms -> 25600ms", 12800 * time.Millisecond, 25600 * time.Millisecond},
		{"25600ms -> 30s (capped)", 25600 * time.Millisecond, 30 * time.Second},
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

func TestReconnector_InitialBackoff_100ms(t *testing.T) {
	// NFR12: reconnect initiated < 1s after loss. With InitialBackoff = 100ms,
	// the Reconnector fires connectFn within ~100ms of receiving the
	// StateDisconnected notification.
	if InitialBackoff != 100*time.Millisecond {
		t.Errorf("InitialBackoff = %v, want 100ms (NFR12)", InitialBackoff)
	}
	if CircuitBreakerThreshold != 5 {
		t.Errorf("CircuitBreakerThreshold = %d, want 5", CircuitBreakerThreshold)
	}
	if MaxBackoff != 30*time.Second {
		t.Errorf("MaxBackoff = %v, want 30s", MaxBackoff)
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

	updates <- StateDisconnected

	time.Sleep(500 * time.Millisecond)

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

func TestReconnector_DetectionLatencyUnder1s(t *testing.T) {
	// NFR12: after StateDisconnected, the first connectFn call must happen
	// within 1 second. With InitialBackoff = 100ms this is a large margin,
	// but we validate the end-to-end path through the updates channel.
	ks := &mockKillSwitch{}
	updates := make(chan ConnState, 1)

	firstCall := make(chan time.Time, 1)
	connectFn := func(_ context.Context) error {
		select {
		case firstCall <- time.Now():
		default:
		}
		return nil
	}

	r := NewReconnector(updates, connectFn, ks)

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	done := make(chan error, 1)
	go func() { done <- r.Start(ctx) }()

	// Small delay to ensure Start is listening on updates.
	time.Sleep(20 * time.Millisecond)
	sent := time.Now()
	updates <- StateDisconnected

	select {
	case got := <-firstCall:
		elapsed := got.Sub(sent)
		if elapsed > time.Second {
			t.Errorf("detection latency = %v, want < 1s (NFR12)", elapsed)
		}
	case <-time.After(1500 * time.Millisecond):
		t.Fatal("connectFn was not called within 1.5s of StateDisconnected")
	}

	r.Stop()
	<-done
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
		return nil
	}

	r := NewReconnector(updates, connectFn, ks)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	done := make(chan error, 1)
	go func() {
		done <- r.Start(ctx)
	}()

	updates <- StateDisconnected

	// 100ms + 200ms + 400ms backoff before 3rd attempt = ~700ms. Wait 2s.
	time.Sleep(2 * time.Second)

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

	time.Sleep(300 * time.Millisecond)
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

	time.Sleep(200 * time.Millisecond)
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

	time.Sleep(100 * time.Millisecond)

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
		return nil
	}

	r := NewReconnector(updates, connectFn, ks, WithFailoverFn(failoverFn))

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	done := make(chan error, 1)
	go func() { done <- r.Start(ctx) }()

	updates <- StateDisconnected

	deadline := time.After(5 * time.Second)
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
		time.Sleep(50 * time.Millisecond)
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
			return nil
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

	time.Sleep(1 * time.Second)

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

func TestReconnector_CircuitBreakerAfter5Failures(t *testing.T) {
	updates := make(chan ConnState, 1)
	ks := &mockKillSwitch{}

	var connectCount atomic.Int64
	connectFn := func(_ context.Context) error {
		connectCount.Add(1)
		return errors.New("connect failed")
	}

	var hookCalled atomic.Int64
	hook := func(_ context.Context) {
		hookCalled.Add(1)
	}

	// No failoverFn — circuit breaker is the only exit path.
	r := NewReconnector(updates, connectFn, ks, WithCircuitBreakerHook(hook))

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	done := make(chan error, 1)
	go func() { done <- r.Start(ctx) }()

	updates <- StateDisconnected

	// 100+200+400+800+1600 = 3.1s cumulative backoff for 5 attempts.
	deadline := time.After(6 * time.Second)
	for {
		select {
		case <-deadline:
			t.Fatalf("circuit breaker hook not called; connectCount=%d", connectCount.Load())
		default:
		}
		if hookCalled.Load() > 0 {
			break
		}
		time.Sleep(50 * time.Millisecond)
	}

	if hookCalled.Load() != 1 {
		t.Errorf("circuit breaker hook called %d times, want 1", hookCalled.Load())
	}
	if connectCount.Load() != int64(CircuitBreakerThreshold) {
		t.Errorf("connectFn called %d times before circuit breaker, want %d",
			connectCount.Load(), CircuitBreakerThreshold)
	}
	if !r.Failed() {
		t.Error("Reconnector.Failed() = false, want true after circuit breaker")
	}

	cancel()
	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("Start did not return")
	}
}

func TestReconnector_KillSwitchActiveAfterCircuitBreaker(t *testing.T) {
	updates := make(chan ConnState, 1)
	ks := &mockKillSwitch{}

	connectFn := func(_ context.Context) error {
		return errors.New("always fail")
	}

	r := NewReconnector(updates, connectFn, ks)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	done := make(chan error, 1)
	go func() { done <- r.Start(ctx) }()

	updates <- StateDisconnected

	// Wait for circuit breaker to trip (>3.1s cumulative backoff).
	deadline := time.After(6 * time.Second)
	for {
		select {
		case <-deadline:
			t.Fatal("circuit breaker did not trip")
		default:
		}
		if r.Failed() {
			break
		}
		time.Sleep(50 * time.Millisecond)
	}

	// Kill switch MUST remain active — no Deactivate call after circuit breaker.
	if ks.deactivateCount.Load() != 0 {
		t.Errorf("kill switch deactivated %d times after circuit breaker, want 0",
			ks.deactivateCount.Load())
	}
	if !ks.IsActive() {
		t.Error("kill switch is inactive after circuit breaker; want active")
	}

	cancel()
	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("Start did not return")
	}
}

func TestReconnector_ResetAfterCircuitBreaker(t *testing.T) {
	updates := make(chan ConnState, 2)
	ks := &mockKillSwitch{}

	var connectCount atomic.Int64
	var failPhase atomic.Bool
	failPhase.Store(true)

	connectFn := func(_ context.Context) error {
		connectCount.Add(1)
		if failPhase.Load() {
			return errors.New("fail")
		}
		return nil
	}

	r := NewReconnector(updates, connectFn, ks)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	done := make(chan error, 1)
	go func() { done <- r.Start(ctx) }()

	updates <- StateDisconnected

	// Wait for circuit breaker.
	deadline := time.After(6 * time.Second)
	for {
		select {
		case <-deadline:
			t.Fatal("circuit breaker did not trip")
		default:
		}
		if r.Failed() {
			break
		}
		time.Sleep(50 * time.Millisecond)
	}

	countAtTrip := connectCount.Load()

	// Subsequent StateDisconnected must be IGNORED while failed=true.
	updates <- StateDisconnected
	time.Sleep(300 * time.Millisecond)
	if connectCount.Load() != countAtTrip {
		t.Errorf("connectFn called after circuit breaker without Reset(): count went %d -> %d",
			countAtTrip, connectCount.Load())
	}

	// Reset clears the flag; next disconnect triggers a fresh reconnect cycle.
	failPhase.Store(false)
	r.Reset()
	if r.Failed() {
		t.Error("Reconnector.Failed() = true after Reset(), want false")
	}
	updates <- StateDisconnected

	time.Sleep(500 * time.Millisecond)

	if connectCount.Load() <= countAtTrip {
		t.Errorf("connectFn was not called after Reset(); count still %d", connectCount.Load())
	}

	cancel()
	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("Start did not return")
	}
}

func TestReconnector_FailoverFailsThenCircuitBreaker(t *testing.T) {
	// When WithFailoverFn is provided but failover itself fails, the
	// Reconnector must continue backoff and eventually trip the circuit
	// breaker at CircuitBreakerThreshold.
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
		return errors.New("failover unavailable")
	}

	var hookCalled atomic.Int64
	hook := func(_ context.Context) { hookCalled.Add(1) }

	r := NewReconnector(updates, connectFn, ks,
		WithFailoverFn(failoverFn),
		WithCircuitBreakerHook(hook),
	)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	done := make(chan error, 1)
	go func() { done <- r.Start(ctx) }()

	updates <- StateDisconnected

	deadline := time.After(6 * time.Second)
	for {
		select {
		case <-deadline:
			t.Fatalf("circuit breaker not tripped; connect=%d failover=%d hook=%d",
				connectCount.Load(), failoverCalled.Load(), hookCalled.Load())
		default:
		}
		if hookCalled.Load() > 0 {
			break
		}
		time.Sleep(50 * time.Millisecond)
	}

	if failoverCalled.Load() != 1 {
		t.Errorf("failover called %d times, want exactly 1 (one-shot)", failoverCalled.Load())
	}
	if hookCalled.Load() != 1 {
		t.Errorf("circuit breaker hook called %d times, want 1", hookCalled.Load())
	}
	if connectCount.Load() != int64(CircuitBreakerThreshold) {
		t.Errorf("connectFn called %d times, want %d", connectCount.Load(), CircuitBreakerThreshold)
	}

	cancel()
	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("Start did not return")
	}
}

// Story 5.9 — WithReconnectSuccessHook fires after a successful reconnect cycle,
// after the kill switch is deactivated. Used by the service to auto-restore
// the OS-level firewall (kill switch) when degraded mode was active.
func TestReconnector_ReconnectSuccessHook_FiresOnSuccess(t *testing.T) {
	ks := &mockKillSwitch{}
	updates := make(chan ConnState, 1)
	var connectCount atomic.Int64
	var hookCount atomic.Int64
	var hookOrder atomic.Int64
	var deactivateOrder atomic.Int64

	connectFn := func(_ context.Context) error {
		connectCount.Add(1)
		return nil
	}

	// Wrap deactivate to record ordering: hook MUST run AFTER deactivate.
	wrappedKS := &mockKillSwitch{}
	origDeactivate := wrappedKS.Deactivate
	_ = origDeactivate
	hook := func(_ context.Context) {
		hookOrder.Store(deactivateOrder.Load() + 1)
		hookCount.Add(1)
	}
	// Patch ks Deactivate via a small adapter type.
	adapter := &orderedKillSwitch{
		inner: ks,
		onDeactivate: func() { deactivateOrder.Store(1) },
	}

	r := NewReconnector(updates, connectFn, adapter, WithReconnectSuccessHook(hook))

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	done := make(chan error, 1)
	go func() { done <- r.Start(ctx) }()

	updates <- StateDisconnected
	time.Sleep(500 * time.Millisecond)

	r.Stop()
	<-done

	if hookCount.Load() != 1 {
		t.Errorf("reconnect success hook called %d times, want 1", hookCount.Load())
	}
	if hookOrder.Load() != 2 {
		t.Errorf("hook order = %d, want 2 (after deactivate=1)", hookOrder.Load())
	}
	if connectCount.Load() != 1 {
		t.Errorf("connectFn called %d times, want 1", connectCount.Load())
	}
}

// orderedKillSwitch wraps a mockKillSwitch and signals on Deactivate, used to
// verify the reconnect-success hook runs strictly after kill switch deactivation.
type orderedKillSwitch struct {
	inner        *mockKillSwitch
	onDeactivate func()
}

func (o *orderedKillSwitch) Activate(ctx context.Context) error { return o.inner.Activate(ctx) }
func (o *orderedKillSwitch) Deactivate(ctx context.Context) error {
	err := o.inner.Deactivate(ctx)
	if o.onDeactivate != nil {
		o.onDeactivate()
	}
	return err
}
func (o *orderedKillSwitch) IsActive() bool { return o.inner.IsActive() }

// Story 5.9 — Hook is NOT called when reconnect fails (circuit breaker trip).
func TestReconnector_ReconnectSuccessHook_NotCalledOnCircuitBreaker(t *testing.T) {
	ks := &mockKillSwitch{}
	updates := make(chan ConnState, 1)
	var hookCount atomic.Int64
	var connectCount atomic.Int64
	var cbHookCalled atomic.Int64

	connectFn := func(_ context.Context) error {
		connectCount.Add(1)
		return errors.New("always fail")
	}
	successHook := func(_ context.Context) { hookCount.Add(1) }
	cbHook := func(_ context.Context) { cbHookCalled.Add(1) }

	r := NewReconnector(updates, connectFn, ks,
		WithReconnectSuccessHook(successHook),
		WithCircuitBreakerHook(cbHook),
	)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	done := make(chan error, 1)
	go func() { done <- r.Start(ctx) }()

	updates <- StateDisconnected

	deadline := time.After(6 * time.Second)
	for cbHookCalled.Load() == 0 {
		select {
		case <-deadline:
			t.Fatal("circuit breaker did not trip")
		default:
			time.Sleep(50 * time.Millisecond)
		}
	}

	if hookCount.Load() != 0 {
		t.Errorf("reconnect success hook fired %d times on circuit-breaker trip, want 0", hookCount.Load())
	}

	cancel()
	<-done
}

// Story 5.9 — A panicking hook does not take down the reconnect loop.
func TestReconnector_ReconnectSuccessHook_PanicRecovered(t *testing.T) {
	ks := &mockKillSwitch{}
	updates := make(chan ConnState, 2)
	var connectCount atomic.Int64

	connectFn := func(_ context.Context) error {
		connectCount.Add(1)
		return nil
	}
	hook := func(_ context.Context) { panic("hook panic") }

	r := NewReconnector(updates, connectFn, ks, WithReconnectSuccessHook(hook))

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	done := make(chan error, 1)
	go func() { done <- r.Start(ctx) }()

	// Trigger two disconnect cycles — second cycle proves the loop survived
	// the first hook panic.
	updates <- StateDisconnected
	time.Sleep(300 * time.Millisecond)
	updates <- StateDisconnected
	time.Sleep(300 * time.Millisecond)

	r.Stop()
	<-done

	if connectCount.Load() != 2 {
		t.Errorf("connectFn called %d times after hook panic, want 2 (loop survived)", connectCount.Load())
	}
}
