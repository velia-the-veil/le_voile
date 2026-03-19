package httpproxy

import (
	"bytes"
	"context"
	"io"
	"net"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestDomainKey_RegisteredDomain(t *testing.T) {
	got := domainKey("dl1.steamcontent.com:443")
	if got != "steamcontent.com" {
		t.Errorf("domainKey(dl1.steamcontent.com:443) = %q, want steamcontent.com", got)
	}
}

func TestDomainKey_SharedCDN(t *testing.T) {
	got := domainKey("audio-ak.akamaized.net:443")
	if got != "audio-ak.akamaized.net" {
		t.Errorf("domainKey(audio-ak.akamaized.net:443) = %q, want audio-ak.akamaized.net", got)
	}
}

func TestDomainKey_IPAddress(t *testing.T) {
	got := domainKey("93.184.216.34:443")
	if got != "93.184.216.34" {
		t.Errorf("domainKey(93.184.216.34:443) = %q, want 93.184.216.34", got)
	}
}

func TestDomainKey_PSLError(t *testing.T) {
	// Single-label hostname — EffectiveTLDPlusOne fails.
	got := domainKey("localhost:8080")
	if got != "localhost" {
		t.Errorf("domainKey(localhost:8080) = %q, want localhost", got)
	}
}

func TestAddBytes_UnderThreshold(t *testing.T) {
	vt := NewVolumeTracker(1000)
	if vt.AddBytes("example.com:443", 500) {
		t.Error("AddBytes returned true under threshold")
	}
	if vt.IsBypassed("example.com:443") {
		t.Error("IsBypassed returned true under threshold")
	}
}

func TestAddBytes_ExceedsThreshold(t *testing.T) {
	vt := NewVolumeTracker(1000)
	if vt.AddBytes("example.com:443", 500) {
		t.Error("first AddBytes returned true")
	}
	if !vt.AddBytes("example.com:443", 600) {
		t.Error("AddBytes did not return true when exceeding threshold")
	}
	if !vt.IsBypassed("example.com:443") {
		t.Error("IsBypassed returned false after threshold exceeded")
	}
}

func TestAddBytes_WindowReset(t *testing.T) {
	vt := NewVolumeTracker(1000)
	vt.AddBytes("example.com:443", 800)

	// Simulate expired window by manipulating windowStart.
	val, _ := vt.domains.Load("example.com")
	st := val.(*domainState)
	st.windowStart.Store(time.Now().Add(-2 * time.Hour).Unix())

	// This should reset the counter to 0 then add 200 — under threshold.
	if vt.AddBytes("example.com:443", 200) {
		t.Error("AddBytes returned true after window reset")
	}
	if st.bytesUsed.Load() != 200 {
		t.Errorf("bytesUsed = %d, want 200", st.bytesUsed.Load())
	}
}

func TestIsBypassed_CooldownExpiry(t *testing.T) {
	vt := NewVolumeTracker(100)
	vt.AddBytes("example.com:443", 200) // trigger bypass

	if !vt.IsBypassed("example.com:443") {
		t.Fatal("expected bypassed after threshold exceeded")
	}

	// Simulate cooldown expiry.
	val, _ := vt.domains.Load("example.com")
	st := val.(*domainState)
	st.bypassedAt.Store(time.Now().Add(-25 * time.Hour).Unix())

	if vt.IsBypassed("example.com:443") {
		t.Error("IsBypassed returned true after cooldown expired")
	}
}

func TestIsBypassed_DirectFailed(t *testing.T) {
	vt := NewVolumeTracker(100)
	vt.AddBytes("example.com:443", 200) // trigger bypass

	// Record 3 failures.
	for i := 0; i < 3; i++ {
		vt.RecordDirectFailure("example.com:443")
	}

	if vt.IsBypassed("example.com:443") {
		t.Error("IsBypassed returned true after 3 direct failures")
	}
}

func TestRegisterUnregister(t *testing.T) {
	vt := NewVolumeTracker(1000)
	c1, c2 := net.Pipe()
	defer c1.Close()
	defer c2.Close()

	id := vt.Register("example.com:443", c1)
	if id == 0 {
		t.Error("expected non-zero connection ID")
	}

	// Verify the connection is registered.
	val, ok := vt.conns.Load("example.com")
	if !ok {
		t.Fatal("connSet not found for domain")
	}
	cs := val.(*connSet)
	cs.mu.Lock()
	if len(cs.conns) != 1 {
		t.Errorf("expected 1 connection, got %d", len(cs.conns))
	}
	cs.mu.Unlock()

	vt.Unregister("example.com:443", id)

	// After unregister, connection should be removed from the set.
	val2, ok := vt.conns.Load("example.com")
	if !ok {
		// connSet may still exist (cleanup handles removal) — both are fine.
		return
	}
	cs2 := val2.(*connSet)
	cs2.mu.Lock()
	if len(cs2.conns) != 0 {
		t.Errorf("expected 0 connections after unregister, got %d", len(cs2.conns))
	}
	cs2.mu.Unlock()
}

func TestCloseAll_ClosesRegisteredConns(t *testing.T) {
	vt := NewVolumeTracker(1000)
	s1, c1 := net.Pipe()
	s2, c2 := net.Pipe()
	defer s1.Close()
	defer s2.Close()

	vt.Register("example.com:443", c1)
	vt.Register("example.com:443", c2)

	vt.closeAll("example.com")

	// Connections should be closed — write should fail.
	if _, err := c1.Write([]byte("x")); err == nil {
		t.Error("expected write to closed conn c1 to fail")
	}
	if _, err := c2.Write([]byte("x")); err == nil {
		t.Error("expected write to closed conn c2 to fail")
	}
}

func TestAddBytes_TriggersCloseAll(t *testing.T) {
	vt := NewVolumeTracker(100)
	s1, c1 := net.Pipe()
	defer s1.Close()

	vt.Register("example.com:443", c1)
	vt.AddBytes("example.com:443", 200) // exceeds threshold → closeAll

	if _, err := c1.Write([]byte("x")); err == nil {
		t.Error("expected write to closed conn to fail after threshold exceeded")
	}
}

func TestCleanup_TwoPhase(t *testing.T) {
	vt := NewVolumeTracker(1000)
	vt.AddBytes("example.com:443", 10) // creates entry

	// Set lastSeen to be old enough for cleanup.
	val, _ := vt.domains.Load("example.com")
	st := val.(*domainState)
	st.lastSeen.Store(time.Now().Add(-25 * time.Hour).Unix())

	// First cleanup — marks for deletion.
	vt.cleanup()
	if !st.markedForDeletion.Load() {
		t.Error("expected markedForDeletion after first cleanup")
	}
	if _, ok := vt.domains.Load("example.com"); !ok {
		t.Error("entry should still exist after first cleanup")
	}

	// Second cleanup — deletes.
	vt.cleanup()
	if _, ok := vt.domains.Load("example.com"); ok {
		t.Error("entry should be deleted after second cleanup")
	}
}

func TestCleanup_CASRescue(t *testing.T) {
	vt := NewVolumeTracker(1000)
	vt.AddBytes("example.com:443", 10)

	val, _ := vt.domains.Load("example.com")
	st := val.(*domainState)
	st.lastSeen.Store(time.Now().Add(-25 * time.Hour).Unix())

	// First cleanup — marks.
	vt.cleanup()
	if !st.markedForDeletion.Load() {
		t.Fatal("expected marked after first cleanup")
	}

	// Simulate access between cleanups — rescue.
	vt.AddBytes("example.com:443", 5)

	// Second cleanup — should not delete (rescued).
	vt.cleanup()
	if _, ok := vt.domains.Load("example.com"); !ok {
		t.Error("entry should survive cleanup after rescue")
	}
}

func TestConcurrent_AddBytes(t *testing.T) {
	vt := NewVolumeTracker(100_000_000) // high threshold — don't trigger bypass
	var wg sync.WaitGroup
	const goroutines = 50
	const bytesEach = 1000

	for i := 0; i < goroutines; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			vt.AddBytes("example.com:443", bytesEach)
		}()
	}
	wg.Wait()

	val, ok := vt.domains.Load("example.com")
	if !ok {
		t.Fatal("domain state not found")
	}
	st := val.(*domainState)
	expected := int64(goroutines * bytesEach)
	if got := st.bytesUsed.Load(); got != expected {
		t.Errorf("bytesUsed = %d, want %d", got, expected)
	}
}

func TestCountingReader_CountsBytes(t *testing.T) {
	vt := NewVolumeTracker(100_000)
	data := bytes.Repeat([]byte("x"), 500)
	cr := vt.WrapReader("example.com:443", bytes.NewReader(data))

	buf := make([]byte, 1024)
	n, err := cr.Read(buf)
	if err != nil {
		t.Fatalf("Read error: %v", err)
	}
	if n != 500 {
		t.Errorf("Read returned %d bytes, want 500", n)
	}

	val, ok := vt.domains.Load("example.com")
	if !ok {
		t.Fatal("domain state not found after read")
	}
	st := val.(*domainState)
	if st.bytesUsed.Load() != 500 {
		t.Errorf("bytesUsed = %d, want 500", st.bytesUsed.Load())
	}
}

func TestCountingReader_StopsOnBypass(t *testing.T) {
	vt := NewVolumeTracker(100) // low threshold
	data := bytes.Repeat([]byte("x"), 200)
	cr := vt.WrapReader("example.com:443", bytes.NewReader(data))

	// Read all — should trigger bypass.
	io.ReadAll(cr)

	if !cr.Stopped() {
		t.Error("expected Stopped() to be true after threshold exceeded")
	}
	if !vt.IsBypassed("example.com:443") {
		t.Error("expected domain to be bypassed")
	}
}

func TestStartCleanup_RespectsContext(t *testing.T) {
	vt := NewVolumeTracker(1000)
	ctx, cancel := context.WithCancel(context.Background())

	var done atomic.Bool
	go func() {
		vt.StartCleanup(ctx)
		done.Store(true)
	}()

	cancel()

	// Wait for goroutine to exit.
	deadline := time.After(2 * time.Second)
	for !done.Load() {
		select {
		case <-deadline:
			t.Fatal("StartCleanup did not exit after context cancel")
		default:
			time.Sleep(10 * time.Millisecond)
		}
	}
}
