package relay

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestIPLimiter_AcquireRelease(t *testing.T) {
	tests := []struct {
		name    string
		actions func(l *IPLimiter) bool // returns true if test passed
	}{
		{
			name: "first acquire succeeds",
			actions: func(l *IPLimiter) bool {
				return l.Acquire("10.0.0.1")
			},
		},
		{
			name: "acquire and release then acquire again succeeds",
			actions: func(l *IPLimiter) bool {
				if !l.Acquire("10.0.0.1") {
					return false
				}
				l.Release("10.0.0.1")
				return l.Acquire("10.0.0.1")
			},
		},
		{
			name: "release unknown IP does not panic",
			actions: func(l *IPLimiter) bool {
				l.Release("192.168.0.1") // should be a no-op
				return true
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			l := NewIPLimiter(IPLimiterMaxPerIP)
			if !tt.actions(l) {
				t.Errorf("test %q failed", tt.name)
			}
		})
	}
}

func TestIPLimiter_AcquireUpToLimit(t *testing.T) {
	const limit int64 = 20
	l := NewIPLimiter(limit)
	ip := "10.0.0.5"

	for i := int64(1); i <= limit; i++ {
		if !l.Acquire(ip) {
			t.Fatalf("Acquire #%d should succeed (limit=%d)", i, limit)
		}
	}

	// The 21st must fail.
	if l.Acquire(ip) {
		t.Errorf("Acquire #%d should fail (limit=%d)", limit+1, limit)
	}

	// A second over-limit attempt should also fail.
	if l.Acquire(ip) {
		t.Errorf("Acquire #%d should fail (limit=%d)", limit+2, limit)
	}
}

func TestIPLimiter_ReleaseAllowsNewAcquire(t *testing.T) {
	const limit int64 = 3
	l := NewIPLimiter(limit)
	ip := "10.0.0.6"

	for i := int64(0); i < limit; i++ {
		l.Acquire(ip)
	}
	if l.Acquire(ip) {
		t.Fatalf("should be at limit")
	}

	l.Release(ip)

	if !l.Acquire(ip) {
		t.Errorf("after release, Acquire should succeed")
	}
}

func TestIPLimiter_DoubleReleaseNonNegative(t *testing.T) {
	l := NewIPLimiter(IPLimiterMaxPerIP)
	ip := "10.0.0.7"

	if !l.Acquire(ip) {
		t.Fatalf("initial Acquire should succeed")
	}
	l.Release(ip)
	l.Release(ip) // double release — counter must not go below 0

	// If the counter went negative, we would be able to acquire maxPer+1 times.
	for i := int64(0); i < IPLimiterMaxPerIP; i++ {
		if !l.Acquire(ip) {
			t.Fatalf("Acquire #%d should succeed after double release", i+1)
		}
	}
	if l.Acquire(ip) {
		t.Errorf("should be at limit; double release must not allow extra slots")
	}
}

func TestIPLimiter_CleanupTwoPhase(t *testing.T) {
	l := NewIPLimiter(IPLimiterMaxPerIP)
	ip := "10.0.0.8"

	// Acquire and immediately release so active == 0.
	l.Acquire(ip)
	l.Release(ip)

	// Backdate lastSeen so it appears stale (>5 min ago).
	val, ok := l.ips.Load(ip)
	if !ok {
		t.Fatalf("expected ip entry to exist")
	}
	st := val.(*ipState)
	st.lastSeen.Store(time.Now().Add(-10 * time.Minute).Unix())

	// First cleanup: should mark for deletion but NOT delete yet.
	l.cleanup()

	if _, ok := l.ips.Load(ip); !ok {
		t.Fatalf("entry should still exist after first cleanup (marked, not deleted)")
	}
	if !st.markedForDeletion.Load() {
		t.Errorf("entry should be marked for deletion after first cleanup")
	}

	// Second cleanup: now it should be deleted.
	l.cleanup()

	if _, ok := l.ips.Load(ip); ok {
		t.Errorf("entry should be deleted after second cleanup")
	}
}

func TestIPLimiter_CleanupCASRescue(t *testing.T) {
	l := NewIPLimiter(IPLimiterMaxPerIP)
	ip := "10.0.0.9"

	// Create a stale, inactive entry.
	l.Acquire(ip)
	l.Release(ip)

	val, _ := l.ips.Load(ip)
	st := val.(*ipState)
	st.lastSeen.Store(time.Now().Add(-10 * time.Minute).Unix())

	// First cleanup marks for deletion.
	l.cleanup()
	if !st.markedForDeletion.Load() {
		t.Fatalf("should be marked after first cleanup")
	}

	// Simulate new traffic: Acquire resets the markedForDeletion flag.
	if !l.Acquire(ip) {
		t.Fatalf("Acquire should succeed on a marked entry")
	}
	if st.markedForDeletion.Load() {
		t.Errorf("Acquire should clear the markedForDeletion flag")
	}

	// Release and backdate again so cleanup sees it as stale.
	l.Release(ip)
	st.lastSeen.Store(time.Now().Add(-10 * time.Minute).Unix())

	// Second cleanup should only mark again (not delete) because the flag was cleared.
	l.cleanup()

	if _, ok := l.ips.Load(ip); !ok {
		t.Errorf("entry should survive second cleanup after CAS rescue (re-mark, not delete)")
	}
}

func TestIPLimiter_CleanupSkipsActiveEntries(t *testing.T) {
	l := NewIPLimiter(IPLimiterMaxPerIP)
	ip := "10.0.0.10"

	l.Acquire(ip)

	// Backdate but keep active > 0.
	val, _ := l.ips.Load(ip)
	st := val.(*ipState)
	st.lastSeen.Store(time.Now().Add(-10 * time.Minute).Unix())

	l.cleanup()
	l.cleanup()

	if _, ok := l.ips.Load(ip); !ok {
		t.Errorf("active entries must not be cleaned up regardless of age")
	}
	if st.markedForDeletion.Load() {
		t.Errorf("active entries should not be marked for deletion")
	}
}

func TestIPLimiter_StartCleanupRespectsContext(t *testing.T) {
	l := NewIPLimiter(IPLimiterMaxPerIP)
	ctx, cancel := context.WithCancel(context.Background())

	done := make(chan struct{})
	go func() {
		l.StartCleanup(ctx)
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

func TestIPLimiter_ConcurrentAcquireRelease(t *testing.T) {
	const limit int64 = 20
	const goroutines = 50
	const iterations = 200

	l := NewIPLimiter(limit)
	ip := "10.0.0.11"

	var wg sync.WaitGroup
	wg.Add(goroutines)

	for g := 0; g < goroutines; g++ {
		go func() {
			defer wg.Done()
			for i := 0; i < iterations; i++ {
				if l.Acquire(ip) {
					// Hold briefly then release.
					l.Release(ip)
				}
			}
		}()
	}

	wg.Wait()

	// After all goroutines finish, active count must be 0.
	val, ok := l.ips.Load(ip)
	if !ok {
		// Entry may have never been stored if every Acquire failed, which is
		// unlikely but acceptable.
		return
	}
	st := val.(*ipState)
	active := st.active.Load()
	if active != 0 {
		t.Errorf("expected active count 0 after all releases, got %d", active)
	}
}

func TestIPLimiter_ConcurrentMultipleIPs(t *testing.T) {
	const limit int64 = 5
	const goroutinesPerIP = 10
	const iterations = 100

	l := NewIPLimiter(limit)
	ips := []string{"10.1.0.1", "10.1.0.2", "10.1.0.3", "10.1.0.4"}

	var wg sync.WaitGroup
	wg.Add(len(ips) * goroutinesPerIP)

	for _, ip := range ips {
		for g := 0; g < goroutinesPerIP; g++ {
			go func(addr string) {
				defer wg.Done()
				for i := 0; i < iterations; i++ {
					if l.Acquire(addr) {
						l.Release(addr)
					}
				}
			}(ip)
		}
	}

	wg.Wait()

	for _, ip := range ips {
		val, ok := l.ips.Load(ip)
		if !ok {
			continue
		}
		st := val.(*ipState)
		active := st.active.Load()
		if active != 0 {
			t.Errorf("ip %s: expected active 0, got %d", ip, active)
		}
	}
}

func TestIPLimiter_ConcurrentAcquireNeverExceedsLimit(t *testing.T) {
	const limit int64 = 5
	const goroutines = 50

	l := NewIPLimiter(limit)
	ip := "10.0.0.12"

	// All goroutines acquire at once and hold — at most `limit` should succeed.
	var wg sync.WaitGroup
	wg.Add(goroutines)

	var acquired atomic.Int64

	for g := 0; g < goroutines; g++ {
		go func() {
			defer wg.Done()
			if l.Acquire(ip) {
				acquired.Add(1)
			}
		}()
	}

	wg.Wait()

	got := acquired.Load()
	if got != limit {
		t.Errorf("expected exactly %d acquires to succeed, got %d", limit, got)
	}
}
