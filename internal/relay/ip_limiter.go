package relay

import (
	"context"
	"sync"
	"sync/atomic"
	"time"
)

// IPLimiterMaxPerIP is the maximum concurrent connections per IP.
const IPLimiterMaxPerIP int64 = 20

// ipState tracks per-IP connection state using atomic operations.
type ipState struct {
	active           atomic.Int64
	lastSeen         atomic.Int64 // unix timestamp
	markedForDeletion atomic.Bool
}

// IPLimiter enforces per-IP concurrent connection limits.
// Uses sync.Map for lock-free read path and CAS-based cleanup.
type IPLimiter struct {
	ips    sync.Map // map[string]*ipState
	maxPer int64
}

// NewIPLimiter creates a limiter with the given per-IP max.
func NewIPLimiter(maxPerIP int64) *IPLimiter {
	return &IPLimiter{maxPer: maxPerIP}
}

// Acquire tries to acquire a connection slot for the given IP.
// Returns true if under limit, false if rejected.
func (l *IPLimiter) Acquire(ip string) bool {
	val, _ := l.ips.LoadOrStore(ip, &ipState{})
	st := val.(*ipState)

	// Rescue from pending deletion.
	st.markedForDeletion.Store(false)

	n := st.active.Add(1)
	if n > l.maxPer {
		st.active.Add(-1)
		return false
	}
	st.lastSeen.Store(time.Now().Unix())
	return true
}

// Release decrements the active count for the given IP.
func (l *IPLimiter) Release(ip string) {
	val, ok := l.ips.Load(ip)
	if !ok {
		return
	}
	st := val.(*ipState)
	if n := st.active.Add(-1); n < 0 {
		st.active.Add(1)
	}
	st.lastSeen.Store(time.Now().Unix())
}

// StartCleanup runs a goroutine that periodically cleans up idle entries.
// Two-phase CAS: first cycle marks, second cycle deletes (safe against concurrent Acquire).
func (l *IPLimiter) StartCleanup(ctx context.Context) {
	ticker := time.NewTicker(60 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			l.cleanup()
		}
	}
}

func (l *IPLimiter) cleanup() {
	cutoff := time.Now().Add(-5 * time.Minute).Unix()
	l.ips.Range(func(key, val any) bool {
		st := val.(*ipState)
		if st.active.Load() == 0 && st.lastSeen.Load() < cutoff {
			if st.markedForDeletion.Load() {
				// Second cycle: still marked, no Acquire rescued it → safe to delete.
				l.ips.Delete(key)
			} else {
				// First cycle: mark for deletion.
				st.markedForDeletion.Store(true)
			}
		}
		return true
	})
}
