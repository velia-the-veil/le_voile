//go:build windows

// Package uiwatchdog supervises the levoile-ui process from inside the
// privileged service. On Windows the service spawns the UI in the active
// interactive session and respawns it on crash; on Linux supervision is
// delegated to systemd user units, so the package compiles to a no-op
// Stub that satisfies the same API.
//
// Story 5.7 — auto-restart UI en cas de crash (FR15b).
package uiwatchdog

import (
	"context"
	"errors"
	"sync"
	"time"
)

// Defaults align with Story 5.7 ACs (5 restarts / 60s → 5 min backoff).
const (
	DefaultMaxRestarts     = 5
	DefaultWindow          = 60 * time.Second
	DefaultBackoff         = 5 * time.Minute
	DefaultMinRestartDelay = 2 * time.Second
	DefaultPollInterval    = 5 * time.Second
)

// Sentinel errors exported for callers that need to react to specific
// failure modes (mostly the service program and tests).
var (
	ErrAlreadyRunning = errors.New("uiwatchdog: already running")
	ErrNoLauncher     = errors.New("uiwatchdog: ProcessLauncher required")
)

// Logger lets the watchdog emit structured info / warning / error messages
// through whatever the host service uses. Levels match the rest of the repo.
type Logger interface {
	Infof(format string, args ...any)
	Warnf(format string, args ...any)
	Errorf(format string, args ...any)
}

// ProcessExit is the result of a supervised process terminating.
type ProcessExit struct {
	ExitCode int
	Err      error
}

// ProcessLauncher launches and waits on the supervised process. The
// returned channel emits exactly one ProcessExit when the process exits;
// after that the channel may be closed or stay open — the watchdog calls
// Launch again to re-supervise.
//
// Available reports whether a target environment exists for a launch
// (e.g. an interactive Windows session). Returning false defers the next
// launch until the next poll instead of consuming the rate-limit budget.
type ProcessLauncher interface {
	Launch(ctx context.Context) (<-chan ProcessExit, error)
	Available() bool
}

// Snapshot is a point-in-time view of the watchdog state, exposed via IPC
// so the UI / diagnostics tooling can observe behaviour without scraping
// OS logs (AC7).
type Snapshot struct {
	Enabled            bool      `json:"enabled"`
	LastRestartAt      time.Time `json:"last_restart_at,omitempty"`
	RestartCountWindow int       `json:"restart_count_window"`
	BackoffUntil       time.Time `json:"backoff_until,omitempty"`
}

// Config parameters a Watchdog. All durations have sensible defaults — pass
// zero to use them. Launcher is mandatory.
//
// MinRestartDelay is special: zero means "use DefaultMinRestartDelay (2s)",
// and a **negative** value means "no cooldown between respawns" (tests use
// this to run the FSM without wall-clock delays). Callers who genuinely
// want zero cooldown should pass -1, not 0.
type Config struct {
	Launcher        ProcessLauncher
	MaxRestarts     int           // launches allowed inside Window
	Window          time.Duration // sliding window
	Backoff         time.Duration // sleep when MaxRestarts is breached
	MinRestartDelay time.Duration // cooldown between respawns; <0 = no cooldown
	PollInterval    time.Duration // poll cadence when Launcher.Available()=false
	Logger          Logger
}

func (c *Config) applyDefaults() {
	if c.MaxRestarts == 0 {
		c.MaxRestarts = DefaultMaxRestarts
	}
	if c.Window == 0 {
		c.Window = DefaultWindow
	}
	if c.Backoff == 0 {
		c.Backoff = DefaultBackoff
	}
	if c.MinRestartDelay == 0 {
		c.MinRestartDelay = DefaultMinRestartDelay
	}
	if c.PollInterval == 0 {
		c.PollInterval = DefaultPollInterval
	}
	if c.Logger == nil {
		c.Logger = noopLogger{}
	}
}

// clock indirection lets unit tests freeze time without touching production
// timing behaviour. Production builds pass realClock{}.
type clock interface {
	Now() time.Time
}

type realClock struct{}

func (realClock) Now() time.Time { return time.Now() }

type noopLogger struct{}

func (noopLogger) Infof(string, ...any)  {}
func (noopLogger) Warnf(string, ...any)  {}
func (noopLogger) Errorf(string, ...any) {}

// Watchdog supervises a single child process. Use New to construct it and
// Start(ctx) to run the supervision loop (blocking). Stop is safe from
// any goroutine.
type Watchdog struct {
	cfg   Config
	clock clock

	mu      sync.Mutex
	running bool
	cancel  context.CancelFunc
	done    chan struct{}

	// Snapshot fields. Guarded by snapMu to avoid lock contention with the
	// supervision loop, since Snapshot may be polled at high frequency by
	// IPC GetStatus.
	snapMu        sync.RWMutex
	restartTimes  []time.Time
	lastRestartAt time.Time
	backoffUntil  time.Time
}

// New constructs a Watchdog with the given configuration. Returns
// ErrNoLauncher if Config.Launcher is nil.
func New(cfg Config) (*Watchdog, error) {
	if cfg.Launcher == nil {
		return nil, ErrNoLauncher
	}
	cfg.applyDefaults()
	return &Watchdog{cfg: cfg, clock: realClock{}}, nil
}

// newForTest builds a Watchdog with an injected clock — exposed for the
// test suite only.
func newForTest(cfg Config, c clock) *Watchdog {
	cfg.applyDefaults()
	if c == nil {
		c = realClock{}
	}
	return &Watchdog{cfg: cfg, clock: c}
}

// Snapshot returns a point-in-time copy of the watchdog state. Safe to
// call concurrently and before/after Start/Stop.
func (w *Watchdog) Snapshot() Snapshot {
	w.snapMu.RLock()
	defer w.snapMu.RUnlock()
	w.mu.Lock()
	enabled := w.running
	w.mu.Unlock()
	return Snapshot{
		Enabled:            enabled,
		LastRestartAt:      w.lastRestartAt,
		RestartCountWindow: len(w.restartTimes),
		BackoffUntil:       w.backoffUntil,
	}
}

// Start runs the supervision loop until ctx is cancelled or Stop is
// called. It blocks; callers typically launch it in a goroutine.
//
// Returns ErrAlreadyRunning if Start is called twice without an
// intervening Stop.
func (w *Watchdog) Start(ctx context.Context) error {
	w.mu.Lock()
	if w.running {
		w.mu.Unlock()
		return ErrAlreadyRunning
	}
	w.running = true
	loopCtx, cancel := context.WithCancel(ctx)
	w.cancel = cancel
	w.done = make(chan struct{})
	w.mu.Unlock()

	defer func() {
		w.mu.Lock()
		w.running = false
		w.mu.Unlock()
		close(w.done)
	}()

	w.cfg.Logger.Infof("uiwatchdog: started")
	w.loop(loopCtx)
	w.cfg.Logger.Infof("uiwatchdog: stopped")
	return nil
}

// Stop signals the supervision loop to exit and waits for it to do so.
// Idempotent and safe to call before Start has been invoked.
func (w *Watchdog) Stop() {
	w.mu.Lock()
	cancel := w.cancel
	done := w.done
	w.mu.Unlock()
	if cancel != nil {
		cancel()
	}
	if done != nil {
		<-done
	}
}

// loop is the main supervision FSM. States: WAITING_SESSION → LAUNCHING →
// RUNNING → (clean exit → stop) | (crash → BACKOFF? → LAUNCHING).
func (w *Watchdog) loop(ctx context.Context) {
	for {
		if ctx.Err() != nil {
			return
		}

		// Honour any active backoff before doing anything else.
		if !w.waitForBackoff(ctx) {
			return
		}

		// Wait for an interactive session if the launcher reports none.
		if !w.cfg.Launcher.Available() {
			if !w.sleep(ctx, w.cfg.PollInterval) {
				return
			}
			continue
		}

		// Launch the process.
		exitCh, err := w.cfg.Launcher.Launch(ctx)
		if err != nil {
			w.cfg.Logger.Errorf("uiwatchdog: launch failed: %v", err)
			if !w.sleep(ctx, w.cfg.PollInterval) {
				return
			}
			continue
		}
		w.cfg.Logger.Infof("uiwatchdog: process launched")

		// Wait for the process to exit (or shutdown).
		var exit ProcessExit
		select {
		case <-ctx.Done():
			return
		case exit = <-exitCh:
		}

		// Clean exit (code 0) means the user asked to quit — stand down.
		if exit.ExitCode == 0 && exit.Err == nil {
			w.cfg.Logger.Infof("uiwatchdog: process exited cleanly, supervision idle")
			// Stay in the loop so we still react to a future explicit
			// re-launch trigger… but Story 5.7 doesn't need that, so block
			// on ctx until shutdown.
			<-ctx.Done()
			return
		}

		// Crash path — record, rate-limit, optionally backoff, then loop.
		over := w.recordCrash()
		if over {
			now := w.clock.Now()
			until := now.Add(w.cfg.Backoff)
			w.snapMu.Lock()
			w.backoffUntil = until
			w.snapMu.Unlock()
			w.cfg.Logger.Warnf("uiwatchdog: rate limit hit (%d restarts in %s), backing off until %s",
				w.cfg.MaxRestarts, w.cfg.Window, until.Format(time.RFC3339))
			continue
		}

		// Cooldown to avoid tight respawn loops on transient launcher
		// faults that exit immediately.
		if w.cfg.MinRestartDelay > 0 {
			if !w.sleep(ctx, w.cfg.MinRestartDelay) {
				return
			}
		}
	}
}

// recordCrash appends a restart timestamp inside the sliding window and
// returns true when the rate limit has been crossed.
//
// Semantic: MaxRestarts is the **total number of launches** allowed inside
// Window, not the number of respawns. This matches systemd's StartLimitBurst
// directive so Windows and Linux fail at the same crash count (Story 5.7
// AC5 — rate limit identique sur les deux plateformes).
func (w *Watchdog) recordCrash() bool {
	now := w.clock.Now()
	w.snapMu.Lock()
	defer w.snapMu.Unlock()

	cutoff := now.Add(-w.cfg.Window)
	kept := w.restartTimes[:0]
	for _, t := range w.restartTimes {
		if !t.Before(cutoff) {
			kept = append(kept, t)
		}
	}
	kept = append(kept, now)
	w.restartTimes = kept
	w.lastRestartAt = now
	return len(w.restartTimes) >= w.cfg.MaxRestarts
}

// waitForBackoff blocks until the current backoff window expires (if any)
// or the context is cancelled. Returns false if cancelled, true otherwise.
func (w *Watchdog) waitForBackoff(ctx context.Context) bool {
	w.snapMu.RLock()
	until := w.backoffUntil
	w.snapMu.RUnlock()
	if until.IsZero() {
		return true
	}
	now := w.clock.Now()
	if !until.After(now) {
		// Backoff expired — clear it and reset the sliding window so the
		// next crash starts fresh.
		w.snapMu.Lock()
		w.backoffUntil = time.Time{}
		w.restartTimes = w.restartTimes[:0]
		w.snapMu.Unlock()
		return true
	}
	d := until.Sub(now)
	return w.sleep(ctx, d)
}

// sleep is a context-aware time.Sleep that returns false if ctx fires
// during the wait.
func (w *Watchdog) sleep(ctx context.Context, d time.Duration) bool {
	if d <= 0 {
		return ctx.Err() == nil
	}
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return false
	case <-t.C:
		return true
	}
}
