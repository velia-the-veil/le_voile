package relay

import (
	"context"
	"net"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

const (
	// DailyQuotaBytes is the per-IP daily download quota (10 GiB).
	DailyQuotaBytes int64 = 10 * 1024 * 1024 * 1024

	// HourlyQuotaBytes is the per-IP hourly download quota (1 GiB).
	HourlyQuotaBytes int64 = 1 * 1024 * 1024 * 1024

	// SubnetQuotaMultiplier scales the per-IP quota up to the aggregate cap
	// applied to a whole /24 (IPv4) or /64 (IPv6) prefix. Mitigates H4
	// (audit sécurité) : an attacker rotating addresses inside a single
	// subnet hits the subnet cap long before each individual IP's cap,
	// so the amplification factor is bounded. Set to 4 so a household or
	// small office NAT'd behind a single public IP — or a cell carrier
	// CG-NAT pool — still has legitimate headroom.
	SubnetQuotaMultiplier int64 = 4

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
//
// The counter is applied twice: once keyed by the exact client IP (existing
// behaviour) and once keyed by the /24 (IPv4) or /64 (IPv6) subnet prefix
// with a SubnetQuotaMultiplier cap. The subnet bucket is what defends
// against an attacker rotating addresses inside a single range to
// multiply throughput (fix H4 audit sécurité).
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

	// Subnet-level aggregate — bypasses individual-IP limits to catch
	// rotation attacks. Silent on individual per-IP transitions so we
	// don't double-count metrics.
	subnetKey := subnetPrefix(ip)
	if subnetKey != "" && subnetKey != ip {
		sDaily, sHourly := bl.addBytesScaled(subnetKey, n, SubnetQuotaMultiplier)
		dailyExceeded = dailyExceeded || sDaily
		hourlyExceeded = hourlyExceeded || sHourly
	}

	if dailyExceeded || hourlyExceeded {
		sleepDuration := time.Duration(n) * time.Second / time.Duration(ThrottleBytesPerSec)
		select {
		case <-time.After(sleepDuration):
		case <-ctx.Done():
		}
	}
}

// addBytesScaled is addBytes with an effective quota multiplied by scale.
// Used for the subnet-aggregate bucket so legitimate multi-user NAT stays
// under the cap while rotation attacks still hit it.
func (bl *BandwidthLimiter) addBytesScaled(key string, n int, scale int64) (bool, bool) {
	val, _ := bl.ips.LoadOrStore(key, &bandwidthState{})
	st := val.(*bandwidthState)
	st.markedForDeletion.Store(false)
	st.lastSeen.Store(time.Now().Unix())

	today := currentDayUnix()
	if st.dayTimestamp.Load() < today {
		st.resetMu.Lock()
		if st.dayTimestamp.Load() < today {
			st.bytesUsed.Store(0)
			st.dayTimestamp.Store(today)
		}
		st.resetMu.Unlock()
	}
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
	return dailyTotal >= bl.quota*scale, hourlyTotal >= bl.hourlyQuota*scale
}

// subnetPrefix returns the /24 (IPv4) or /64 (IPv6) prefix string for use as
// a quota key. Empty string on parse failure. The prefix form is distinct
// from bare IPs (contains "/") so the subnet bucket never collides with an
// individual-IP bucket in the shared sync.Map.
func subnetPrefix(ip string) string {
	// Strip any bracketed-IPv6 or zone-id suffix.
	clean := strings.TrimSpace(ip)
	if strings.HasPrefix(clean, "[") {
		if end := strings.IndexByte(clean, ']'); end > 0 {
			clean = clean[1:end]
		}
	}
	if pct := strings.IndexByte(clean, '%'); pct >= 0 {
		clean = clean[:pct]
	}
	parsed := net.ParseIP(clean)
	if parsed == nil {
		return ""
	}
	if v4 := parsed.To4(); v4 != nil {
		return v4.Mask(net.CIDRMask(24, 32)).String() + "/24"
	}
	return parsed.Mask(net.CIDRMask(64, 128)).String() + "/64"
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
