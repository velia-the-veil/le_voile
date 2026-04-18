// Package leakcheck provides WebRTC IP leak detection by sending STUN
// Binding Requests to public STUN servers and comparing the discovered
// IP with the expected tunnel IP.
package leakcheck

import (
	"context"
	"errors"
	"sync"
	"time"

	"github.com/velia-the-veil/le_voile/internal/tunnel"
)

// ErrSchedulerAlreadyRunning is returned when Start is called on an already-running scheduler.
var ErrSchedulerAlreadyRunning = errors.New("leakcheck: scheduler already running")

// TunnelStateQuerier abstracts tunnel.StateManager for test isolation.
type TunnelStateQuerier interface {
	Get() tunnel.ConnState
}

// leakCheckerIface is the internal interface for the leak checker (enables test mocking).
type leakCheckerIface interface {
	RunFullCheck(ctx context.Context) (*FullLeakReport, error)
}

// maxConsecutiveSkips is the maximum number of consecutive skip cycles
// before the scheduler considers the tunnel stuck in a non-connected state.
const maxConsecutiveSkips = 6

// PeriodicScheduler runs WebRTC leak checks at a fixed interval.
// It skips checks when the tunnel is not connected.
// On leak detection it invokes onLeak; on recovery it invokes onRecovery.
//
// Story 6.1 refactor: the kill-switch skip condition was removed — post-Epic-2,
// the kill switch and the TUN capture coexist, and STUN Binding Requests must
// flow through the TUN precisely to validate that the chain is intact.
type PeriodicScheduler struct {
	interval    time.Duration
	checker     leakCheckerIface
	tunnelState TunnelStateQuerier
	onLeak      func(report *FullLeakReport)
	onRecovery  func()

	mu                sync.Mutex
	running           bool
	cancel            context.CancelFunc
	done              chan struct{}
	lastResult        *FullLeakReport
	lastCheckAt       time.Time
	lastWasLeak       bool
	consecutiveSkips  int

	// checkMu serializes runCheck execution so that a user-triggered
	// TriggerCheck cannot run in parallel with the periodic tick. Two
	// concurrent RunFullCheck invocations would dial the same STUN
	// servers twice and race on writes to lastResult. Story 6.2 M1 fix.
	checkMu sync.Mutex
}

// NewPeriodicScheduler creates a PeriodicScheduler.
// Panics if checker is nil or interval is zero (programming error).
func NewPeriodicScheduler(
	interval time.Duration,
	checker *WebRTCLeakChecker,
	ts TunnelStateQuerier,
	onLeak func(*FullLeakReport),
	onRecovery func(),
) *PeriodicScheduler {
	if checker == nil {
		panic("leakcheck: NewPeriodicScheduler: checker must not be nil")
	}
	if interval == 0 {
		panic("leakcheck: NewPeriodicScheduler: interval must not be zero")
	}
	if ts == nil {
		panic("leakcheck: NewPeriodicScheduler: TunnelStateQuerier must not be nil")
	}
	return &PeriodicScheduler{
		interval:    interval,
		checker:     checker,
		tunnelState: ts,
		onLeak:      onLeak,
		onRecovery:  onRecovery,
	}
}

// Start begins the periodic leak check loop. Returns ErrSchedulerAlreadyRunning if already active.
// Returns nil immediately after spawning the background goroutine.
func (p *PeriodicScheduler) Start(ctx context.Context) error {
	p.mu.Lock()
	if p.running {
		p.mu.Unlock()
		return ErrSchedulerAlreadyRunning
	}
	ctx, p.cancel = context.WithCancel(ctx)
	p.done = make(chan struct{})
	p.running = true
	p.mu.Unlock()

	go func() {
		defer func() {
			p.mu.Lock()
			p.running = false
			p.cancel = nil
			close(p.done)
			p.mu.Unlock()
		}()
		ticker := time.NewTicker(p.interval)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				p.runCheck(ctx)
			}
		}
	}()
	return nil
}

// Stop halts the scheduler and waits for the background goroutine to exit.
// Safe to call even if the scheduler was never started.
func (p *PeriodicScheduler) Stop() {
	p.mu.Lock()
	cancel := p.cancel
	done := p.done
	p.mu.Unlock()

	if cancel != nil {
		cancel()
	}
	if done != nil {
		<-done
	}
}

// TriggerCheck triggers an immediate leak check outside the normal schedule.
// Useful for re-checking after a detected leak. Blocks until the check completes.
func (p *PeriodicScheduler) TriggerCheck(ctx context.Context) {
	p.runCheck(ctx)
}

// LastResult returns the last check result and its timestamp (thread-safe).
// Returns nil, zero time if no check has been executed yet.
func (p *PeriodicScheduler) LastResult() (*FullLeakReport, time.Time) {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.lastResult, p.lastCheckAt
}

// ConsecutiveSkips returns the number of consecutive skipped check cycles. Thread-safe.
func (p *PeriodicScheduler) ConsecutiveSkips() int {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.consecutiveSkips
}

// ForTestSetLastResult seeds the cached LastResult/LastCheckAt so tests can
// exercise the IPC-handler pass-through paths (fillLeakStatus) without
// running a real RunFullCheck cycle. Not for production use.
func (p *PeriodicScheduler) ForTestSetLastResult(r *FullLeakReport, when time.Time) {
	p.mu.Lock()
	p.lastResult = r
	p.lastCheckAt = when
	p.mu.Unlock()
}

// runCheck executes a single leak check, applying skip conditions and invoking callbacks.
// Concurrent calls (e.g. periodic tick racing with TriggerCheck) are serialized via
// checkMu — a TryLock ensures the second caller drops rather than queueing, which
// avoids duplicate STUN dials under user-clicked refresh.
func (p *PeriodicScheduler) runCheck(ctx context.Context) {
	if !p.checkMu.TryLock() {
		// Another goroutine is already running RunFullCheck — drop this
		// invocation. The concurrent call will update lastResult shortly.
		return
	}
	defer p.checkMu.Unlock()
	// Skip when tunnel is not fully connected — STUN needs the TUN pump up
	// to reach the relay and the public STUN server.
	if p.tunnelState.Get() != tunnel.StateConnected {
		p.mu.Lock()
		p.consecutiveSkips++
		p.mu.Unlock()
		return
	}

	checkCtx, cancel := context.WithTimeout(ctx, 25*time.Second)
	defer cancel()

	report, err := p.checker.RunFullCheck(checkCtx)
	if err != nil {
		// Transient network error OR DoH outage (story 6.2) — count the
		// miss so consecutiveSkips eventually trips the stuck-check alarm
		// (maxConsecutiveSkips). Without this, a persistent DoH failure
		// would leave lastResult at nil forever with no operator signal.
		p.mu.Lock()
		p.consecutiveSkips++
		p.mu.Unlock()
		return
	}

	p.mu.Lock()
	p.lastResult = report
	p.lastCheckAt = time.Now()
	p.consecutiveSkips = 0
	wasLeak := p.lastWasLeak
	p.lastWasLeak = report.Status == statusLeakDetected
	p.mu.Unlock()

	if report.Status == statusLeakDetected {
		// Leak detected — notify caller.
		if p.onLeak != nil {
			p.onLeak(report)
		}
	} else if wasLeak && p.onRecovery != nil {
		// Recovery: previous check was a leak, this one is ok.
		p.onRecovery()
	}
}
