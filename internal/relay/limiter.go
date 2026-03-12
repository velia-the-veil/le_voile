package relay

import "sync/atomic"

// MaxConnections is the default maximum number of concurrent connections.
const MaxConnections int64 = 150

// Limiter tracks concurrent connections using atomic operations.
// Thread-safe without mutex — uses atomic.Int64 for lock-free counting.
type Limiter struct {
	current atomic.Int64
	max     int64
}

// NewLimiter creates a Limiter with the given maximum concurrent connections.
func NewLimiter(max int64) *Limiter {
	return &Limiter{max: max}
}

// Acquire attempts to acquire a connection slot.
// Returns true if the connection is accepted, false if the limit is reached.
func (l *Limiter) Acquire() bool {
	n := l.current.Add(1)
	if n > l.max {
		l.current.Add(-1)
		return false
	}
	return true
}

// Release releases a connection slot. Guards against underflow from double-release bugs.
func (l *Limiter) Release() {
	if n := l.current.Add(-1); n < 0 {
		l.current.Add(1) // restore to prevent further corruption
	}
}

// Current returns the current number of active connections.
func (l *Limiter) Current() int64 {
	return l.current.Load()
}
