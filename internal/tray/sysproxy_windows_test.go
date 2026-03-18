//go:build windows

package tray

import (
	"encoding/json"
	"os"
	"testing"
)

// mockProtector is a passthrough (no-encryption) DataProtector for tests.
type mockProtector struct{}

func (mockProtector) Protect(data []byte) ([]byte, error)   { return data, nil }
func (mockProtector) Unprotect(data []byte) ([]byte, error) { return data, nil }

// TestWinINETPersistence_SaveReadRemove verifies the file persistence cycle
// for WinINET proxy original state (AC4, Task 7.4).
// Uses a passthrough protector to bypass DPAPI and a temp dir to avoid registry.
func TestWinINETPersistence_SaveReadRemove(t *testing.T) {
	tmpDir := t.TempDir()
	sp := NewSysProxyWithDeps(mockProtector{}, "relay.test.dev", tmpDir)

	// Simulate a persisted state (as Save() would produce after reading the registry).
	state := &proxyOriginalState{
		ProxyEnable:   0,
		ProxyServer:   "old-proxy.example.com:3128",
		ProxyOverride: "localhost;127.0.0.1",
	}
	data, err := json.Marshal(state)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	// atomicWrite is the same path Save() uses after Protect().
	if err := sp.atomicWrite(data); err != nil {
		t.Fatalf("atomicWrite: %v", err)
	}

	// Verify persisted file exists.
	filePath := sp.persistedFilePath()
	if _, err := os.Stat(filePath); err != nil {
		t.Fatalf("persisted file not found after atomicWrite: %v", err)
	}

	// Read back via readPersistedState (mirrors Restore's first step).
	restored, err := sp.readPersistedState()
	if err != nil {
		t.Fatalf("readPersistedState: %v", err)
	}
	if restored.ProxyEnable != state.ProxyEnable {
		t.Errorf("ProxyEnable = %d, want %d", restored.ProxyEnable, state.ProxyEnable)
	}
	if restored.ProxyServer != state.ProxyServer {
		t.Errorf("ProxyServer = %q, want %q", restored.ProxyServer, state.ProxyServer)
	}
	if restored.ProxyOverride != state.ProxyOverride {
		t.Errorf("ProxyOverride = %q, want %q", restored.ProxyOverride, state.ProxyOverride)
	}

	// Remove persisted file (mirrors end of Restore).
	sp.removePersistedFile()
	if _, err := os.Stat(filePath); !os.IsNotExist(err) {
		t.Error("persisted file should be removed after removePersistedFile")
	}
}

// TestWinINETPersistence_RecoverOrphan_NoFile verifies RecoverOrphan is a no-op
// when no crash state file exists.
func TestWinINETPersistence_RecoverOrphan_NoFile(t *testing.T) {
	tmpDir := t.TempDir()
	sp := NewSysProxyWithDeps(mockProtector{}, "relay.test.dev", tmpDir)

	if err := sp.RecoverOrphan(); err != nil {
		t.Errorf("RecoverOrphan with no file should be nil, got: %v", err)
	}
}

// TestWinINETPersistence_ValidateState verifies boundary checks on restored state.
func TestWinINETPersistence_ValidateState(t *testing.T) {
	tmpDir := t.TempDir()
	sp := NewSysProxyWithDeps(mockProtector{}, "relay.test.dev", tmpDir)

	// Valid states.
	if err := sp.validateState(&proxyOriginalState{ProxyEnable: 0}); err != nil {
		t.Errorf("valid state (enable=0) rejected: %v", err)
	}
	if err := sp.validateState(&proxyOriginalState{ProxyEnable: 1, ProxyServer: "127.0.0.1:8080"}); err != nil {
		t.Errorf("valid state (enable=1, server set) rejected: %v", err)
	}

	// Invalid ProxyEnable value.
	if err := sp.validateState(&proxyOriginalState{ProxyEnable: 5}); err == nil {
		t.Error("expected error for invalid ProxyEnable=5")
	}
}

// TestWinINETPersistence_CorruptFile verifies graceful handling of corrupted persisted data.
func TestWinINETPersistence_CorruptFile(t *testing.T) {
	tmpDir := t.TempDir()
	sp := NewSysProxyWithDeps(mockProtector{}, "relay.test.dev", tmpDir)

	// Write garbage data to the persisted file.
	if err := sp.atomicWrite([]byte("not valid json")); err != nil {
		t.Fatalf("atomicWrite: %v", err)
	}

	// readPersistedState should return an error (unmarshal fails).
	_, err := sp.readPersistedState()
	if err == nil {
		t.Error("expected error from readPersistedState with corrupt data")
	}
}
