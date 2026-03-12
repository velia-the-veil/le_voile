package service

import (
	"bytes"
	"context"
	"testing"
	"time"
)

// TestSetSTUNEnabled_NilComponents verifies that setSTUNEnabled does not panic
// when both stunInterceptor and stunRelayer are nil (before startSTUN).
func TestSetSTUNEnabled_NilComponents(t *testing.T) {
	t.Parallel()

	cfg := Config{RelayDomain: "test.example.com", RelayPubKey: "dGVzdA=="}
	prg := NewProgram(cfg)

	// Must not panic — both interceptor and relayer are nil.
	prg.setSTUNEnabled(false)
	prg.setSTUNEnabled(true)
}

// TestStartSTUN_NilTunnelClient verifies that startSTUN creates an interceptor
// without a relayer when tunnelClient is nil (interceptor-only mode).
// Note: startSTUN uses ports 3478/5349 which may already be bound. In that case,
// startSTUN is best-effort and sets stunInterceptor to nil. We test both cases.
func TestStartSTUN_NilTunnelClient(t *testing.T) {
	t.Parallel()

	// Suppress stderr output from best-effort failure.
	var buf bytes.Buffer
	oldStderr := serviceStderr
	serviceStderr = &buf
	defer func() { serviceStderr = oldStderr }()

	cfg := Config{RelayDomain: "test.example.com", RelayPubKey: "dGVzdA=="}
	prg := NewProgram(cfg)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	prg.startSTUN(ctx)
	defer prg.stopSTUN()

	// Regardless of whether ports were available, relayer must be nil
	// (tunnelClient is nil → no relayer created).
	prg.stunMu.Lock()
	hasRelayer := prg.stunRelayer != nil
	prg.stunMu.Unlock()

	if hasRelayer {
		t.Error("[P1] stunRelayer should be nil when tunnelClient is nil")
	}

	// If port was available, STUN is active; if not, it's inactive (best-effort).
	// Both are valid outcomes — we just verify no panic occurred.
	t.Logf("[P1] STUNActive = %v (depends on port 3478 availability)", prg.STUNActive())
}

// TestStopSTUN_DoubleCall verifies that calling stopSTUN twice does not panic.
func TestStopSTUN_DoubleCall(t *testing.T) {
	t.Parallel()

	// Suppress stderr.
	var buf bytes.Buffer
	oldStderr := serviceStderr
	serviceStderr = &buf
	defer func() { serviceStderr = oldStderr }()

	cfg := Config{RelayDomain: "test.example.com", RelayPubKey: "dGVzdA=="}
	prg := NewProgram(cfg)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	prg.startSTUN(ctx)
	prg.stopSTUN()
	// Second call should be a no-op (already cleaned up).
	prg.stopSTUN()
}

// TestSetSTUNEnabled_AfterStop verifies that setSTUNEnabled is safe to call
// after stopSTUN has cleared the interceptor and relayer references.
func TestSetSTUNEnabled_AfterStop(t *testing.T) {
	t.Parallel()

	// Suppress stderr.
	var buf bytes.Buffer
	oldStderr := serviceStderr
	serviceStderr = &buf
	defer func() { serviceStderr = oldStderr }()

	cfg := Config{RelayDomain: "test.example.com", RelayPubKey: "dGVzdA=="}
	prg := NewProgram(cfg)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	prg.startSTUN(ctx)
	prg.stopSTUN()

	// Must not panic — interceptor and relayer have been nilled by stopSTUN.
	prg.setSTUNEnabled(false)
	prg.setSTUNEnabled(true)
}

// TestSTUNActive_AfterStop verifies that STUNActive returns false after stopSTUN.
func TestSTUNActive_AfterStop(t *testing.T) {
	t.Parallel()

	// Suppress stderr.
	var buf bytes.Buffer
	oldStderr := serviceStderr
	serviceStderr = &buf
	defer func() { serviceStderr = oldStderr }()

	cfg := Config{RelayDomain: "test.example.com", RelayPubKey: "dGVzdA=="}
	prg := NewProgram(cfg)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	prg.startSTUN(ctx)

	// startSTUN is best-effort; skip test if port was unavailable.
	if !prg.STUNActive() {
		t.Skip("[P0] port 3478 already in use, skipping STUN lifecycle test")
	}

	prg.stopSTUN()
	time.Sleep(50 * time.Millisecond)

	if prg.STUNActive() {
		t.Error("[P0] STUNActive should be false after stopSTUN")
	}
}
