package leakcheck

import (
	"context"
	"net"
	"sync/atomic"
	"testing"
	"time"

	"github.com/velia-the-veil/le_voile/internal/tunnel"
)

// --- Mock helpers ---

type mockTunnelState struct {
	state tunnel.ConnState
}

func (m *mockTunnelState) Get() tunnel.ConnState { return m.state }

// mockChecker implements leakCheckerIface with a configurable sequence of results.
type mockChecker struct {
	reports   []*FullLeakReport
	callCount int32 // accessed atomically
}

func newMockChecker(reports ...*FullLeakReport) *mockChecker {
	return &mockChecker{reports: reports}
}

func (m *mockChecker) RunFullCheck(_ context.Context) (*FullLeakReport, error) {
	n := int(atomic.AddInt32(&m.callCount, 1)) - 1 // zero-based index
	if n < len(m.reports) {
		return m.reports[n], nil
	}
	return &FullLeakReport{Status: statusOK}, nil
}

func (m *mockChecker) calls() int { return int(atomic.LoadInt32(&m.callCount)) }

// newSchedulerForTest constructs a PeriodicScheduler with the internal interface,
// bypassing the public constructor's panic guards.
func newSchedulerForTest(
	interval time.Duration,
	checker leakCheckerIface,
	ts TunnelStateQuerier,
	onLeak func(*FullLeakReport),
	onRecovery func(),
) *PeriodicScheduler {
	return &PeriodicScheduler{
		interval:    interval,
		checker:     checker,
		tunnelState: ts,
		onLeak:      onLeak,
		onRecovery:  onRecovery,
	}
}

// erroringChecker always returns an error to exercise the scheduler's
// error-handling branch (story 6.2 M2 regression guard).
type erroringChecker struct {
	callCount int32
}

func (e *erroringChecker) RunFullCheck(_ context.Context) (*FullLeakReport, error) {
	atomic.AddInt32(&e.callCount, 1)
	return nil, errSimulatedCheckerFailure
}

func (e *erroringChecker) calls() int { return int(atomic.LoadInt32(&e.callCount)) }

var errSimulatedCheckerFailure = &simpleError{msg: "simulated checker failure"}

type simpleError struct{ msg string }

func (e *simpleError) Error() string { return e.msg }

// slowChecker blocks until a channel is closed, so two concurrent runCheck
// invocations can be orchestrated for the story-6.2 M1 serialization test.
type slowChecker struct {
	release   chan struct{}
	callCount int32
	started   chan struct{}
}

func newSlowChecker() *slowChecker {
	return &slowChecker{
		release: make(chan struct{}),
		started: make(chan struct{}, 1),
	}
}

func (s *slowChecker) RunFullCheck(ctx context.Context) (*FullLeakReport, error) {
	atomic.AddInt32(&s.callCount, 1)
	select {
	case s.started <- struct{}{}:
	default:
	}
	select {
	case <-s.release:
	case <-ctx.Done():
	}
	return &FullLeakReport{Status: statusOK}, nil
}

func (s *slowChecker) calls() int { return int(atomic.LoadInt32(&s.callCount)) }

// TestPeriodicScheduler_IncrementsSkipsOnCheckerError (Story 6.2 M2 review fix)
// — when RunFullCheck errors (e.g. DoH outage), consecutiveSkips must
// increment so the stuck-check alarm (maxConsecutiveSkips) eventually fires.
// Pre-fix, the scheduler silently returned and DoH-down meant eternal "pending"
// with no operator signal.
func TestPeriodicScheduler_IncrementsSkipsOnCheckerError(t *testing.T) {
	checker := &erroringChecker{}
	ts := &mockTunnelState{state: tunnel.StateConnected}
	p := newSchedulerForTest(10*time.Second, checker, ts, nil, nil)

	p.runCheck(context.Background())
	p.runCheck(context.Background())
	p.runCheck(context.Background())

	if got := checker.calls(); got != 3 {
		t.Errorf("checker calls = %d, want 3", got)
	}
	if got := p.ConsecutiveSkips(); got != 3 {
		t.Errorf("ConsecutiveSkips = %d after 3 checker errors, want 3 (M2 regression)", got)
	}
	if result, _ := p.LastResult(); result != nil {
		t.Errorf("LastResult should remain nil on persistent checker failure, got %+v", result)
	}
}

// TestPeriodicScheduler_SerializesConcurrentRunCheck (Story 6.2 M1 review fix)
// — when one runCheck is in flight, a second concurrent invocation MUST be
// dropped rather than dial the same STUN servers in parallel.
func TestPeriodicScheduler_SerializesConcurrentRunCheck(t *testing.T) {
	slow := newSlowChecker()
	ts := &mockTunnelState{state: tunnel.StateConnected}
	p := newSchedulerForTest(10*time.Second, slow, ts, nil, nil)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	// Launch the first check in a goroutine; it will block in RunFullCheck
	// until we close slow.release.
	go p.runCheck(ctx)

	// Wait for the first check to reach the blocking point.
	select {
	case <-slow.started:
	case <-time.After(2 * time.Second):
		t.Fatal("first runCheck never started")
	}

	// Second invocation while the first is still running — must drop, i.e.
	// RunFullCheck MUST NOT be called a second time.
	p.runCheck(ctx)

	if got := slow.calls(); got != 1 {
		t.Errorf("checker calls = %d during concurrent runs, want 1 (M1 regression — TryLock must drop second call)", got)
	}

	// Release the first check so the goroutine exits cleanly.
	close(slow.release)
}

// --- Tests ---

// TestPeriodicScheduler_PassSilent: checker returns "pass" → onLeak never called.
func TestPeriodicScheduler_PassSilent(t *testing.T) {
	checker := newMockChecker(&FullLeakReport{Status: statusOK})
	ts := &mockTunnelState{state: tunnel.StateConnected}

	leakCalled := false
	p := newSchedulerForTest(10*time.Second, checker, ts,
		func(_ *FullLeakReport) { leakCalled = true },
		func() {})

	p.runCheck(context.Background())

	if leakCalled {
		t.Error("onLeak should not be called when result is pass")
	}
	result, checkAt := p.LastResult()
	if result == nil {
		t.Fatal("LastResult should not be nil after a check")
	}
	if result.Status != statusOK {
		t.Errorf("expected Status=pass, got %s", result.Status)
	}
	if checkAt.IsZero() {
		t.Error("lastCheckAt should not be zero after a check")
	}
}

// TestPeriodicScheduler_FailCallsOnLeak: checker returns "fail" → onLeak called exactly once.
func TestPeriodicScheduler_FailCallsOnLeak(t *testing.T) {
	checker := newMockChecker(&FullLeakReport{Status: statusLeakDetected})
	ts := &mockTunnelState{state: tunnel.StateConnected}

	var leakCount int32
	p := newSchedulerForTest(10*time.Second, checker, ts,
		func(_ *FullLeakReport) { atomic.AddInt32(&leakCount, 1) },
		func() {})

	p.runCheck(context.Background())

	if n := atomic.LoadInt32(&leakCount); n != 1 {
		t.Errorf("expected onLeak called 1 time, got %d", n)
	}
	result, _ := p.LastResult()
	if result == nil || result.Status != statusLeakDetected {
		t.Errorf("expected LastResult.Status=fail, got %v", result)
	}
}

// TestPeriodicScheduler_DoesNotSkipOnKillSwitchActive: Story 6.1 — after the
// refactor, the kill switch no longer gates STUN checks. Post-Epic-2, STUN
// flows via the TUN and the check is precisely what validates the chain.
// The scheduler now only skips when the tunnel is not connected.
func TestPeriodicScheduler_DoesNotSkipOnKillSwitchActive(t *testing.T) {
	checker := newMockChecker(&FullLeakReport{Status: statusOK})
	ts := &mockTunnelState{state: tunnel.StateConnected}

	p := newSchedulerForTest(10*time.Second, checker, ts, nil, nil)

	// The scheduler no longer has a KillSwitchQuerier dependency at all —
	// if a caller had the kill switch active, runCheck should still fire.
	p.runCheck(context.Background())

	if checker.calls() != 1 {
		t.Errorf("RunFullCheck should fire once regardless of kill-switch state, got %d calls", checker.calls())
	}
}

// TestPeriodicScheduler_SkipsWhenTunnelDisconnected: tunnel disconnected → RunFullCheck never called.
func TestPeriodicScheduler_SkipsWhenTunnelDisconnected(t *testing.T) {
	checker := newMockChecker()
	ts := &mockTunnelState{state: tunnel.StateDisconnected}

	p := newSchedulerForTest(10*time.Second, checker, ts, nil, nil)

	p.runCheck(context.Background())

	if checker.calls() != 0 {
		t.Errorf("RunFullCheck should not be called when tunnel is disconnected, got %d calls", checker.calls())
	}
}

// TestPeriodicScheduler_RecoveryCallback: fail then pass → onRecovery called exactly once.
func TestPeriodicScheduler_RecoveryCallback(t *testing.T) {
	checker := newMockChecker(
		&FullLeakReport{Status: statusLeakDetected},
		&FullLeakReport{Status: statusOK},
	)
	ts := &mockTunnelState{state: tunnel.StateConnected}

	var recoverCount int32
	p := newSchedulerForTest(10*time.Second, checker, ts,
		func(_ *FullLeakReport) {},
		func() { atomic.AddInt32(&recoverCount, 1) })

	// First check: fail → sets lastWasLeak = true
	p.runCheck(context.Background())
	if n := atomic.LoadInt32(&recoverCount); n != 0 {
		t.Errorf("onRecovery should not be called after first fail, got %d", n)
	}

	// Second check: pass after fail → calls onRecovery once
	p.runCheck(context.Background())
	if n := atomic.LoadInt32(&recoverCount); n != 1 {
		t.Errorf("expected onRecovery called 1 time after recovery, got %d", n)
	}

	// Third check: pass again → no additional onRecovery
	p.runCheck(context.Background())
	if n := atomic.LoadInt32(&recoverCount); n != 1 {
		t.Errorf("onRecovery should not be called again for repeated pass, got %d", n)
	}
}

// TestPeriodicScheduler_StartStop: Start then Stop — goroutine terminates cleanly, no deadlock.
func TestPeriodicScheduler_StartStop(t *testing.T) {
	checker := newMockChecker()
	ts := &mockTunnelState{state: tunnel.StateConnected}

	// Use a large interval so no tick fires during the test.
	p := newSchedulerForTest(10*time.Minute, checker, ts, nil, nil)

	ctx := context.Background()
	if err := p.Start(ctx); err != nil {
		t.Fatalf("Start failed: %v", err)
	}

	done := make(chan struct{})
	go func() {
		p.Stop()
		close(done)
	}()

	select {
	case <-done:
		// OK — goroutine terminated
	case <-time.After(2 * time.Second):
		t.Fatal("Stop() timed out — possible deadlock")
	}

	// Verify scheduler is marked not running (can start again).
	if err := p.Start(ctx); err != nil {
		t.Errorf("expected Start to succeed after Stop, got: %v", err)
	}
	p.Stop()
}

// TestPeriodicScheduler_TriggerCheck: TriggerCheck executes an immediate check outside the normal tick.
func TestPeriodicScheduler_TriggerCheck(t *testing.T) {
	checker := newMockChecker(&FullLeakReport{Status: statusLeakDetected})
	ts := &mockTunnelState{state: tunnel.StateConnected}

	var leakCount int32
	p := newSchedulerForTest(10*time.Second, checker, ts,
		func(_ *FullLeakReport) { atomic.AddInt32(&leakCount, 1) },
		func() {})

	// TriggerCheck must execute synchronously and call RunFullCheck exactly once.
	p.TriggerCheck(context.Background())

	if checker.calls() != 1 {
		t.Errorf("expected 1 RunFullCheck call, got %d", checker.calls())
	}
	if n := atomic.LoadInt32(&leakCount); n != 1 {
		t.Errorf("expected onLeak called 1 time after fail, got %d", n)
	}
	result, checkAt := p.LastResult()
	if result == nil || result.Status != statusLeakDetected {
		t.Errorf("expected LastResult.Status=fail, got %v", result)
	}
	if checkAt.IsZero() {
		t.Error("LastResult checkAt should not be zero after TriggerCheck")
	}
}

// TestPeriodicScheduler_StartAlreadyRunning: second Start returns ErrSchedulerAlreadyRunning.
func TestPeriodicScheduler_StartAlreadyRunning(t *testing.T) {
	checker := newMockChecker()
	ts := &mockTunnelState{state: tunnel.StateConnected}

	p := newSchedulerForTest(10*time.Minute, checker, ts, nil, nil)

	ctx := context.Background()
	if err := p.Start(ctx); err != nil {
		t.Fatalf("first Start failed: %v", err)
	}
	defer p.Stop()

	if err := p.Start(ctx); err != ErrSchedulerAlreadyRunning {
		t.Errorf("expected ErrSchedulerAlreadyRunning, got: %v", err)
	}
}

// TestNewPeriodicScheduler_PanicsOnNilTunnelState: nil TunnelStateQuerier panics at construction.
func TestNewPeriodicScheduler_PanicsOnNilTunnelState(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Error("expected panic for nil TunnelStateQuerier, got none")
		}
	}()
	checker := NewWebRTCLeakChecker(func(_ context.Context) (net.IP, error) { return nil, nil })
	NewPeriodicScheduler(10*time.Second, checker, nil, nil, nil)
}

// TestNewPeriodicScheduler_PanicsOnNilChecker: nil checker panics.
func TestNewPeriodicScheduler_PanicsOnNilChecker(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Error("expected panic for nil checker, got none")
		}
	}()
	NewPeriodicScheduler(10*time.Second, nil, &mockTunnelState{state: tunnel.StateConnected}, nil, nil)
}

// TestNewPeriodicScheduler_PanicsOnZeroInterval: zero interval panics.
func TestNewPeriodicScheduler_PanicsOnZeroInterval(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Error("expected panic for zero interval, got none")
		}
	}()
	checker := NewWebRTCLeakChecker(func(_ context.Context) (net.IP, error) { return nil, nil })
	NewPeriodicScheduler(0, checker, &mockTunnelState{state: tunnel.StateConnected}, nil, nil)
}
