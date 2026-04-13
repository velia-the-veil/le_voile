package relay

import (
	"context"
	"sync"
	"sync/atomic"
	"time"
)

const (
	// DailyQuotaBytes is the per-IP daily download quota (10 GiB).
	DailyQuotaBytes int64 = 10 * 1024 * 1024 * 1024

	// ThrottleBytesPerSec is the throttled rate once quota is exceeded (5 Mbps = 625 KB/s).
	ThrottleBytesPerSec int64 = 625_000

	// BandwidthCleanupInterval is how often the cleanup goroutine runs.
	BandwidthCleanupInterval = 60 * time.Second

	// BandwidthInactivityTTL is how long an idle entry survives before cleanup.
	BandwidthInactivityTTL = 24 * time.Hour
)

// bandwidthState tracks per-IP bandwidth usage using atomic operations.
// The resetMu mutex protects only the day-reset sequence to prevent a race
// between dayTimestamp CAS and bytesUsed reset (F1 fix).
type bandwidthState struct {
	bytesUsed         atomic.Int64
	dayTimestamp       atomic.Int64 // Unix timestamp of the start of the current UTC day
	lastSeen          atomic.Int64 // Unix timestamp of last activity
	markedForDeletion atomic.Bool
	resetMu           sync.Mutex // protects day reset only
}

// BandwidthLimiter enforces per-IP daily download quotas with throttling.
// Uses sync.Map for lock-free read path and CAS-based cleanup.
type BandwidthLimiter struct {
	ips   sync.Map // map[string]*bandwidthState
	quota int64
}

// NewBandwidthLimiter creates a limiter with the given per-IP daily quota in bytes.
func NewBandwidthLimiter(quota int64) *BandwidthLimiter {
	return &BandwidthLimiter{quota: quota}
}

// currentDayUnix returns the Unix timestamp of the start of the current UTC day.
func currentDayUnix() int64 {
	return time.Now().UTC().Truncate(24 * time.Hour).Unix()
}

// addBytes increments the download counter for the given IP.
// Performs a lazy day-reset if the UTC day has changed (double-checked locking).
// Returns true if the quota is exceeded (caller should throttle).
func (bl *BandwidthLimiter) addBytes(ip string, n int) bool {
	val, _ := bl.ips.LoadOrStore(ip, &bandwidthState{})
	st := val.(*bandwidthState)

	// Rescue from pending deletion.
	st.markedForDeletion.Store(false)
	st.lastSeen.Store(time.Now().Unix())

	// Lazy day reset — double-checked locking to avoid race between
	// dayTimestamp update and bytesUsed reset (F1 fix).
	today := currentDayUnix()
	if st.dayTimestamp.Load() < today {
		st.resetMu.Lock()
		if st.dayTimestamp.Load() < today {
			st.bytesUsed.Store(0)
			st.dayTimestamp.Store(today)
		}
		st.resetMu.Unlock()
	}

	total := st.bytesUsed.Add(int64(n))
	return total > bl.quota
}

// AccountAndThrottle counts n downloaded bytes for the given IP and, if the
// daily quota is exceeded, sleeps to enforce the throttle rate. The sleep
// respects context cancellation so the relay goroutine can exit promptly.
func (bl *BandwidthLimiter) AccountAndThrottle(ctx context.Context, ip string, n int) {
	if exceeded := bl.addBytes(ip, n); exceeded {
		sleepDuration := time.Duration(n) * time.Second / time.Duration(ThrottleBytesPerSec)
		select {
		case <-time.After(sleepDuration):
		case <-ctx.Done():
		}
	}
}

// StartCleanup runs a goroutine that periodically cleans up idle entries.
// Two-phase CAS: first cycle marks, second cycle deletes (safe against concurrent addBytes).
func (bl *BandwidthLimiter) StartCleanup(ctx context.Context) {
	ticker := time.NewTicker(BandwidthCleanupInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			bl.cleanup()
		}
	}
}

func (bl *BandwidthLimiter) cleanup() {
	cutoff := time.Now().Add(-BandwidthInactivityTTL).Unix()
	bl.ips.Range(func(key, val any) bool {
		st := val.(*bandwidthState)
		if st.lastSeen.Load() < cutoff {
			if st.markedForDeletion.Load() {
				// Second cycle: still marked, no addBytes rescued it -> safe to delete.
				bl.ips.Delete(key)
			} else {
				// First cycle: mark for deletion.
				st.markedForDeletion.Store(true)
			}
		}
		return true
	})
}
