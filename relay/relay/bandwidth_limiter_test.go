package relay

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestBandwidthLimiter_AddBytesUnderQuota(t *testing.T) {
	bl := NewBandwidthLimiter(1000)

	tests := []struct {
		name  string
		bytes int
		want  bool // exceeded?
	}{
		{"first_add", 100, false},
		{"second_add", 200, false},
		{"just_at_quota", 700, true}, // 100+200+700=1000 >= quota(1000)
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, _ := bl.addBytes("10.0.0.1", tt.bytes)
			if got != tt.want {
				t.Errorf("addBytes(%d) daily = %v, want %v", tt.bytes, got, tt.want)
			}
		})
	}
}

func TestBandwidthLimiter_AddBytesExceedsQuota(t *testing.T) {
	bl := NewBandwidthLimiter(1000)
	ip := "10.0.0.2"

	// Fill up to quota.
	bl.addBytes(ip, 1000)

	// Next byte should exceed.
	if daily, _ := bl.addBytes(ip, 1); !daily {
		t.Errorf("addBytes should return dailyExceeded=true when quota exceeded")
	}

	// Subsequent calls should also exceed.
	if daily, _ := bl.addBytes(ip, 500); !daily {
		t.Errorf("addBytes should still return dailyExceeded=true after quota exceeded")
	}
}

func TestBandwidthLimiter_LazyReset(t *testing.T) {
	bl := NewBandwidthLimiter(1000)
	ip := "10.0.0.3"

	// Add bytes to exceed quota.
	bl.addBytes(ip, 1001)
	if daily, _ := bl.addBytes(ip, 1); !daily {
		t.Fatalf("should be over quota")
	}

	// Simulate yesterday by backdating dayTimestamp.
	val, ok := bl.ips.Load(ip)
	if !ok {
		t.Fatalf("expected ip entry to exist")
	}
	st := val.(*bandwidthState)
	yesterday := time.Now().UTC().Truncate(24*time.Hour).Add(-24*time.Hour).Unix()
	st.dayTimestamp.Store(yesterday)

	// Next addBytes should trigger lazy reset.
	dailyExceeded, _ := bl.addBytes(ip, 100)
	if dailyExceeded {
		t.Errorf("after day reset, 100 bytes should not exceed quota of 1000")
	}

	// Verify counter was reset (should be 100, not 1002+100).
	if st.bytesUsed.Load() != 100 {
		t.Errorf("bytesUsed = %d, want 100 after reset", st.bytesUsed.Load())
	}
}

func TestBandwidthLimiter_CleanupTwoPhase(t *testing.T) {
	bl := NewBandwidthLimiter(DailyQuotaBytes)
	ip := "10.0.0.4"

	// Create an entry.
	bl.addBytes(ip, 100)

	// Backdate lastSeen so it appears stale (>24h ago).
	val, ok := bl.ips.Load(ip)
	if !ok {
		t.Fatalf("expected ip entry to exist")
	}
	st := val.(*bandwidthState)
	st.lastSeen.Store(time.Now().Add(-48 * time.Hour).Unix())

	// First cleanup: should mark for deletion but NOT delete yet.
	bl.cleanup()

	if _, ok := bl.ips.Load(ip); !ok {
		t.Fatalf("entry should still exist after first cleanup (marked, not deleted)")
	}
	if !st.markedForDeletion.Load() {
		t.Errorf("entry should be marked for deletion after first cleanup")
	}

	// Second cleanup: now it should be deleted.
	bl.cleanup()

	if _, ok := bl.ips.Load(ip); ok {
		t.Errorf("entry should be deleted after second cleanup")
	}
}

func TestBandwidthLimiter_CleanupCASRescue(t *testing.T) {
	bl := NewBandwidthLimiter(DailyQuotaBytes)
	ip := "10.0.0.5"

	// Create a stale entry.
	bl.addBytes(ip, 100)

	val, _ := bl.ips.Load(ip)
	st := val.(*bandwidthState)
	st.lastSeen.Store(time.Now().Add(-48 * time.Hour).Unix())

	// First cleanup marks for deletion.
	bl.cleanup()
	if !st.markedForDeletion.Load() {
		t.Fatalf("should be marked after first cleanup")
	}

	// Simulate new traffic: addBytes resets the markedForDeletion flag.
	bl.addBytes(ip, 50)
	if st.markedForDeletion.Load() {
		t.Errorf("addBytes should clear the markedForDeletion flag")
	}

	// Backdate again so cleanup sees it as stale.
	st.lastSeen.Store(time.Now().Add(-48 * time.Hour).Unix())

	// Second cleanup should only mark again (not delete) because the flag was cleared.
	bl.cleanup()

	if _, ok := bl.ips.Load(ip); !ok {
		t.Errorf("entry should survive second cleanup after CAS rescue (re-mark, not delete)")
	}
}

func TestBandwidthLimiter_ConcurrentAddBytes(t *testing.T) {
	const quota int64 = 1_000_000
	const goroutines = 50
	const bytesPerCall = 100
	const iterations = 200

	bl := NewBandwidthLimiter(quota)
	ip := "10.0.0.6"

	var wg sync.WaitGroup
	wg.Add(goroutines)

	var totalAdded atomic.Int64

	for g := 0; g < goroutines; g++ {
		go func() {
			defer wg.Done()
			for i := 0; i < iterations; i++ {
				bl.addBytes(ip, bytesPerCall)
				totalAdded.Add(int64(bytesPerCall))
			}
		}()
	}

	wg.Wait()

	val, ok := bl.ips.Load(ip)
	if !ok {
		t.Fatalf("expected ip entry to exist")
	}
	st := val.(*bandwidthState)
	got := st.bytesUsed.Load()
	expected := totalAdded.Load()

	if got != expected {
		t.Errorf("bytesUsed = %d, want %d (concurrent consistency check)", got, expected)
	}
}

func TestBandwidthLimiter_AccountAndThrottle_UnderQuota(t *testing.T) {
	bl := NewBandwidthLimiter(1_000_000)
	ip := "10.0.0.7"
	ctx := context.Background()

	start := time.Now()
	bl.AccountAndThrottle(ctx, ip, 1024)
	elapsed := time.Since(start)

	// Under quota: should complete nearly instantly (< 50ms).
	if elapsed > 50*time.Millisecond {
		t.Errorf("AccountAndThrottle took %v, expected near-instant (under quota)", elapsed)
	}

	// Verify bytes were counted.
	val, ok := bl.ips.Load(ip)
	if !ok {
		t.Fatalf("expected ip entry to exist")
	}
	st := val.(*bandwidthState)
	if st.bytesUsed.Load() != 1024 {
		t.Errorf("bytesUsed = %d, want 1024", st.bytesUsed.Load())
	}
}

func TestBandwidthLimiter_AccountAndThrottle_OverQuota(t *testing.T) {
	bl := NewBandwidthLimiter(100) // tiny 100-byte quota
	ip := "10.0.0.8"
	ctx := context.Background()

	// Exhaust quota first.
	bl.addBytes(ip, 200)

	// AccountAndThrottle with 6250 bytes at 625000 B/s = 10ms sleep.
	start := time.Now()
	bl.AccountAndThrottle(ctx, ip, 6250)
	elapsed := time.Since(start)

	// Should sleep ~10ms. Allow wide range due to OS scheduling.
	if elapsed < 5*time.Millisecond {
		t.Errorf("AccountAndThrottle took %v, expected at least ~10ms throttle delay", elapsed)
	}
}

func TestBandwidthLimiter_AccountAndThrottle_RespectsContext(t *testing.T) {
	bl := NewBandwidthLimiter(100) // tiny quota
	ip := "10.0.0.10"

	// Exhaust quota.
	bl.addBytes(ip, 200)

	// Cancel context immediately.
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	// AccountAndThrottle with a large chunk that would sleep ~1.6s without cancellation.
	// With cancelled context, it should return nearly instantly.
	start := time.Now()
	bl.AccountAndThrottle(ctx, ip, 1_000_000) // 1MB at 625KB/s = 1.6s
	elapsed := time.Since(start)

	if elapsed > 100*time.Millisecond {
		t.Errorf("AccountAndThrottle took %v with cancelled context, expected near-instant", elapsed)
	}
}

// --- CanOpenTunnel tests (AC2) ---

func TestBandwidthLimiter_CanOpenTunnel_UnderQuota(t *testing.T) {
	bl := NewBandwidthLimiter(1000)
	ip := "10.0.0.30"
	bl.addBytes(ip, 500)

	if !bl.CanOpenTunnel(ip) {
		t.Errorf("CanOpenTunnel should be true (500 < 1000)")
	}
}

func TestBandwidthLimiter_CanOpenTunnel_OverQuota(t *testing.T) {
	bl := NewBandwidthLimiter(1000)
	ip := "10.0.0.31"
	bl.addBytes(ip, 1100)

	if bl.CanOpenTunnel(ip) {
		t.Errorf("CanOpenTunnel should be false (1100 >= 1000)")
	}
}

func TestBandwidthLimiter_CanOpenTunnel_UnknownIP(t *testing.T) {
	bl := NewBandwidthLimiter(1000)
	if !bl.CanOpenTunnel("10.0.0.99") {
		t.Errorf("CanOpenTunnel should be true for unknown IP")
	}
}

func TestBandwidthLimiter_CanOpenTunnel_AfterDayReset(t *testing.T) {
	bl := NewBandwidthLimiter(1000)
	ip := "10.0.0.32"
	bl.addBytes(ip, 1100) // exceed

	if bl.CanOpenTunnel(ip) {
		t.Fatalf("should be over quota before reset")
	}

	// Backdate to yesterday — lazy reset should clear counter.
	val, _ := bl.ips.Load(ip)
	st := val.(*bandwidthState)
	yesterday := time.Now().UTC().Truncate(24*time.Hour).Add(-24*time.Hour).Unix()
	st.dayTimestamp.Store(yesterday)

	if !bl.CanOpenTunnel(ip) {
		t.Errorf("CanOpenTunnel should be true after day reset")
	}
}

// --- Hourly quota tests (AC3, AC5, AC6) ---

func TestBandwidthLimiter_HourlyExceeded(t *testing.T) {
	bl := NewBandwidthLimiter(DailyQuotaBytes) // large daily quota
	ip := "10.0.0.40"

	// Add just over 1 GiB.
	_, hourly := bl.addBytes(ip, int(HourlyQuotaBytes)+1)
	if !hourly {
		t.Errorf("hourlyExceeded should be true (> HourlyQuotaBytes)")
	}
}

func TestBandwidthLimiter_HourlyNotExceeded(t *testing.T) {
	bl := NewBandwidthLimiter(DailyQuotaBytes)
	ip := "10.0.0.41"

	_, hourly := bl.addBytes(ip, int(HourlyQuotaBytes)-1)
	if hourly {
		t.Errorf("hourlyExceeded should be false (< HourlyQuotaBytes)")
	}
}

func TestBandwidthLimiter_HourlyLazyReset(t *testing.T) {
	bl := NewBandwidthLimiter(DailyQuotaBytes)
	ip := "10.0.0.42"

	// Exceed hourly.
	bl.addBytes(ip, int(HourlyQuotaBytes)+100)

	val, _ := bl.ips.Load(ip)
	st := val.(*bandwidthState)

	// Backdate hourTimestamp to previous hour.
	lastHour := time.Now().UTC().Truncate(time.Hour).Add(-time.Hour).Unix()
	st.hourTimestamp.Store(lastHour)

	// Next addBytes should trigger lazy reset.
	_, hourly := bl.addBytes(ip, 50)
	if hourly {
		t.Errorf("hourlyExceeded should be false after hour reset (50 bytes)")
	}
	if st.hourlyBytesUsed.Load() != 50 {
		t.Errorf("hourlyBytesUsed = %d, want 50 after reset", st.hourlyBytesUsed.Load())
	}
}

func TestBandwidthLimiter_HourlyThrottleSleep(t *testing.T) {
	bl := NewBandwidthLimiter(DailyQuotaBytes)
	ip := "10.0.0.43"
	ctx := context.Background()

	// Exceed hourly quota (but under daily).
	bl.addBytes(ip, int(HourlyQuotaBytes)+1)

	// AccountAndThrottle with 6250 bytes should sleep ~10ms.
	start := time.Now()
	bl.AccountAndThrottle(ctx, ip, 6250)
	elapsed := time.Since(start)

	if elapsed < 5*time.Millisecond {
		t.Errorf("AccountAndThrottle took %v, expected at least ~10ms throttle delay", elapsed)
	}
}

func TestBandwidthLimiter_ConcurrentHourly(t *testing.T) {
	const goroutines = 50
	const bytesPerCall = 100
	const iterations = 200

	bl := NewBandwidthLimiter(DailyQuotaBytes)
	ip := "10.0.0.44"

	var wg sync.WaitGroup
	wg.Add(goroutines)

	var totalAdded atomic.Int64

	for g := 0; g < goroutines; g++ {
		go func() {
			defer wg.Done()
			for i := 0; i < iterations; i++ {
				bl.addBytes(ip, bytesPerCall)
				totalAdded.Add(int64(bytesPerCall))
			}
		}()
	}

	wg.Wait()

	val, ok := bl.ips.Load(ip)
	if !ok {
		t.Fatalf("expected ip entry to exist")
	}
	st := val.(*bandwidthState)

	// Check both daily and hourly counters.
	gotDaily := st.bytesUsed.Load()
	gotHourly := st.hourlyBytesUsed.Load()
	expected := totalAdded.Load()

	if gotDaily != expected {
		t.Errorf("bytesUsed = %d, want %d", gotDaily, expected)
	}
	if gotHourly != expected {
		t.Errorf("hourlyBytesUsed = %d, want %d", gotHourly, expected)
	}
}

func TestBandwidthLimiter_StartCleanupRespectsContext(t *testing.T) {
	bl := NewBandwidthLimiter(DailyQuotaBytes)
	ctx, cancel := context.WithCancel(context.Background())

	done := make(chan struct{})
	go func() {
		bl.StartCleanup(ctx)
		close(done)
	}()

	cancel()

	select {
	case <-done:
		// success
	case <-time.After(2 * time.Second):
		t.Fatalf("StartCleanup did not exit after context cancellation")
	}
}
