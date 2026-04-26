//go:build windows

// Package anomaly surfaces auto-recovery events (kill-switch-preserving
// reconnection triggered by STUN leak detection or TUN watchdog) through
// two independent channels:
//
//   - Logger: writes a short, operator-facing warning to the platform's
//     system log (Windows Event Log source "LeVoile" / Linux syslog-to-
//     journald tag "levoile"). Messages are scrubbed of user data per
//     NFR22a: no IPs, no interface names, no file paths — only a fixed
//     set of categorical reason / error codes.
//
//   - Notifier: pushes the current recovery state to the UI process so the
//     tray icon can switch to an orange "alert" glyph and the webview can
//     show an "Anomaly detected — reconnecting" banner.
//
// Story 6.3 (Epic 6 — Validation Anti-Fuite): the package is deliberately
// separate from internal/firewall's eventlog shim to keep logging
// responsibilities scoped. Firewall logs its own activation/teardown
// events; anomaly logs the recovery orchestration events.
//
// Cross-platform split: anomaly_windows.go, anomaly_linux.go, and
// anomaly_stub.go (macOS/other — tests build here). All implementations
// fall back to stderr when the underlying log sink cannot be opened so
// the service never fails to start because journald/Event-Log is
// unreachable.
package anomaly

import (
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
	"sync"
)

// Reason categorizes why a recovery sequence was initiated. Used as part
// of the log payload and forwarded to the Notifier so the UI can render
// a slightly different message for each case (Story 6.3 AC5).
//
// Only a fixed set of values is allowed — never a free-form string — to
// guarantee NFR22a compliance (no user data leaks into logs).
type Reason string

const (
	// ReasonLeakDetected is set when the leakcheck PeriodicScheduler
	// observes Status == leak_detected (Story 6.2).
	ReasonLeakDetected Reason = "leak_detected"
	// ReasonTUNAltered is set when the TUN watchdog (Story 2.2) fires
	// because levoile0 disappeared, MTU changed, or flags were tampered.
	ReasonTUNAltered Reason = "tun_altered"
	// ReasonManual is set when an operator triggers a recovery from
	// levoile-ctl (IPC ActionTriggerRecovery) for debugging or incident
	// response.
	ReasonManual Reason = "manual"
)

// ErrorCategory classifies a recovery failure. Values are a closed set
// so that Log output stays NFR22a-compliant regardless of the raw error
// message wrapped inside.
//
// Live vs reserved categories (M1 review fix — documentation of dead
// code reality, Story 6.3):
//
//   - CategoryTUNCreateFailed       LIVE — returned by recoverTUN stage 1.
//   - CategoryTunnelConnectFailed   LIVE — returned by recoverTUN stage 4.
//   - CategoryUnknown               LIVE — catch-all fallback.
//   - CategoryRoutingSetupFailed    RESERVED — recoverTUN currently
//       swallows stage-2 routing errors (logs to stderr and continues
//       best-effort). The category is kept so that a future refactor
//       that propagates those errors does not need to re-invent the
//       taxonomy. Tests use synthetic errors to validate the matcher.
//   - CategoryFirewallActivateFail  RESERVED — same reasoning for
//       recoverTUN stage 3. Firewall activation failures are logged
//       but not bubbled because partial recovery (TUN up, firewall
//       lagging) is still preferable to no recovery.
type ErrorCategory string

const (
	CategoryTUNCreateFailed      ErrorCategory = "tun_create_failed"
	CategoryRoutingSetupFailed   ErrorCategory = "routing_setup_failed"
	CategoryFirewallActivateFail ErrorCategory = "firewall_activate_failed"
	CategoryTunnelConnectFailed  ErrorCategory = "tunnel_connect_failed"
	CategoryUnknown              ErrorCategory = "unknown"
)

// Logger writes anomaly lifecycle events to the OS system log. Messages
// MUST NOT contain IPs, interface names, domains, or file paths (NFR22a).
// Implementations fall back to stderr when the system log sink fails to
// open.
type Logger interface {
	// Started logs that a recovery sequence has begun for the given reason.
	Started(reason Reason)
	// Succeeded logs a successful recovery along with its duration in
	// milliseconds.
	Succeeded(durationMs int64)
	// Failed logs a recovery failure classified by ErrorCategory.
	Failed(category ErrorCategory)
	// Close releases any OS handles held by the logger. Safe to call
	// multiple times.
	Close() error
}

// Notifier reports recovery lifecycle events to the UI process so the
// tray icon and webview banner can be updated. Implementations MUST be
// goroutine-safe: Started/Succeeded/Failed may fire from the leak
// scheduler or the TUN watchdog, which run on their own goroutines.
type Notifier interface {
	Started(reason Reason)
	Succeeded(durationMs int64)
	Failed(category ErrorCategory)
}

// NopNotifier is a Notifier that discards every event. Returned by the
// factory when no UI is attached (headless service mode).
type NopNotifier struct{}

func (NopNotifier) Started(Reason)          {}
func (NopNotifier) Succeeded(int64)         {}
func (NopNotifier) Failed(ErrorCategory)    {}

// CategorizeError maps a recovery error produced by service.recoverTUN
// onto a fixed ErrorCategory for NFR22a-safe logging. Matching is done
// on substring markers present in the error messages returned by
// internal/service/service.go:recoverTUN. Unknown errors fall back to
// CategoryUnknown — never to the raw error string.
func CategorizeError(err error) ErrorCategory {
	if err == nil {
		return CategoryUnknown
	}
	msg := err.Error()
	// Order matters: the tunnel/firewall/routing markers are checked
	// before the generic "tun" marker because recoverTUN's stage-2/3/4
	// errors contain the "tun recovery:" prefix that would otherwise win.
	switch {
	case containsAny(msg, "tunnel.Connect", "tunnel: connect", "tunnel connect"):
		return CategoryTunnelConnectFailed
	case containsAny(msg, "firewall"):
		return CategoryFirewallActivateFail
	case containsAny(msg, "routing", "route"):
		return CategoryRoutingSetupFailed
	case containsAny(msg, "tun.New", "tun recovery: tun"):
		return CategoryTUNCreateFailed
	}
	return CategoryUnknown
}

func containsAny(haystack string, needles ...string) bool {
	for _, n := range needles {
		if strings.Contains(haystack, n) {
			return true
		}
	}
	return false
}

// ErrLoggerClosed is returned by Close on a logger that was already
// closed. Callers should treat it as a no-op.
var ErrLoggerClosed = errors.New("anomaly: logger already closed")

// stderrLogger is the fallback used when the platform logger cannot open
// its system log sink. Messages are written as prefixed lines to
// os.Stderr (or a configurable writer for tests). It also doubles as the
// Linux/Windows "inner" logger so a single code path renders the exact
// same text everywhere.
type stderrLogger struct {
	mu     sync.Mutex
	out    io.Writer
	closed bool
}

func newStderrLogger(out io.Writer) *stderrLogger {
	if out == nil {
		out = os.Stderr
	}
	return &stderrLogger{out: out}
}

func (l *stderrLogger) Started(reason Reason) {
	l.write(fmt.Sprintf("anomaly detected: reason=%s, starting recovery", reason))
}

func (l *stderrLogger) Succeeded(durationMs int64) {
	l.write(fmt.Sprintf("anomaly recovery succeeded after %dms", durationMs))
}

func (l *stderrLogger) Failed(category ErrorCategory) {
	l.write(fmt.Sprintf("anomaly recovery failed: %s", category))
}

func (l *stderrLogger) Close() error {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.closed {
		return ErrLoggerClosed
	}
	l.closed = true
	return nil
}

func (l *stderrLogger) write(msg string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.closed {
		return
	}
	fmt.Fprintf(l.out, "anomaly: WARN %s\n", msg)
}
