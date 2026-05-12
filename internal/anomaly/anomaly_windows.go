//go:build windows

package anomaly

import (
	"fmt"
	"sync"

	"golang.org/x/sys/windows/svc/eventlog"
)

const eventLogSource = "LeVoile"

// Event Log levels — mirror the firewall package convention so operators
// looking at the unified "LeVoile" source see a consistent numbering.
const (
	eventLogLevelInfo    uint32 = 1
	eventLogLevelWarning uint32 = 2
	eventLogLevelError   uint32 = 3
)

// eventLogLogger writes anomaly events to the Windows Event Log. When
// eventlog.Open fails (e.g. the source has not been registered yet in
// development runs without the NSIS installer), it falls back to the
// embedded stderrLogger so Started/Succeeded/Failed still produce
// operator-visible output.
type eventLogLogger struct {
	fallback *stderrLogger
	mu       sync.Mutex
	elog     *eventlog.Log
}

// NewLogger returns a Logger that writes to the Windows Event Log with
// source "LeVoile". When the source is not registered (dev builds), it
// silently falls back to stderr.
func NewLogger() Logger {
	fb := newStderrLogger(nil)
	el, err := eventlog.Open(eventLogSource)
	if err != nil {
		// Source not registered — fall back to stderr only.
		return fb
	}
	return &eventLogLogger{fallback: fb, elog: el}
}

func (l *eventLogLogger) Started(reason Reason) {
	msg := fmt.Sprintf("anomaly detected: reason=%s, starting recovery", reason)
	l.emitWarning(msg)
}

func (l *eventLogLogger) Succeeded(durationMs int64) {
	msg := fmt.Sprintf("anomaly recovery succeeded after %dms", durationMs)
	l.emitWarning(msg)
}

func (l *eventLogLogger) Failed(category ErrorCategory) {
	msg := fmt.Sprintf("anomaly recovery failed: %s", category)
	l.emitWarning(msg)
}

func (l *eventLogLogger) emitWarning(msg string) {
	// Always mirror to stderr so the unified service log retains a copy,
	// matching the firewall eventLogger pattern.
	l.fallback.write(msg)

	l.mu.Lock()
	elog := l.elog
	l.mu.Unlock()
	if elog != nil {
		_ = elog.Warning(eventLogLevelWarning, msg)
	}
}

func (l *eventLogLogger) Close() error {
	l.mu.Lock()
	elog := l.elog
	l.elog = nil
	l.mu.Unlock()
	if elog != nil {
		_ = elog.Close()
	}
	_ = l.fallback.Close()
	return nil
}
