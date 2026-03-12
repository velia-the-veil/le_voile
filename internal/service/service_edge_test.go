package service

import (
	"bytes"
	"testing"
)

func TestProgram_StopProxy_NilCancel(t *testing.T) {
	// Verify stopProxy is nil-safe when proxyCancel is nil.
	cfg := Config{RelayDomain: "test.example.com", RelayPubKey: "dGVzdA=="}
	prg := NewProgram(cfg)

	// stopProxy should not panic with nil proxyCancel, errCh, v6ErrCh.
	prg.stopProxy() // should be a no-op
}

func TestProgram_StopProxy_WithCancelAndChannels(t *testing.T) {
	var buf bytes.Buffer
	oldStderr := serviceStderr
	serviceStderr = &buf
	defer func() { serviceStderr = oldStderr }()

	cfg := Config{RelayDomain: "test.example.com", RelayPubKey: "dGVzdA=="}
	prg := NewProgram(cfg)

	// Simulate proxy being started by setting cancel and channels.
	cancelled := false
	prg.proxyCancel = func() { cancelled = true }
	prg.proxyErrCh = make(chan error, 1)
	prg.proxyV6ErrCh = make(chan error, 1)

	// Send errors on channels so stopProxy doesn't block.
	prg.proxyErrCh <- nil
	prg.proxyV6ErrCh <- nil

	prg.stopProxy()

	if !cancelled {
		t.Error("expected proxyCancel to be called")
	}

	// After stopProxy, fields should be nil.
	prg.proxyMu.Lock()
	if prg.proxyCancel != nil {
		t.Error("proxyCancel should be nil after stopProxy")
	}
	if prg.proxyErrCh != nil {
		t.Error("proxyErrCh should be nil after stopProxy")
	}
	if prg.proxyV6ErrCh != nil {
		t.Error("proxyV6ErrCh should be nil after stopProxy")
	}
	prg.proxyMu.Unlock()
}

func TestProgram_StopProxy_DoubleCall(t *testing.T) {
	cfg := Config{RelayDomain: "test.example.com", RelayPubKey: "dGVzdA=="}
	prg := NewProgram(cfg)

	// First call with proxy "running".
	prg.proxyCancel = func() {}
	prg.proxyErrCh = make(chan error, 1)
	prg.proxyErrCh <- nil
	prg.proxyV6ErrCh = make(chan error, 1)
	prg.proxyV6ErrCh <- nil

	prg.stopProxy()

	// Second call should be a no-op (already nil).
	prg.stopProxy()
}

func TestProgram_Cancel_NilSafe(t *testing.T) {
	cfg := Config{RelayDomain: "test.example.com", RelayPubKey: "dGVzdA=="}
	prg := NewProgram(cfg)

	// Cancel should not panic when cancel function is nil.
	prg.Cancel()
}

func TestProgram_DNSManager_NilBeforeStart(t *testing.T) {
	cfg := Config{RelayDomain: "test.example.com", RelayPubKey: "dGVzdA=="}
	prg := NewProgram(cfg)
	if prg.DNSManager() != nil {
		t.Error("DNSManager should be nil before Start")
	}
}

func TestProgram_TunnelClient_NilBeforeStart(t *testing.T) {
	cfg := Config{RelayDomain: "test.example.com", RelayPubKey: "dGVzdA=="}
	prg := NewProgram(cfg)
	if prg.TunnelClient() != nil {
		t.Error("TunnelClient should be nil before Start")
	}
}
