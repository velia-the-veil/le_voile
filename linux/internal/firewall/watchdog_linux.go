//go:build linux

package firewall

import (
	"context"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

// watchdogPollInterval is the period between two `nft list table inet
// levoile` checks. Audit fix F1 (2026-05-04) — short enough to react
// before a typical user notices a leaked connection (3-5 s feels safe);
// long enough not to spam the kernel with nft shellouts. Aligned with
// the Windows WFP watchdog cadence.
const watchdogPollInterval = 3 * time.Second

// nftWatchdog polls the nftables table + chain set in the background and
// pushes an event on alteredCh whenever the expected ruleset has been
// dropped, replaced, or shrunk by an external actor. The watchdog is
// strictly observational: it never re-applies the ruleset itself —
// re-Activate is the service's responsibility (so the service can
// coordinate with the rest of the kill-switch state machine).
type nftWatchdog struct {
	run        commandRunner
	expected   atomic.Pointer[watchdogSnapshot]
	alteredCh  chan struct{}
	startMu    sync.Mutex
	cancelFn   context.CancelFunc
	doneCh     chan struct{}
}

// watchdogSnapshot captures the structural fingerprint of an expected
// ruleset: the chain names that must exist + the minimum total rule
// count. The watchdog flags as "altered" any deviation that drops below
// the snapshot.
type watchdogSnapshot struct {
	chainsRequired []string
	minRules       int
}

// newNftWatchdog allocates the channel; Start arms it.
func newNftWatchdog(run commandRunner) *nftWatchdog {
	return &nftWatchdog{
		run:       run,
		alteredCh: make(chan struct{}, 1),
	}
}

// updateSnapshot records the expected ruleset shape so the next poll
// can compare against it. Called from activateLocked after every
// successful apply.
func (w *nftWatchdog) updateSnapshot(s watchdogSnapshot) {
	cp := s
	cp.chainsRequired = append([]string(nil), s.chainsRequired...)
	w.expected.Store(&cp)
}

// start arms the watchdog. Cancels any previous run and spawns a fresh
// goroutine. Idempotent: calling start while one is already running
// resets the polling and the snapshot is taken from the latest
// updateSnapshot call.
func (w *nftWatchdog) start(parent context.Context) {
	w.startMu.Lock()
	defer w.startMu.Unlock()

	if w.cancelFn != nil {
		w.cancelFn()
		<-w.doneCh
	}

	ctx, cancel := context.WithCancel(parent)
	w.cancelFn = cancel
	w.doneCh = make(chan struct{})

	go w.run_(ctx, w.doneCh)
}

// stop cancels the goroutine and waits for it to exit.
func (w *nftWatchdog) stop() {
	w.startMu.Lock()
	defer w.startMu.Unlock()
	if w.cancelFn == nil {
		return
	}
	w.cancelFn()
	<-w.doneCh
	w.cancelFn = nil
	w.doneCh = nil
}

func (w *nftWatchdog) run_(ctx context.Context, done chan struct{}) {
	defer close(done)
	ticker := time.NewTicker(watchdogPollInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			snap := w.expected.Load()
			if snap == nil {
				continue
			}
			if !w.check(ctx, snap) {
				select {
				case w.alteredCh <- struct{}{}:
				default:
				}
			}
		}
	}
}

// check returns true when the live ruleset still matches the snapshot.
// Returns false the moment any of the following is observed:
//   - the table was deleted (`nft list` errors with "No such file"),
//   - any required chain is missing from the live output,
//   - the total rule line count is less than the snapshot's minimum
//     (a third party flushed rules, leaving the table shell behind).
//
// `nft list table inet levoile` output is small (<10 KB) so the parse
// is cheap. Network is never touched — purely local.
func (w *nftWatchdog) check(ctx context.Context, snap *watchdogSnapshot) bool {
	out, err := w.run(ctx, "nft", "list", "table", "inet", "levoile")
	if err != nil {
		// Table gone → altered.
		return false
	}
	body := string(out)
	for _, chain := range snap.chainsRequired {
		if !strings.Contains(body, "chain "+chain+" ") &&
			!strings.Contains(body, "chain "+chain+"\n") {
			return false
		}
	}
	// Rule count: every nft list line ending with a verdict (accept,
	// drop, jump, return, log) counts. Cheap and good-enough heuristic
	// — the strict canonical list is in ruleset.nft.tmpl. If the count
	// drops below the rendered minimum, something pruned us.
	rules := 0
	for _, line := range strings.Split(body, "\n") {
		t := strings.TrimSpace(line)
		if t == "" || strings.HasPrefix(t, "#") {
			continue
		}
		if strings.HasSuffix(t, "accept") ||
			strings.HasSuffix(t, "drop") ||
			strings.HasSuffix(t, "return") ||
			strings.Contains(t, " jump ") {
			rules++
		}
	}
	return rules >= snap.minRules
}
