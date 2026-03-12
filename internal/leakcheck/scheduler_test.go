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

type mockKillSwitch struct {
	active bool
}

func (m *mockKillSwitch) IsActive() bool { return m.active }

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
	return &FullLeakReport{Status: statusPass}, nil
}

func (m *mockChecker) calls() int { return int(atomic.LoadInt32(&m.callCount)) }

// newSchedulerForTest constructs a PeriodicScheduler with the internal interface,
// bypassing the public constructor's panic guards.
func newSchedulerForTest(
	interval time.Duration,
	checker leakCheckerIface,
	ks KillSwitchQuerier,
	ts TunnelStateQuerier,
	onLeak func(*FullLeakReport),
	onRecovery func(),
) *PeriodicScheduler {
	return &PeriodicScheduler{
		interval:    interval,
		checker:     checker,
		killSwitch:  ks,
		tunnelState: ts,
		onLeak:      onLeak,
		onRecovery:  onRecovery,
	}
}

// --- Tests ---

// TestPeriodicScheduler_PassSilent: checker returns "pass" → onLeak never called (AC2).
func TestPeriodicScheduler_PassSilent(t *testing.T) {
	checker := newMockChecker(&FullLeakReport{Status: statusPass})
	ks := &mockKillSwitch{active: false}
	ts := &mockTunnelState{state: tunnel.StateConnected}

	leakCalled := false
	p := newSchedulerForTest(10*time.Second, checker, ks, ts,
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
	if result.Status != statusPass {
		t.Errorf("expected Status=pass, got %s", result.Status)
	}
	if checkAt.IsZero() {
		t.Error("lastCheckAt should not be zero after a check")
	}
}

// TestPeriodicScheduler_FailCallsOnLeak: checker returns "fail" → onLeak called exactly once (AC3).
func TestPeriodicScheduler_FailCallsOnLeak(t *testing.T) {
	checker := newMockChecker(&FullLeakReport{Status: statusFail})
	ks := &mockKillSwitch{active: false}
	ts := &mockTunnelState{state: tunnel.StateConnected}

	var leakCount int32
	p := newSchedulerForTest(10*time.Second, checker, ks, ts,
		func(_ *FullLeakReport) { atomic.AddInt32(&leakCount, 1) },
		func() {})

	p.runCheck(context.Background())

	if n := atomic.LoadInt32(&leakCount); n != 1 {
		t.Errorf("expected onLeak called 1 time, got %d", n)
	}
	result, _ := p.LastResult()
	if result == nil || result.Status != statusFail {
		t.Errorf("expected LastResult.Status=fail, got %v", result)
	}
}

// TestPeriodicScheduler_SkipsWhenKillSwitchActive: kill switch active → RunFullCheck never called (AC5).
func TestPeriodicScheduler_SkipsWhenKillSwitchActive(t *testing.T) {
	checker := newMockChecker()
	ks := &mockKillSwitch{active: true}
	ts := &mockTunnelState{state: tunnel.StateConnected}

	p := newSchedulerForTest(10*time.Second, checker, ks, ts, nil, nil)

	p.runCheck(context.Background())

	if checker.calls() != 0 {
		t.Errorf("RunFullCheck should not be called when kill switch is active, got %d calls", checker.calls())
	}
	result, _ := p.LastResult()
	if result != nil {
		t.Error("LastResult should be nil when check was skipped")
	}
}

// TestPeriodicScheduler_SkipsWhenTunnelDisconnected: tunnel disconnected → RunFullCheck never called (AC6).
func TestPeriodicScheduler_SkipsWhenTunnelDisconnected(t *testing.T) {
	checker := newMockChecker()
	ks := &mockKillSwitch{active: false}
	ts := &mockTunnelState{state: tunnel.StateDisconnected}

	p := newSchedulerForTest(10*time.Second, checker, ks, ts, nil, nil)

	p.runCheck(context.Background())

	if checker.calls() != 0 {
		t.Errorf("RunFullCheck should not be called when tunnel is disconnected, got %d calls", checker.calls())
	}
}

// TestPeriodicScheduler_RecoveryCallback: fail then pass → onRecovery called exactly once.
func TestPeriodicScheduler_RecoveryCallback(t *testing.T) {
	checker := newMockChecker(
		&FullLeakReport{Status: statusFail},
		&FullLeakReport{Status: statusPass},
	)
	ks := &mockKillSwitch{active: false}
	ts := &mockTunnelState{state: tunnel.StateConnected}

	var recoverCount int32
	p := newSchedulerForTest(10*time.Second, checker, ks, ts,
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
	ks := &mockKillSwitch{active: false}
	ts := &mockTunnelState{state: tunnel.StateConnected}

	// Use a large interval so no tick fires during the test.
	p := newSchedulerForTest(10*time.Minute, checker, ks, ts, nil, nil)

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

// TestPeriodicScheduler_TriggerCheck: TriggerCheck executes an immediate check outside the normal tick (M2 coverage: AC4 re-test path).
func TestPeriodicScheduler_TriggerCheck(t *testing.T) {
	checker := newMockChecker(&FullLeakReport{Status: statusFail})
	ks := &mockKillSwitch{active: false}
	ts := &mockTunnelState{state: tunnel.StateConnected}

	var leakCount int32
	p := newSchedulerForTest(10*time.Second, checker, ks, ts,
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
	if result == nil || result.Status != statusFail {
		t.Errorf("expected LastResult.Status=fail, got %v", result)
	}
	if checkAt.IsZero() {
		t.Error("LastResult checkAt should not be zero after TriggerCheck")
	}
}

// TestPeriodicScheduler_TriggerCheck_SkipsWhenKillSwitchActive: TriggerCheck respects kill switch skip (AC4+AC5 interaction).
func TestPeriodicScheduler_TriggerCheck_SkipsWhenKillSwitchActive(t *testing.T) {
	checker := newMockChecker(&FullLeakReport{Status: statusPass})
	ks := &mockKillSwitch{active: true} // kill switch active
	ts := &mockTunnelState{state: tunnel.StateConnected}

	p := newSchedulerForTest(10*time.Second, checker, ks, ts, nil, nil)

	p.TriggerCheck(context.Background())

	// When kill switch is active, TriggerCheck must not run the check.
	if checker.calls() != 0 {
		t.Errorf("expected 0 RunFullCheck calls when kill switch active, got %d", checker.calls())
	}
}

// TestPeriodicScheduler_StartAlreadyRunning: second Start returns ErrSchedulerAlreadyRunning.
func TestPeriodicScheduler_StartAlreadyRunning(t *testing.T) {
	checker := newMockChecker()
	ks := &mockKillSwitch{active: false}
	ts := &mockTunnelState{state: tunnel.StateConnected}

	p := newSchedulerForTest(10*time.Minute, checker, ks, ts, nil, nil)

	ctx := context.Background()
	if err := p.Start(ctx); err != nil {
		t.Fatalf("first Start failed: %v", err)
	}
	defer p.Stop()

	if err := p.Start(ctx); err != ErrSchedulerAlreadyRunning {
		t.Errorf("expected ErrSchedulerAlreadyRunning, got: %v", err)
	}
}

// TestNewPeriodicScheduler_PanicsOnNilKillSwitch: nil KillSwitchQuerier panics at construction.
func TestNewPeriodicScheduler_PanicsOnNilKillSwitch(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Error("expected panic for nil KillSwitchQuerier, got none")
		}
	}()
	checker := NewWebRTCLeakChecker(func(_ context.Context) (net.IP, error) { return nil, nil })
	NewPeriodicScheduler(10*time.Second, checker, nil, &mockTunnelState{state: tunnel.StateConnected}, nil, nil)
}

// TestNewPeriodicScheduler_PanicsOnNilTunnelState: nil TunnelStateQuerier panics at construction.
func TestNewPeriodicScheduler_PanicsOnNilTunnelState(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Error("expected panic for nil TunnelStateQuerier, got none")
		}
	}()
	checker := NewWebRTCLeakChecker(func(_ context.Context) (net.IP, error) { return nil, nil })
	NewPeriodicScheduler(10*time.Second, checker, &mockKillSwitch{active: false}, nil, nil, nil)
}
