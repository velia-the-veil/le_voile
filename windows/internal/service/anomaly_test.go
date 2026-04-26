//go:build windows

package service

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/velia-the-veil/le_voile/internal/anomaly"
	"github.com/velia-the-veil/le_voile/internal/firewall"
	"github.com/velia-the-veil/le_voile/internal/routing"
	"github.com/velia-the-veil/le_voile/internal/tun"
)

// captureLogger records every Started/Succeeded/Failed event so tests
// can assert on ordering and values without reaching into platform log
// sinks.
type captureLogger struct {
	mu        sync.Mutex
	started   []anomaly.Reason
	succeeded []int64
	failed    []anomaly.ErrorCategory
	closed    bool
}

func (c *captureLogger) Started(r anomaly.Reason) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.started = append(c.started, r)
}

func (c *captureLogger) Succeeded(ms int64) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.succeeded = append(c.succeeded, ms)
}

func (c *captureLogger) Failed(cat anomaly.ErrorCategory) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.failed = append(c.failed, cat)
}

func (c *captureLogger) Close() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.closed = true
	return nil
}

// captureNotifier mirrors captureLogger but exposes the same counters
// through the anomaly.Notifier interface.
type captureNotifier struct {
	mu              sync.Mutex
	startedCount    int
	succeededCount  int
	failedCount     int
	lastStartedReason anomaly.Reason
}

func (c *captureNotifier) Started(r anomaly.Reason) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.startedCount++
	c.lastStartedReason = r
}

func (c *captureNotifier) Succeeded(int64)               {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.succeededCount++
}

func (c *captureNotifier) Failed(anomaly.ErrorCategory) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.failedCount++
}

func (c *captureNotifier) counts() (int, int, int) {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.startedCount, c.succeededCount, c.failedCount
}

// --- minimal mocks for recoverTUN dependencies ---

type fakeTUN struct {
	name   string
	mtu    int
	closed atomic.Bool
}

func (f *fakeTUN) Name() string                { return f.name }
func (f *fakeTUN) MTU() int                    { return f.mtu }
func (f *fakeTUN) Close() error                { f.closed.Store(true); return nil }
func (f *fakeTUN) Read(_ []byte) (int, error)  { return 0, io.EOF }
func (f *fakeTUN) Write(_ []byte) (int, error) { return 0, io.ErrClosedPipe }

type fakeRouteMgr struct {
	setupCalls    atomic.Int32
	teardownCalls atomic.Int32
	cleanupCalls  atomic.Int32
	setupErr      error
	mu            sync.Mutex
	lastRelayIP   net.IP
}

func (f *fakeRouteMgr) Setup(_ string, relayIP net.IP, _ net.IP, _ string) error {
	f.setupCalls.Add(1)
	f.mu.Lock()
	f.lastRelayIP = relayIP
	f.mu.Unlock()
	return f.setupErr
}

func (f *fakeRouteMgr) LastRelayIP() net.IP {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.lastRelayIP
}

func (f *fakeRouteMgr) Teardown() error         { f.teardownCalls.Add(1); return nil }
func (f *fakeRouteMgr) Cleanup() error          { f.cleanupCalls.Add(1); return nil }
func (f *fakeRouteMgr) Saved() *routing.SavedRoutes { return nil }

// fakeFirewall tracks Activate/Deactivate calls and IsActive polling.
// observedInactive counts every IsActive(ctx) reply that returned false
// so tests can prove the kill switch never dropped during recovery (AC3).
type fakeFirewall struct {
	mu               sync.Mutex
	activateCalls    int
	deactivateCalls  int
	isActive         bool
	observedInactive int
	lastRelayIP      net.IP
}

func (f *fakeFirewall) Activate(_ context.Context, p firewall.ActivateParams) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.activateCalls++
	f.lastRelayIP = p.RelayIP
	f.isActive = true
	return nil
}

// LastRelayIP returns the RelayIP from the most recent Activate call.
func (f *fakeFirewall) LastRelayIP() net.IP {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.lastRelayIP
}

func (f *fakeFirewall) Deactivate(_ context.Context) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.deactivateCalls++
	f.isActive = false
	return nil
}

func (f *fakeFirewall) IsActive(_ context.Context) (bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if !f.isActive {
		f.observedInactive++
	}
	return f.isActive, nil
}

func (f *fakeFirewall) CleanupOrphans(_ context.Context) (int, error) { return 0, nil }
func (f *fakeFirewall) SetIPv6Policy(_ context.Context, _ bool) error { return nil }
func (f *fakeFirewall) AlteredCh() <-chan struct{}                    { return nil }

// --- test helpers ---

// newTestProgramForAnomaly builds a Program with enough wiring to run
// RecoverFromAnomaly end-to-end without touching the OS. Returns the
// Program plus the injected captureLogger/captureNotifier so callers can
// assert on events, and the fakeFirewall so AC3 checks can peek at its
// state.
func newTestProgramForAnomaly(t *testing.T) (*Program, *captureLogger, *captureNotifier, *fakeFirewall) {
	t.Helper()

	// Redirect serviceStderr to a buffer so tests stay quiet.
	origStderr := serviceStderr
	serviceStderr = &bytes.Buffer{}
	t.Cleanup(func() { serviceStderr = origStderr })

	fw := &fakeFirewall{isActive: true}
	origFirewallFactory := firewallFactory
	firewallFactory = func(_ firewall.Logger, _ firewall.Options) firewall.Firewall { return fw }
	t.Cleanup(func() { firewallFactory = origFirewallFactory })

	origTunFactory := tunFactory
	tunFactory = func(name string, mtu int) (tun.Device, error) {
		return &fakeTUN{name: name, mtu: mtu}, nil
	}
	t.Cleanup(func() { tunFactory = origTunFactory })

	origRoutingFactory := routingFactory
	routingFactory = func() routing.RouteManager { return &fakeRouteMgr{} }
	t.Cleanup(func() { routingFactory = origRoutingFactory })

	origCaptureOriginalRoute := captureOriginalRouteFunc
	captureOriginalRouteFunc = func() (net.IP, string, error) {
		return net.IPv4(10, 0, 0, 1), "eth0", nil
	}
	t.Cleanup(func() { captureOriginalRouteFunc = origCaptureOriginalRoute })

	p := NewProgram(Config{
		RelayDomain:     "example.test",
		FirewallEnabled: true,
	})
	p.tunDev = &fakeTUN{name: "levoile0", mtu: 1420}
	p.firewallRelayIP.Store(net.IPv4(203, 0, 113, 5))
	p.firewallMgr = fw

	cl := &captureLogger{}
	cn := &captureNotifier{}
	p.SetAnomalyLogger(cl)
	p.SetAnomalyNotifier(cn)

	return p, cl, cn, fw
}

func TestRecoverFromAnomaly_HappyPath_UpdatesStateAndLogs(t *testing.T) {
	p, cl, cn, _ := newTestProgramForAnomaly(t)

	// Before: state is idle.
	if p.AnomalyActive() {
		t.Fatal("anomaly should not be active before first RecoverFromAnomaly")
	}
	if p.AnomalyReason() != "" {
		t.Fatalf("expected empty reason, got %q", p.AnomalyReason())
	}

	err := p.RecoverFromAnomaly(context.Background(), anomaly.ReasonLeakDetected)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// After: state is cleared, logger/notifier saw one full cycle.
	if p.AnomalyActive() {
		t.Error("anomaly should be inactive after recovery completes")
	}
	if p.AnomalyReason() != "" {
		t.Errorf("expected empty reason after recovery, got %q", p.AnomalyReason())
	}

	cl.mu.Lock()
	defer cl.mu.Unlock()
	if len(cl.started) != 1 || cl.started[0] != anomaly.ReasonLeakDetected {
		t.Errorf("logger.Started: want [leak_detected], got %v", cl.started)
	}
	if len(cl.succeeded) != 1 {
		t.Errorf("logger.Succeeded: want 1 call, got %d", len(cl.succeeded))
	}
	if len(cl.failed) != 0 {
		t.Errorf("logger.Failed: want 0 calls, got %d", len(cl.failed))
	}

	s, sc, f := cn.counts()
	if s != 1 || sc != 1 || f != 0 {
		t.Errorf("notifier counts: want started=1 succeeded=1 failed=0; got started=%d succeeded=%d failed=%d", s, sc, f)
	}
}

func TestRecoverFromAnomaly_SerializesConcurrent(t *testing.T) {
	p, cl, _, _ := newTestProgramForAnomaly(t)

	// Deterministic barrier: the winner (first to acquire the mutex)
	// signals via `started` then blocks until all other callers have
	// drained (TryLock-failed and returned). Only then does main
	// release the winner. This avoids the flaky pattern where
	// goroutines schedule one-at-a-time after the winner releases.
	started := make(chan struct{})
	release := make(chan struct{})
	origFactory := tunFactory
	tunFactory = func(name string, mtu int) (tun.Device, error) {
		close(started)
		<-release
		return &fakeTUN{name: name, mtu: mtu}, nil
	}
	t.Cleanup(func() { tunFactory = origFactory })

	const callers = 10
	drain := make(chan struct{}, callers)
	for i := 0; i < callers; i++ {
		go func() {
			defer func() { drain <- struct{}{} }()
			_ = p.RecoverFromAnomaly(context.Background(), anomaly.ReasonLeakDetected)
		}()
	}

	// Wait for the winner to enter tunFactory.
	select {
	case <-started:
	case <-time.After(2 * time.Second):
		close(release)
		t.Fatal("no caller entered recoverTUN within 2s")
	}

	// The winner is blocked in tunFactory; the other callers must
	// TryLock-fail and drain quickly. If any of them stalls waiting on
	// the mutex, the timeout catches the bug.
	for i := 0; i < callers-1; i++ {
		select {
		case <-drain:
		case <-time.After(2 * time.Second):
			close(release)
			t.Fatalf("only %d/%d non-winners drained within 2s — TryLock not releasing", i, callers-1)
		}
	}

	// All losers are done; release the winner and wait for its drain.
	close(release)
	select {
	case <-drain:
	case <-time.After(2 * time.Second):
		t.Fatal("winner never completed recovery")
	}

	cl.mu.Lock()
	defer cl.mu.Unlock()
	if len(cl.started) != 1 {
		t.Errorf("expected exactly 1 Started event (TryLock dropped %d), got %d", callers-1, len(cl.started))
	}
	if len(cl.succeeded) != 1 {
		t.Errorf("expected exactly 1 Succeeded event, got %d", len(cl.succeeded))
	}
}

func TestRecoverFromAnomaly_FirewallStaysActive(t *testing.T) {
	p, _, _, fw := newTestProgramForAnomaly(t)

	// Poll the firewall state throughout the recovery to catch any
	// false->true->false transitions. The test poller runs in parallel
	// with RecoverFromAnomaly; observedInactive is bumped by fakeFirewall
	// every time IsActive returns false.
	stopPoll := make(chan struct{})
	go func() {
		t := time.NewTicker(1 * time.Millisecond)
		defer t.Stop()
		for {
			select {
			case <-stopPoll:
				return
			case <-t.C:
				_, _ = fw.IsActive(context.Background())
			}
		}
	}()

	if err := p.RecoverFromAnomaly(context.Background(), anomaly.ReasonTUNAltered); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	close(stopPoll)

	fw.mu.Lock()
	defer fw.mu.Unlock()
	if fw.deactivateCalls != 0 {
		t.Errorf("AC3 violation: firewall.Deactivate called %d times during recovery", fw.deactivateCalls)
	}
	if fw.observedInactive != 0 {
		t.Errorf("AC3 violation: firewall observed inactive %d times during recovery", fw.observedInactive)
	}
	if fw.activateCalls < 1 {
		t.Errorf("expected at least 1 Activate call (recovery re-activates idempotently), got %d", fw.activateCalls)
	}
}

func TestRecoverFromAnomaly_PropagatesTunnelConnectError(t *testing.T) {
	p, cl, cn, _ := newTestProgramForAnomaly(t)

	// We don't have a real tunnel.Client here — recoverTUN's step 4 is
	// only executed when p.tunnelClient != nil. Intercept via a custom
	// tunFactory that returns an error so recoverTUN fails in step 1
	// (tun_create_failed category). This exercises the Failed branch
	// without needing a tunnel mock.
	wantErr := errors.New("tun.New: permission denied")
	origFactory := tunFactory
	tunFactory = func(_ string, _ int) (tun.Device, error) {
		return nil, wantErr
	}
	t.Cleanup(func() { tunFactory = origFactory })

	err := p.RecoverFromAnomaly(context.Background(), anomaly.ReasonManual)
	if err == nil {
		t.Fatal("expected error from RecoverFromAnomaly, got nil")
	}

	// State is cleared even on failure.
	if p.AnomalyActive() {
		t.Error("anomaly should be inactive after recovery failure")
	}

	cl.mu.Lock()
	defer cl.mu.Unlock()
	if len(cl.failed) != 1 || cl.failed[0] != anomaly.CategoryTUNCreateFailed {
		t.Errorf("logger.Failed: want [tun_create_failed], got %v", cl.failed)
	}
	if len(cl.succeeded) != 0 {
		t.Errorf("logger.Succeeded: want 0 calls on failure, got %d", len(cl.succeeded))
	}

	_, sc, f := cn.counts()
	if sc != 0 || f != 1 {
		t.Errorf("notifier: want succeeded=0 failed=1, got succeeded=%d failed=%d", sc, f)
	}
}

func TestRecoverFromAnomaly_SetsReasonWhileRunning(t *testing.T) {
	p, _, _, _ := newTestProgramForAnomaly(t)

	// Install a slow tunFactory that blocks until we release it, then
	// capture AnomalyActive()/AnomalyReason() mid-flight.
	release := make(chan struct{})
	inside := make(chan struct{})
	origFactory := tunFactory
	tunFactory = func(name string, mtu int) (tun.Device, error) {
		close(inside)
		<-release
		return &fakeTUN{name: name, mtu: mtu}, nil
	}
	t.Cleanup(func() { tunFactory = origFactory })

	done := make(chan error, 1)
	go func() {
		done <- p.RecoverFromAnomaly(context.Background(), anomaly.ReasonTUNAltered)
	}()

	select {
	case <-inside:
	case <-time.After(2 * time.Second):
		close(release)
		t.Fatal("recoverTUN didn't reach tunFactory")
	}
	if !p.AnomalyActive() {
		t.Error("AnomalyActive() should be true while recovery is running")
	}
	if p.AnomalyReason() != string(anomaly.ReasonTUNAltered) {
		t.Errorf("AnomalyReason(): want %q, got %q", anomaly.ReasonTUNAltered, p.AnomalyReason())
	}

	close(release)
	if err := <-done; err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if p.AnomalyActive() {
		t.Error("AnomalyActive() should clear after recovery completes")
	}
}

func TestRecoverFromAnomaly_ContextCancelledBeforeStart(t *testing.T) {
	p, cl, _, _ := newTestProgramForAnomaly(t)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	err := p.RecoverFromAnomaly(ctx, anomaly.ReasonManual)
	if err == nil {
		t.Fatal("expected error when ctx is already cancelled")
	}

	cl.mu.Lock()
	defer cl.mu.Unlock()
	if len(cl.started) != 0 {
		t.Errorf("no Started event should fire when ctx is cancelled, got %d", len(cl.started))
	}
}

func TestEnsureAnomalyNotifier_FallsBackToNop(t *testing.T) {
	p := NewProgram(Config{})
	if got := p.ensureAnomalyNotifier(); got == nil {
		t.Fatal("ensureAnomalyNotifier returned nil; want NopNotifier")
	} else if _, ok := got.(anomaly.NopNotifier); !ok {
		t.Fatalf("expected NopNotifier fallback, got %T", got)
	}
}

// Review-fix H2 regression guard. ensureAnomalyLogger must not hold
// p.mu during the I/O-heavy anomaly.NewLogger call — otherwise any
// handler contending on p.mu (connect, get_status…) would stall for
// the duration of a syslog / Event Log system call.
//
// The assertion is surgical: we swap in a slow-building factory that
// blocks until we release it, then from a parallel goroutine we try to
// acquire p.mu. If p.mu is free while NewLogger is running, H2 holds.
// If it's held, this sub-goroutine blocks until the barrier releases
// and our timeout fires.
func TestEnsureAnomalyLogger_DoesNotHoldMuDuringConstruction(t *testing.T) {
	p := NewProgram(Config{})

	barrier := make(chan struct{})
	entered := make(chan struct{})
	prev := anomalyNewLoggerFactory
	anomalyNewLoggerFactory = func() anomaly.Logger {
		close(entered)
		<-barrier
		return &captureLogger{}
	}
	t.Cleanup(func() { anomalyNewLoggerFactory = prev })

	result := make(chan anomaly.Logger, 1)
	go func() { result <- p.ensureAnomalyLogger() }()

	// Wait until the factory is actually running.
	select {
	case <-entered:
	case <-time.After(2 * time.Second):
		close(barrier)
		t.Fatal("factory was never called")
	}

	// While the factory is blocked, p.mu MUST be free. Prove it by
	// acquiring the lock from a sibling goroutine and releasing it
	// immediately.
	muFree := make(chan struct{})
	go func() {
		p.mu.Lock()
		p.mu.Unlock()
		close(muFree)
	}()

	select {
	case <-muFree:
		// H2 holds — p.mu was free during the I/O-heavy phase.
	case <-time.After(1 * time.Second):
		close(barrier)
		<-result
		t.Fatal("p.mu was held while anomaly.NewLogger was running — H2 regressed")
	}

	// Release the factory and make sure the function completes.
	close(barrier)
	select {
	case got := <-result:
		if got == nil {
			t.Fatal("ensureAnomalyLogger returned nil after barrier release")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("ensureAnomalyLogger did not return after barrier release")
	}
}

// Review-fix M3 regression guard. ForTestSetAnomaly(true, "") must
// panic to catch tests that would otherwise publish an inconsistent
// (active=true, reason="") state.
func TestForTestSetAnomaly_PanicsOnEmptyReasonWhenActive(t *testing.T) {
	p := NewProgram(Config{})
	defer func() {
		if r := recover(); r == nil {
			t.Fatal("ForTestSetAnomaly(true, \"\") did not panic — M3 regressed")
		}
	}()
	p.ForTestSetAnomaly(true, "")
}

func TestForTestSetAnomaly_DoesNotPanicOnEmptyReasonWhenInactive(t *testing.T) {
	p := NewProgram(Config{})
	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("ForTestSetAnomaly(false, \"\") panicked — want quiet clear: %v", r)
		}
	}()
	p.ForTestSetAnomaly(false, "")
	if p.AnomalyActive() {
		t.Error("active flag should stay false after clear")
	}
	if p.AnomalyReason() != "" {
		t.Errorf("reason should be cleared, got %q", p.AnomalyReason())
	}
}

// Review-fix M4 regression guard. SetAnomalyLogger must Close() the
// previous logger to avoid handle leaks. The captureLogger records a
// Close() call so we can assert the order.
func TestSetAnomalyLogger_ClosesPrevious(t *testing.T) {
	p := NewProgram(Config{})

	first := &captureLogger{}
	p.SetAnomalyLogger(first)

	second := &captureLogger{}
	p.SetAnomalyLogger(second)

	first.mu.Lock()
	wasClosed := first.closed
	first.mu.Unlock()
	if !wasClosed {
		t.Error("first logger was not Close()'d when replaced")
	}

	// Replacing by the same instance is a no-op (we check identity
	// explicitly in SetAnomalyLogger to avoid closing the live one).
	p.SetAnomalyLogger(second)
	second.mu.Lock()
	stillAlive := !second.closed
	second.mu.Unlock()
	if !stillAlive {
		t.Error("re-setting the same logger wrongly Close()'d it")
	}
}

// Sanity check: the package-level accessors exposed by the service
// package really return the atomic state rather than reading through
// p.mu, otherwise concurrent IPC polling would block during a recovery.
func TestAnomalyAccessors_AreLockFree(t *testing.T) {
	p := NewProgram(Config{})
	p.anomalyActive.Store(true)
	reason := string(anomaly.ReasonLeakDetected)
	p.anomalyReasonPtr.Store(&reason)

	// Hold the mutex and assert the accessors still return.
	p.mu.Lock()
	defer p.mu.Unlock()

	done := make(chan struct{})
	go func() {
		if !p.AnomalyActive() {
			t.Error("AnomalyActive returned false while mu is held")
		}
		if p.AnomalyReason() != string(anomaly.ReasonLeakDetected) {
			t.Errorf("AnomalyReason returned %q while mu is held", p.AnomalyReason())
		}
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(1 * time.Second):
		t.Fatal("accessors appear to acquire p.mu — they should be lock-free")
	}
}

// Compile-time guard: captureLogger and captureNotifier satisfy the
// anomaly interfaces so a future rename breaks the tests loudly.
var (
	_ anomaly.Logger   = (*captureLogger)(nil)
	_ anomaly.Notifier = (*captureNotifier)(nil)
)

