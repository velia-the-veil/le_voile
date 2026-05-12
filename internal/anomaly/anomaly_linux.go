//go:build linux

package anomaly

import (
	"fmt"
	"log/syslog"
	"sync"
)

// syslogTag is the process identifier passed to syslog.Dial so journald
// surfaces anomaly events under "levoile" (e.g. `journalctl -t levoile`).
const syslogTag = "levoile"

// journaldLogger writes anomaly events through log/syslog. On modern
// systemd distros the journal captures syslog automatically, so
// `journalctl -t levoile` shows the same messages an operator would see
// on Windows via Event Viewer.
//
// If syslog.Dial fails (rare — requires either /dev/log or a running
// syslog/journald daemon), the logger falls back to the embedded
// stderrLogger so the service still boots cleanly.
type journaldLogger struct {
	fallback *stderrLogger
	mu       sync.Mutex
	w        *syslog.Writer
}

// NewLogger returns a Logger that routes messages through log/syslog at
// LOG_WARNING|LOG_DAEMON with tag "levoile".
func NewLogger() Logger {
	fb := newStderrLogger(nil)
	w, err := syslog.Dial("", "", syslog.LOG_WARNING|syslog.LOG_DAEMON, syslogTag)
	if err != nil {
		return fb
	}
	return &journaldLogger{fallback: fb, w: w}
}

func (l *journaldLogger) Started(reason Reason) {
	msg := fmt.Sprintf("anomaly detected: reason=%s, starting recovery", reason)
	l.emitWarning(msg)
}

func (l *journaldLogger) Succeeded(durationMs int64) {
	msg := fmt.Sprintf("anomaly recovery succeeded after %dms", durationMs)
	l.emitWarning(msg)
}

func (l *journaldLogger) Failed(category ErrorCategory) {
	msg := fmt.Sprintf("anomaly recovery failed: %s", category)
	l.emitWarning(msg)
}

func (l *journaldLogger) emitWarning(msg string) {
	l.fallback.write(msg)

	l.mu.Lock()
	w := l.w
	l.mu.Unlock()
	if w != nil {
		_ = w.Warning(msg)
	}
}

func (l *journaldLogger) Close() error {
	l.mu.Lock()
	w := l.w
	l.w = nil
	l.mu.Unlock()
	if w != nil {
		_ = w.Close()
	}
	_ = l.fallback.Close()
	return nil
}
