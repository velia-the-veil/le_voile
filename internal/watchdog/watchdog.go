// Package watchdog monitors the system DNS resolver and corrects
// inconsistencies to prevent DNS leaks.
package watchdog

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"
)

// Sentinel errors for watchdog operations.
var (
	ErrWatchdogAlreadyRunning = errors.New("watchdog: already running")
	ErrResolverInconsistent   = errors.New("watchdog: resolver inconsistent")
)

// WatchdogInterval is the default interval between resolver checks.
const WatchdogInterval = 3 * time.Second

// ResolverChecker returns the current system DNS resolver address.
type ResolverChecker func(ctx context.Context) (string, error)

// ResolverFixer sets the system DNS resolver to the given address.
type ResolverFixer func(ctx context.Context, addr string) error

// Watchdog periodically checks the system DNS resolver and restores it
// to the expected address if an external process modifies it.
type Watchdog struct {
	mu       sync.Mutex
	running  bool
	cancel   context.CancelFunc
	done     chan struct{}

	interval     time.Duration
	expectedAddr string
	checker      ResolverChecker
	fixer        ResolverFixer
}

// NewWatchdog creates a Watchdog that verifies the system resolver matches
// expectedAddr every interval, correcting it via fixer when inconsistent.
func NewWatchdog(expectedAddr string, checker ResolverChecker, fixer ResolverFixer) *Watchdog {
	return &Watchdog{
		interval:     WatchdogInterval,
		expectedAddr: expectedAddr,
		checker:      checker,
		fixer:        fixer,
	}
}

// Start begins the periodic resolver check loop. It blocks until ctx is
// cancelled or Stop is called.
//
// Returns ErrWatchdogAlreadyRunning if the watchdog is already active.
func (w *Watchdog) Start(ctx context.Context) error {
	w.mu.Lock()
	if w.running {
		w.mu.Unlock()
		return ErrWatchdogAlreadyRunning
	}

	ctx, w.cancel = context.WithCancel(ctx)
	w.done = make(chan struct{})
	w.running = true
	w.mu.Unlock()

	defer func() {
		w.mu.Lock()
		w.running = false
		w.cancel = nil
		close(w.done)
		w.mu.Unlock()
	}()

	ticker := time.NewTicker(w.interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
			w.checkAndFix(ctx)
		}
	}
}

// Stop halts the watchdog and waits for it to finish.
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

// VerifyAndRestore performs a single resolver check and fix. This is used
// during shutdown to confirm the resolver state before exiting.
func (w *Watchdog) VerifyAndRestore(ctx context.Context, expectedAddr string) error {
	current, err := w.checker(ctx)
	if err != nil {
		return fmt.Errorf("watchdog: verify: %w", err)
	}

	if current != expectedAddr {
		if err := w.fixer(ctx, expectedAddr); err != nil {
			return fmt.Errorf("watchdog: restore: %w", err)
		}
	}

	return nil
}

// checkAndFix performs a single resolver check cycle with retry on failure.
func (w *Watchdog) checkAndFix(ctx context.Context) {
	if ctx.Err() != nil {
		return
	}

	current, err := w.checker(ctx)
	if err != nil {
		return
	}

	if current != w.expectedAddr {
		// Resolver is inconsistent — fix it, retry once on failure.
		if err := w.fixer(ctx, w.expectedAddr); err != nil {
			select {
			case <-ctx.Done():
				return
			case <-time.After(500 * time.Millisecond):
			}
			w.fixer(ctx, w.expectedAddr)
		}
	}
}
