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

// KillSwitchQuerier abstracts dns.KillSwitch for test isolation.
type KillSwitchQuerier interface {
	IsActive() bool
}

// TunnelStateQuerier abstracts tunnel.StateManager for test isolation.
type TunnelStateQuerier interface {
	Get() tunnel.ConnState
}

// leakCheckerIface is the internal interface for the leak checker (enables test mocking).
type leakCheckerIface interface {
	RunFullCheck(ctx context.Context) (*FullLeakReport, error)
}

// PeriodicScheduler runs WebRTC leak checks at a fixed interval.
// It skips checks when the kill switch is active or the tunnel is not connected.
// On leak detection it invokes onLeak; on recovery it invokes onRecovery.
type PeriodicScheduler struct {
	interval    time.Duration
	checker     leakCheckerIface
	killSwitch  KillSwitchQuerier
	tunnelState TunnelStateQuerier
	onLeak      func(report *FullLeakReport)
	onRecovery  func()

	mu          sync.Mutex
	running     bool
	cancel      context.CancelFunc
	done        chan struct{}
	lastResult  *FullLeakReport
	lastCheckAt time.Time
	lastWasLeak bool
}

// NewPeriodicScheduler creates a PeriodicScheduler.
// Panics if checker is nil or interval is zero (programming error).
func NewPeriodicScheduler(
	interval time.Duration,
	checker *WebRTCLeakChecker,
	ks KillSwitchQuerier,
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
	return &PeriodicScheduler{
		interval:    interval,
		checker:     checker,
		killSwitch:  ks,
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

// runCheck executes a single leak check, applying skip conditions and invoking callbacks.
func (p *PeriodicScheduler) runCheck(ctx context.Context) {
	// Skip when kill switch is active (AC5): no traffic outside tunnel.
	if p.killSwitch.IsActive() {
		return
	}
	// Skip when tunnel is not fully connected (AC6).
	if p.tunnelState.Get() != tunnel.StateConnected {
		return
	}

	checkCtx, cancel := context.WithTimeout(ctx, 25*time.Second)
	defer cancel()

	report, err := p.checker.RunFullCheck(checkCtx)
	if err != nil {
		// Transient network error — skip silently (no log, no alert).
		return
	}

	p.mu.Lock()
	p.lastResult = report
	p.lastCheckAt = time.Now()
	wasLeak := p.lastWasLeak
	p.lastWasLeak = report.Status == statusFail
	p.mu.Unlock()

	if report.Status == statusFail {
		// Leak detected — notify caller (AC3, AC4).
		if p.onLeak != nil {
			p.onLeak(report)
		}
	} else if wasLeak && p.onRecovery != nil {
		// Recovery: previous check was a leak, this one passed.
		p.onRecovery()
	}
}
