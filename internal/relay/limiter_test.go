package relay

import (
	"sync"
	"testing"
)

func TestLimiter_AcquireRelease(t *testing.T) {
	l := NewLimiter(10)
	if !l.Acquire() {
		t.Fatal("expected Acquire to succeed")
	}
	if l.Current() != 1 {
		t.Errorf("expected current 1, got %d", l.Current())
	}
	l.Release()
	if l.Current() != 0 {
		t.Errorf("expected current 0 after release, got %d", l.Current())
	}
}

func TestLimiter_MaxReached(t *testing.T) {
	l := NewLimiter(MaxConnections)
	for i := int64(0); i < MaxConnections; i++ {
		if !l.Acquire() {
			t.Fatalf("Acquire failed at connection %d", i)
		}
	}
	if l.Acquire() {
		t.Error("expected 151st Acquire to fail")
	}
	if l.Current() != MaxConnections {
		t.Errorf("expected current %d, got %d", MaxConnections, l.Current())
	}
	// cleanup
	for i := int64(0); i < MaxConnections; i++ {
		l.Release()
	}
}

func TestLimiter_ReleaseAfterMax(t *testing.T) {
	l := NewLimiter(MaxConnections)
	for i := int64(0); i < MaxConnections; i++ {
		l.Acquire()
	}
	if l.Acquire() {
		t.Fatal("expected Acquire to fail at max")
	}
	l.Release()
	if !l.Acquire() {
		t.Error("expected Acquire to succeed after Release")
	}
	// cleanup
	for i := int64(0); i < MaxConnections; i++ {
		l.Release()
	}
}

func TestLimiter_Concurrent(t *testing.T) {
	l := NewLimiter(MaxConnections)
	var wg sync.WaitGroup
	rejected := int64(0)
	var mu sync.Mutex

	for i := 0; i < int(MaxConnections)*2; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if l.Acquire() {
				// verify current never exceeds max
				cur := l.Current()
				if cur > MaxConnections {
					t.Errorf("current %d exceeds max %d", cur, MaxConnections)
				}
				l.Release()
			} else {
				mu.Lock()
				rejected++
				mu.Unlock()
			}
		}()
	}

	wg.Wait()

	if l.Current() != 0 {
		t.Errorf("expected current 0 after all goroutines done, got %d", l.Current())
	}
}
