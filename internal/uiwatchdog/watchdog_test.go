package uiwatchdog

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"
)

// fakeLauncher is an in-memory ProcessLauncher that lets tests drive
// process exits manually via PushExit / drain.
type fakeLauncher struct {
	mu          sync.Mutex
	available   bool
	launchCalls int
	launchErr   error
	// pendingExits queues exit events that next Launch consumers will receive.
	exitCh chan ProcessExit
}

func newFakeLauncher() *fakeLauncher {
	return &fakeLauncher{
		available: true,
		exitCh:    make(chan ProcessExit, 16),
	}
}

func (f *fakeLauncher) Launch(ctx context.Context) (<-chan ProcessExit, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.launchErr != nil {
		return nil, f.launchErr
	}
	f.launchCalls++
	// Hand back the same channel — the test pushes exits into it.
	return f.exitCh, nil
}

func (f *fakeLauncher) Available() bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.available
}

func (f *fakeLauncher) setAvailable(v bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.available = v
}

func (f *fakeLauncher) launches() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.launchCalls
}

func (f *fakeLauncher) pushExit(code int) {
	f.exitCh <- ProcessExit{ExitCode: code}
}

// nopLogger is a logger that discards messages — keeps tests quiet.
type nopLogger struct{}

func (nopLogger) Infof(string, ...any)  {}
func (nopLogger) Warnf(string, ...any)  {}
func (nopLogger) Errorf(string, ...any) {}

// fakeClock is a manually-controlled clock for deterministic time-based tests.
type fakeClock struct {
	mu  sync.Mutex
	now time.Time
}

func (c *fakeClock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.now
}

func (c *fakeClock) advance(d time.Duration) {
	c.mu.Lock()
	c.now = c.now.Add(d)
	c.mu.Unlock()
}

func newFakeClock() *fakeClock {
	return &fakeClock{now: time.Date(2026, 4, 18, 12, 0, 0, 0, time.UTC)}
}

// waitFor polls until pred returns true or the deadline is reached.
// Helps wait for async state changes (goroutine scheduling) without sleeping.
func waitFor(t *testing.T, deadline time.Duration, pred func() bool, msg string) {
	t.Helper()
	end := time.Now().Add(deadline)
	for time.Now().Before(end) {
		if pred() {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("waitFor timeout: %s", msg)
}

func TestWatchdog_LaunchesOnStart(t *testing.T) {
	t.Parallel()
	fl := newFakeLauncher()
	clk := newFakeClock()
	wd := newForTest(Config{
		Launcher:        fl,
		MinRestartDelay: time.Millisecond,
		MaxRestarts:     5,
		Window:          60 * time.Second,
		Backoff:         5 * time.Minute,
		Logger:          nopLogger{},
	}, clk)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go wd.Start(ctx)
	defer wd.Stop()

	waitFor(t, 1*time.Second, func() bool { return fl.launches() == 1 }, "first launch")
	snap := wd.Snapshot()
	if !snap.Enabled {
		t.Errorf("Snapshot.Enabled = false, want true")
	}
}

func TestWatchdog_RespawnsAfterCrash(t *testing.T) {
	t.Parallel()
	fl := newFakeLauncher()
	clk := newFakeClock()
	wd := newForTest(Config{
		Launcher:        fl,
		MinRestartDelay: time.Millisecond,
		MaxRestarts:     5,
		Window:          60 * time.Second,
		Backoff:         5 * time.Minute,
		Logger:          nopLogger{},
	}, clk)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go wd.Start(ctx)
	defer wd.Stop()

	waitFor(t, 1*time.Second, func() bool { return fl.launches() == 1 }, "first launch")
	fl.pushExit(1) // simulate crash
	waitFor(t, 1*time.Second, func() bool { return fl.launches() == 2 }, "respawn after crash")
	snap := wd.Snapshot()
	if snap.RestartCountWindow != 1 {
		t.Errorf("RestartCountWindow = %d, want 1", snap.RestartCountWindow)
	}
	if snap.LastRestartAt.IsZero() {
		t.Errorf("LastRestartAt should be set after a restart")
	}
}

func TestWatchdog_RateLimitTriggersBackoff(t *testing.T) {
	t.Parallel()
	fl := newFakeLauncher()
	clk := newFakeClock()
	wd := newForTest(Config{
		Launcher:        fl,
		MinRestartDelay: time.Millisecond,
		MaxRestarts:     5,
		Window:          60 * time.Second,
		Backoff:         5 * time.Minute,
		Logger:          nopLogger{},
	}, clk)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go wd.Start(ctx)
	defer wd.Stop()

	// MaxRestarts=5 with systemd-parity semantic (>= MaxRestarts): 5 launches
	// are allowed inside the window, the 5th crash trips backoff before a 6th
	// respawn can happen. Matches StartLimitBurst=5 on Linux.
	for i := 0; i < 5; i++ {
		want := i + 1
		waitFor(t, 1*time.Second, func() bool { return fl.launches() == want }, "launch")
		fl.pushExit(1)
	}
	// After the 5th crash, we hit the rate limit (5 launches inside the window).
	waitFor(t, 1*time.Second, func() bool {
		s := wd.Snapshot()
		return !s.BackoffUntil.IsZero()
	}, "backoff engaged")

	snap := wd.Snapshot()
	if snap.BackoffUntil.IsZero() {
		t.Errorf("BackoffUntil should be set after rate limit")
	}
	expected := clk.Now().Add(5 * time.Minute)
	if snap.BackoffUntil.Before(expected.Add(-time.Second)) || snap.BackoffUntil.After(expected.Add(time.Second)) {
		t.Errorf("BackoffUntil = %v, want ~%v", snap.BackoffUntil, expected)
	}
	// During backoff, no new launches occur.
	before := fl.launches()
	time.Sleep(50 * time.Millisecond)
	if got := fl.launches(); got != before {
		t.Errorf("launches during backoff = %d, want %d", got, before)
	}
}

func TestWatchdog_CleanExitDoesNotCountTowardRateLimit(t *testing.T) {
	t.Parallel()
	fl := newFakeLauncher()
	clk := newFakeClock()
	wd := newForTest(Config{
		Launcher:        fl,
		MinRestartDelay: time.Millisecond,
		MaxRestarts:     5,
		Window:          60 * time.Second,
		Backoff:         5 * time.Minute,
		Logger:          nopLogger{},
	}, clk)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go wd.Start(ctx)
	defer wd.Stop()

	waitFor(t, 1*time.Second, func() bool { return fl.launches() == 1 }, "first launch")
	fl.pushExit(0) // clean exit (e.g. user clicked Quit)
	// Clean exit → watchdog should NOT respawn (and counter stays 0).
	time.Sleep(100 * time.Millisecond)
	if fl.launches() != 1 {
		t.Errorf("clean exit triggered respawn: launches = %d, want 1", fl.launches())
	}
	snap := wd.Snapshot()
	if snap.RestartCountWindow != 0 {
		t.Errorf("RestartCountWindow = %d after clean exit, want 0", snap.RestartCountWindow)
	}
}

func TestWatchdog_StopUnblocks(t *testing.T) {
	t.Parallel()
	fl := newFakeLauncher()
	clk := newFakeClock()
	wd := newForTest(Config{
		Launcher:        fl,
		MinRestartDelay: time.Millisecond,
		MaxRestarts:     5,
		Window:          60 * time.Second,
		Backoff:         5 * time.Minute,
		Logger:          nopLogger{},
	}, clk)

	ctx := context.Background()
	done := make(chan struct{})
	go func() {
		wd.Start(ctx)
		close(done)
	}()
	waitFor(t, 1*time.Second, func() bool { return fl.launches() == 1 }, "first launch")

	wd.Stop()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("Stop did not unblock Start within 2s")
	}
}

func TestWatchdog_NoSessionDefersLaunch(t *testing.T) {
	t.Parallel()
	fl := newFakeLauncher()
	fl.setAvailable(false)
	clk := newFakeClock()
	wd := newForTest(Config{
		Launcher:        fl,
		MinRestartDelay: time.Millisecond,
		MaxRestarts:     5,
		Window:          60 * time.Second,
		Backoff:         5 * time.Minute,
		PollInterval:    20 * time.Millisecond,
		Logger:          nopLogger{},
	}, clk)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go wd.Start(ctx)
	defer wd.Stop()

	// No session → no launch.
	time.Sleep(80 * time.Millisecond)
	if fl.launches() != 0 {
		t.Errorf("launched without session: launches = %d, want 0", fl.launches())
	}
	// Session appears → watchdog should detect and launch.
	fl.setAvailable(true)
	waitFor(t, 1*time.Second, func() bool { return fl.launches() == 1 }, "launch after session appears")
}

func TestWatchdog_LaunchErrorDoesNotPanic(t *testing.T) {
	t.Parallel()
	fl := newFakeLauncher()
	fl.launchErr = errors.New("simulated launch failure")
	clk := newFakeClock()
	wd := newForTest(Config{
		Launcher:        fl,
		MinRestartDelay: time.Millisecond,
		MaxRestarts:     5,
		Window:          60 * time.Second,
		Backoff:         5 * time.Minute,
		PollInterval:    20 * time.Millisecond,
		Logger:          nopLogger{},
	}, clk)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go wd.Start(ctx)
	defer wd.Stop()

	// Watchdog should keep retrying without crashing.
	time.Sleep(80 * time.Millisecond)
	wd.Stop() // must not deadlock
}

func TestWatchdog_DoubleStartFails(t *testing.T) {
	t.Parallel()
	fl := newFakeLauncher()
	clk := newFakeClock()
	wd := newForTest(Config{
		Launcher:        fl,
		MinRestartDelay: time.Millisecond,
		MaxRestarts:     5,
		Window:          60 * time.Second,
		Backoff:         5 * time.Minute,
		Logger:          nopLogger{},
	}, clk)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	go wd.Start(ctx)
	defer wd.Stop()
	waitFor(t, 1*time.Second, func() bool { return fl.launches() == 1 }, "first launch")

	if err := wd.Start(ctx); !errors.Is(err, ErrAlreadyRunning) {
		t.Errorf("second Start() error = %v, want ErrAlreadyRunning", err)
	}
}

func TestWatchdog_WindowSlidingClearsOldRestarts(t *testing.T) {
	t.Parallel()
	fl := newFakeLauncher()
	clk := newFakeClock()
	wd := newForTest(Config{
		Launcher:        fl,
		MinRestartDelay: time.Millisecond,
		MaxRestarts:     5,
		Window:          60 * time.Second,
		Backoff:         5 * time.Minute,
		Logger:          nopLogger{},
	}, clk)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go wd.Start(ctx)
	defer wd.Stop()

	waitFor(t, 1*time.Second, func() bool { return fl.launches() == 1 }, "launch")
	// 3 quick crashes within the same instant.
	for i := 0; i < 3; i++ {
		want := i + 2
		fl.pushExit(1)
		waitFor(t, 1*time.Second, func() bool { return fl.launches() == want }, "respawn")
	}
	if c := wd.Snapshot().RestartCountWindow; c != 3 {
		t.Errorf("RestartCountWindow = %d, want 3", c)
	}

	// Advance clock past the window — old entries should be evicted on next
	// crash event.
	clk.advance(61 * time.Second)
	fl.pushExit(1)
	waitFor(t, 1*time.Second, func() bool { return fl.launches() == 5 }, "respawn after window expiry")
	// Only the latest restart (the 4th one we just induced, post-eviction) remains in window.
	if c := wd.Snapshot().RestartCountWindow; c != 1 {
		t.Errorf("RestartCountWindow after window slide = %d, want 1", c)
	}
}

// TestWatchdog_StopWhileBackoff confirms Stop unblocks Start even when the
// watchdog is parked inside its backoff sleep — important for clean service
// shutdown ordering (uiwatchdog must Stop before tunnel/firewall teardown).
func TestWatchdog_StopWhileBackoff(t *testing.T) {
	t.Parallel()
	fl := newFakeLauncher()
	clk := newFakeClock()
	wd := newForTest(Config{
		Launcher:        fl,
		MinRestartDelay: time.Millisecond,
		MaxRestarts:     2,
		Window:          60 * time.Second,
		Backoff:         5 * time.Minute,
		Logger:          nopLogger{},
	}, clk)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan struct{})
	go func() { wd.Start(ctx); close(done) }()

	// Trigger backoff: MaxRestarts=2 means 2 launches allowed, the 2nd crash
	// trips the rate limit (>= semantic, systemd parity).
	for i := 0; i < 2; i++ {
		want := i + 1
		waitFor(t, 1*time.Second, func() bool { return fl.launches() == want }, "launch")
		fl.pushExit(1)
	}
	waitFor(t, 1*time.Second, func() bool { return !wd.Snapshot().BackoffUntil.IsZero() }, "backoff engaged")

	wd.Stop()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("Stop did not unblock Start while in backoff")
	}
}

// TestWatchdog_NegativeMinRestartDelayDisablesCooldown locks in the
// sentinel convention documented on Config.MinRestartDelay: a negative
// value means "no cooldown", while zero means "use the 2s default".
func TestWatchdog_NegativeMinRestartDelayDisablesCooldown(t *testing.T) {
	t.Parallel()
	fl := newFakeLauncher()
	clk := newFakeClock()
	wd := newForTest(Config{
		Launcher:        fl,
		MinRestartDelay: -1, // explicit "no cooldown"
		MaxRestarts:     10,
		Window:          60 * time.Second,
		Backoff:         5 * time.Minute,
		Logger:          nopLogger{},
	}, clk)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go wd.Start(ctx)
	defer wd.Stop()

	// Three crashes back-to-back must complete without blocking on any
	// wall-clock delay (would take 6s+ with default 2s cooldown).
	start := time.Now()
	for i := 0; i < 3; i++ {
		want := i + 1
		waitFor(t, 500*time.Millisecond, func() bool { return fl.launches() == want }, "launch")
		fl.pushExit(1)
	}
	waitFor(t, 500*time.Millisecond, func() bool { return fl.launches() == 4 }, "final respawn")
	if elapsed := time.Since(start); elapsed > 2*time.Second {
		t.Errorf("cooldown was not disabled: took %v, expected < 2s", elapsed)
	}
}

// Compile-time check that the test fake satisfies the launcher contract.
var _ ProcessLauncher = (*fakeLauncher)(nil)

// Confirms Snapshot is safe to call before/after Start.
func TestWatchdog_SnapshotIdempotent(t *testing.T) {
	t.Parallel()
	fl := newFakeLauncher()
	wd, err := New(Config{Launcher: fl, Logger: nopLogger{}})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	s1 := wd.Snapshot()
	s2 := wd.Snapshot()
	if s1.Enabled != s2.Enabled {
		t.Errorf("snapshot drift: s1=%+v s2=%+v", s1, s2)
	}
}
