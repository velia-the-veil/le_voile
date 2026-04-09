//go:build windows

package ui

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

// mockProtector is a no-op DPAPI replacement that returns data as-is.
type mockProtector struct{}

func (mockProtector) Protect(data []byte) ([]byte, error)   { return data, nil }
func (mockProtector) Unprotect(data []byte) ([]byte, error) { return data, nil }

// TestSysProxyPersistence verifies that Save persists proxy state to disk
// and Restore can read it back, deleting the file afterward.
func TestSysProxyPersistence(t *testing.T) {
	dir := t.TempDir()
	sp := &SysProxy{
		protector:   mockProtector{},
		relayDomain: "test.example",
		dataDir:     dir,
	}

	// Manually write a known state to the persisted file (simulating Save).
	original := proxyOriginalState{
		ProxyEnable:   0,
		ProxyServer:   "",
		ProxyOverride: "",
	}
	data, err := json.Marshal(original)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	// Use mockProtector (no-op encrypt), write to file.
	encrypted, err := sp.protector.Protect(data)
	if err != nil {
		t.Fatalf("protect: %v", err)
	}
	filePath := filepath.Join(dir, proxyOriginalFile)
	if err := os.WriteFile(filePath, encrypted, 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}

	// Verify file exists.
	if _, err := os.Stat(filePath); err != nil {
		t.Fatalf("persisted file should exist: %v", err)
	}

	// Read back and verify.
	state, err := sp.readPersistedState()
	if err != nil {
		t.Fatalf("readPersistedState: %v", err)
	}
	if state.ProxyEnable != 0 {
		t.Errorf("ProxyEnable = %d, want 0", state.ProxyEnable)
	}
	if state.ProxyServer != "" {
		t.Errorf("ProxyServer = %q, want empty", state.ProxyServer)
	}

	// Validate state.
	if err := sp.validateState(state); err != nil {
		t.Errorf("validateState: %v", err)
	}

	// Remove persisted file (simulating successful Restore cleanup).
	sp.removePersistedFile()
	if _, err := os.Stat(filePath); !os.IsNotExist(err) {
		t.Error("persisted file should be removed after restore")
	}
}

// TestSysProxyPersistence_AtomicWrite verifies atomicWrite creates the file.
func TestSysProxyPersistence_AtomicWrite(t *testing.T) {
	dir := t.TempDir()
	sp := &SysProxy{
		protector:   mockProtector{},
		relayDomain: "test.example",
		dataDir:     dir,
	}

	data := []byte(`{"proxy_enable":1,"proxy_server":"127.0.0.1:8080","proxy_override":"localhost"}`)
	if err := sp.atomicWrite(data); err != nil {
		t.Fatalf("atomicWrite: %v", err)
	}

	// Read back.
	got, err := os.ReadFile(sp.persistedFilePath())
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if string(got) != string(data) {
		t.Errorf("atomicWrite data mismatch: got %q", string(got))
	}
}
