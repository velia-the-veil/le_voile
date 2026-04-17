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

	// HourlyQuotaBytes is the per-IP hourly download quota (1 GiB).
	HourlyQuotaBytes int64 = 1 * 1024 * 1024 * 1024

	// ThrottleBytesPerSec is the throttled rate once quota is exceeded (5 Mbps = 625 KB/s).
	ThrottleBytesPerSec int64 = 625_000

	// BandwidthCleanupInterval is how often the cleanup goroutine runs.
	BandwidthCleanupInterval = 60 * time.Second

	// BandwidthInactivityTTL is how long an idle entry survives before cleanup.
	BandwidthInactivityTTL = 24 * time.Hour
)

// bandwidthState tracks per-IP bandwidth usage using atomic operations.
// Separate mutexes protect the day-reset and hour-reset sequences to prevent
// races between timestamp CAS and counter reset without cross-contention.
type bandwidthState struct {
	bytesUsed         atomic.Int64
	dayTimestamp       atomic.Int64 // Unix timestamp of the start of the current UTC day
	hourlyBytesUsed    atomic.Int64
	hourTimestamp      atomic.Int64 // Unix timestamp of the start of the current UTC hour
	hourlyThrottled    atomic.Bool  // true once hourly quota exceeded this hour; reset on hour change
	lastSeen           atomic.Int64 // Unix timestamp of last activity
	markedForDeletion  atomic.Bool
	resetMu            sync.Mutex // protects day reset only
	resetMuHour        sync.Mutex // protects hour reset only
}

// BandwidthLimiter enforces per-IP daily and hourly download quotas with throttling.
// Uses sync.Map for lock-free read path and CAS-based cleanup.
type BandwidthLimiter struct {
	ips         sync.Map // map[string]*bandwidthState
	quota       int64
	hourlyQuota int64
}

// NewBandwidthLimiter creates a limiter with the given per-IP daily quota in bytes.
// The hourly quota defaults to HourlyQuotaBytes.
func NewBandwidthLimiter(quota int64) *BandwidthLimiter {
	return &BandwidthLimiter{quota: quota, hourlyQuota: HourlyQuotaBytes}
}

// NewBandwidthLimiterWithHourly creates a limiter with explicit daily and hourly quotas.
func NewBandwidthLimiterWithHourly(dailyQuota, hourlyQuota int64) *BandwidthLimiter {
	return &BandwidthLimiter{quota: dailyQuota, hourlyQuota: hourlyQuota}
}

// currentDayUnix returns the Unix timestamp of the start of the current UTC day.
func currentDayUnix() int64 {
	return time.Now().UTC().Truncate(24 * time.Hour).Unix()
}

// currentHourUnix returns the Unix timestamp of the start of the current UTC hour.
func currentHourUnix() int64 {
	return time.Now().UTC().Truncate(time.Hour).Unix()
}

// addBytes increments the download counter for the given IP.
// Performs lazy day-reset and hour-reset via double-checked locking.
// Returns (dailyExceeded, hourlyExceeded).
func (bl *BandwidthLimiter) addBytes(ip string, n int) (bool, bool) {
	val, _ := bl.ips.LoadOrStore(ip, &bandwidthState{})
	st := val.(*bandwidthState)

	// Rescue from pending deletion.
	st.markedForDeletion.Store(false)
	st.lastSeen.Store(time.Now().Unix())

	// Lazy day reset — double-checked locking.
	today := currentDayUnix()
	if st.dayTimestamp.Load() < today {
		st.resetMu.Lock()
		if st.dayTimestamp.Load() < today {
			st.bytesUsed.Store(0)
			st.dayTimestamp.Store(today)
		}
		st.resetMu.Unlock()
	}

	// Lazy hour reset — double-checked locking (separate mutex).
	thisHour := currentHourUnix()
	if st.hourTimestamp.Load() < thisHour {
		st.resetMuHour.Lock()
		if st.hourTimestamp.Load() < thisHour {
			st.hourlyBytesUsed.Store(0)
			st.hourlyThrottled.Store(false)
			st.hourTimestamp.Store(thisHour)
		}
		st.resetMuHour.Unlock()
	}

	dailyTotal := st.bytesUsed.Add(int64(n))
	hourlyTotal := st.hourlyBytesUsed.Add(int64(n))

	return dailyTotal >= bl.quota, hourlyTotal >= bl.hourlyQuota
}

// CanOpenTunnel checks whether the given IP is still under the daily quota.
// This is a read-only check (does not increment counters) used to reject new
// tunnel connections at the handler level before dialing upstream.
func (bl *BandwidthLimiter) CanOpenTunnel(ip string) bool {
	val, ok := bl.ips.Load(ip)
	if !ok {
		return true // no state → under quota
	}
	st := val.(*bandwidthState)

	// Perform lazy day reset so stale entries from yesterday don't block.
	today := currentDayUnix()
	if st.dayTimestamp.Load() < today {
		st.resetMu.Lock()
		if st.dayTimestamp.Load() < today {
			st.bytesUsed.Store(0)
			st.dayTimestamp.Store(today)
		}
		st.resetMu.Unlock()
	}

	return st.bytesUsed.Load() < bl.quota
}

// AccountAndThrottle counts n downloaded bytes for the given IP and, if any
// quota is exceeded, sleeps to enforce the throttle rate. The sleep respects
// context cancellation so the relay goroutine can exit promptly.
func (bl *BandwidthLimiter) AccountAndThrottle(ctx context.Context, ip string, n int) {
	dailyExceeded, hourlyExceeded := bl.addBytes(ip, n)
	if hourlyExceeded {
		// Increment counter only on first transition per IP per hour (not per packet).
		val, ok := bl.ips.Load(ip)
		if ok {
			st := val.(*bandwidthState)
			if !st.hourlyThrottled.Swap(true) {
				ThrottledHourlyQuotaTotal.Add(1)
			}
		}
	}
	if dailyExceeded || hourlyExceeded {
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
