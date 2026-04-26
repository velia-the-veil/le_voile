//go:build windows

package service

import (
	"context"
	"fmt"
	"time"

	"github.com/velia-the-veil/le_voile/internal/anomaly"
)

// anomalyNewLoggerFactory is var-injectable for tests. The H2 regression
// test swaps in a slow-build fake to verify that p.mu is NOT held during
// the I/O-heavy NewLogger call. Production code uses anomaly.NewLogger
// directly.
var anomalyNewLoggerFactory = anomaly.NewLogger

// SetAnomalyLogger replaces the Program's anomaly logger. Intended for
// tests that want to capture Started/Succeeded/Failed events through an
// in-memory sink. Production code receives a platform-specific logger
// via ensureAnomalyLogger which lazily calls anomaly.NewLogger on first
// use.
//
// M4 review fix: if a previous logger was registered (e.g. NewLogger()
// had already opened an Event Log handle or a syslog.Writer), it is
// closed before being replaced to avoid handle leaks. Close happens
// OUTSIDE the lock because Close may do I/O — same reasoning as
// ensureAnomalyLogger (H2 fix).
func (p *Program) SetAnomalyLogger(l anomaly.Logger) {
	p.mu.Lock()
	old := p.anomalyLogger
	p.anomalyLogger = l
	p.mu.Unlock()
	if old != nil && old != l {
		_ = old.Close()
	}
}

// SetAnomalyNotifier registers the UI-facing notifier. When the service
// runs headless (no UI attached), callers pass anomaly.NopNotifier{} or
// leave the field nil — either yields silent UI updates. Production
// wiring sets this from cmd/client/main.go once the UI IPC channel is
// established.
func (p *Program) SetAnomalyNotifier(n anomaly.Notifier) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.anomalyNotifier = n
}

// ensureAnomalyLogger returns the Program's anomaly logger, lazily
// constructing the platform default on first access. Called at the top
// of RecoverFromAnomaly so test wiring (SetAnomalyLogger) can inject a
// mock before any recovery fires.
//
// H2 review fix: anomaly.NewLogger() can do blocking I/O (syslog.Dial,
// eventlog.Open). We MUST NOT hold p.mu during that call — it would
// freeze every handler that contends on p.mu (Connect, GetStatus, etc.)
// for the duration of a system-call. The pattern is: read under lock,
// release, build if nil, re-acquire to commit. A race where two callers
// both build a logger in parallel is benign — the second discards its
// logger and takes the one already stored.
func (p *Program) ensureAnomalyLogger() anomaly.Logger {
	p.mu.Lock()
	l := p.anomalyLogger
	p.mu.Unlock()
	if l != nil {
		return l
	}

	built := anomalyNewLoggerFactory()

	p.mu.Lock()
	if p.anomalyLogger == nil {
		p.anomalyLogger = built
	} else {
		// Another goroutine won the race — close our orphan and use
		// the committed one.
		l = p.anomalyLogger
		p.mu.Unlock()
		_ = built.Close()
		return l
	}
	committed := p.anomalyLogger
	p.mu.Unlock()
	return committed
}

// ensureAnomalyNotifier returns the UI notifier, falling back to the
// no-op implementation when none is wired.
func (p *Program) ensureAnomalyNotifier() anomaly.Notifier {
	p.mu.Lock()
	n := p.anomalyNotifier
	p.mu.Unlock()
	if n == nil {
		return anomaly.NopNotifier{}
	}
	return n
}

// AnomalyActive reports whether a recovery sequence is currently running.
// Lock-free read backed by an atomic.Bool. Consumed by the IPC handler
// so /api/status can surface the flag to the webview.
func (p *Program) AnomalyActive() bool {
	return p.anomalyActive.Load()
}

// AnomalyReason returns the current recovery Reason as a string, or ""
// when no recovery is in progress. Lock-free read backed by
// atomic.Pointer[string].
func (p *Program) AnomalyReason() string {
	if r := p.anomalyReasonPtr.Load(); r != nil {
		return *r
	}
	return ""
}

// ForTestSetAnomaly seeds the anomaly state directly without running
// the recovery sequence. Intended for unit tests that want to exercise
// downstream IPC / HTTP serialization paths without spinning up a real
// TUN/firewall. Never call from production code.
//
// M3 review fix: enforces the invariant that an "active" recovery must
// always carry a non-empty reason. A test that violates this invariant
// would mask real bugs where the pair gets out of sync in production.
func (p *Program) ForTestSetAnomaly(active bool, reason string) {
	if active && reason == "" {
		panic("service: ForTestSetAnomaly: active=true requires a non-empty reason")
	}
	p.anomalyActive.Store(active)
	if reason == "" {
		p.anomalyReasonPtr.Store(nil)
		return
	}
	r := reason
	p.anomalyReasonPtr.Store(&r)
}

// RecoverFromAnomaly wraps recoverTUN with logging, UI notification, and
// serialization. It is the single entry point for both the leakcheck
// scheduler (ReasonLeakDetected) and the TUN watchdog
// (ReasonTUNAltered), plus the optional manual IPC trigger
// (ReasonManual — Story 6.3 Task 8).
//
// Concurrency: a TryLock gate guarantees at most one recovery sequence
// is in flight. A second caller that arrives while the first is still
// running returns nil immediately without logging — the first sequence
// already covers the window. This is the expected behaviour when a STUN
// tick and a TUN watchdog event fire within a few seconds of each
// other.
//
// Failure mode: errors from recoverTUN are categorised (anomaly
// ErrorCategory) and surfaced through Failed/Warnf but not returned to
// the scheduler. The scheduler continues its cycle — a transient
// failure should not permanently disable leak detection.
//
// Kill-switch preservation (AC3): recoverTUN invokes firewall.Activate
// WITHOUT a prior Deactivate, leveraging nftables/WFP atomic
// flush-and-replace semantics. This function adds no new firewall
// operations, so the AC3 guarantee holds by construction.
func (p *Program) RecoverFromAnomaly(ctx context.Context, reason anomaly.Reason) error {
	if ctx.Err() != nil {
		return ctx.Err()
	}

	if !p.anomalyRecoveryMu.TryLock() {
		// A previous invocation is already running — drop silently.
		// This matches the design note in the story Dev Notes: "a leak
		// detected while a recovery is in progress is ignored".
		return nil
	}
	defer p.anomalyRecoveryMu.Unlock()

	// Lazy-init logger / notifier so tests can inject mocks at any
	// point before the first recovery.
	logger := p.ensureAnomalyLogger()
	notifier := p.ensureAnomalyNotifier()

	// Publish the "in-progress" state BEFORE starting the sequence so
	// the /api/status polling picks it up on the very first tick after
	// the trigger. The value is reset in defer to guarantee clearance
	// even when recoverTUN panics (unlikely — it doesn't — but defence
	// in depth).
	reasonStr := string(reason)
	p.anomalyReasonPtr.Store(&reasonStr)
	p.anomalyActive.Store(true)
	defer func() {
		p.anomalyActive.Store(false)
		p.anomalyReasonPtr.Store(nil)
	}()

	logger.Started(reason)
	notifier.Started(reason)

	start := time.Now()
	err := p.recoverTUN(ctx)
	durationMs := time.Since(start).Milliseconds()

	if err != nil {
		cat := anomaly.CategorizeError(err)
		logger.Failed(cat)
		notifier.Failed(cat)
		// Echo a short, NFR22a-compliant summary to the unified service
		// stderr stream so local `journalctl -u levoile` tails also see
		// the failure alongside the richer recoverTUN stage logs.
		fmt.Fprintf(serviceStderr, "service: anomaly recovery failed: reason=%s category=%s\n", reason, cat)
		return err
	}

	logger.Succeeded(durationMs)
	notifier.Succeeded(durationMs)
	fmt.Fprintf(serviceStderr, "service: anomaly recovery succeeded: reason=%s duration_ms=%d\n", reason, durationMs)
	return nil
}
