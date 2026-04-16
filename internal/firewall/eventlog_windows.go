//go:build windows

package firewall

import (
	"fmt"

	"golang.org/x/sys/windows/svc/eventlog"
)

const eventLogSource = "LeVoile"

// eventLogger wraps a firewall.Logger and additionally writes key events to
// the Windows Event Log (source "LeVoile"). Messages contain no user data
// (no relay IP, no TUN name) per NFR22a.
type eventLogger struct {
	inner Logger
	elog  *eventlog.Log
}

// newEventLogger creates a logger that writes to both the inner Logger and
// the Windows Event Log. If the Event Log cannot be opened (source not
// registered), falls back to inner-only logging without error.
func newEventLogger(inner Logger) Logger {
	el, err := eventlog.Open(eventLogSource)
	if err != nil {
		// Source not registered (not installed as service) — fallback to inner.
		return inner
	}
	return &eventLogger{inner: inner, elog: el}
}

func (l *eventLogger) Infof(format string, args ...any) {
	msg := fmt.Sprintf(format, args...)
	if l.inner != nil {
		l.inner.Infof("%s", msg)
	}
	if l.elog != nil {
		_ = l.elog.Info(1, msg)
	}
}

func (l *eventLogger) Warnf(format string, args ...any) {
	msg := fmt.Sprintf(format, args...)
	if l.inner != nil {
		l.inner.Warnf("%s", msg)
	}
	if l.elog != nil {
		_ = l.elog.Warning(2, msg)
	}
}

func (l *eventLogger) Errorf(format string, args ...any) {
	msg := fmt.Sprintf(format, args...)
	if l.inner != nil {
		l.inner.Errorf("%s", msg)
	}
	if l.elog != nil {
		_ = l.elog.Error(3, msg)
	}
}

func (l *eventLogger) Debugf(format string, args ...any) {
	// Debug messages go to inner logger only — not Event Log.
	if l.inner != nil {
		l.inner.Debugf(format, args...)
	}
}
