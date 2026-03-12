package registry

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestCache_SaveAndLoad(t *testing.T) {
	dir := t.TempDir()
	cachePath := filepath.Join(dir, "relay-cache.toml")
	cache := NewCache(cachePath)

	entries := []RelayEntry{
		{ID: "relay-1", Domain: "r1.example.com", PublicKey: "key1", Signature: "sig1"},
		{ID: "relay-2", Domain: "r2.example.com", PublicKey: "key2", Signature: "sig2"},
	}

	if err := cache.Save(entries, "master-key-b64"); err != nil {
		t.Fatalf("save error: %v", err)
	}

	loaded, masterKey, err := cache.Load()
	if err != nil {
		t.Fatalf("load error: %v", err)
	}
	if masterKey != "master-key-b64" {
		t.Errorf("master key: got %q, want %q", masterKey, "master-key-b64")
	}
	if len(loaded) != 2 {
		t.Fatalf("loaded entries: got %d, want 2", len(loaded))
	}
	if loaded[0].ID != "relay-1" || loaded[0].Domain != "r1.example.com" {
		t.Errorf("entry 0 mismatch: %+v", loaded[0])
	}
	if loaded[1].ID != "relay-2" || loaded[1].Domain != "r2.example.com" {
		t.Errorf("entry 1 mismatch: %+v", loaded[1])
	}
}

func TestCache_LoadNotFound(t *testing.T) {
	cachePath := filepath.Join(t.TempDir(), "nonexistent.toml")
	cache := NewCache(cachePath)

	_, _, err := cache.Load()
	if !errors.Is(err, ErrCacheNotFound) {
		t.Errorf("expected ErrCacheNotFound, got %v", err)
	}
}

func TestCache_AtomicWrite(t *testing.T) {
	dir := t.TempDir()
	cachePath := filepath.Join(dir, "relay-cache.toml")
	cache := NewCache(cachePath)

	entries := []RelayEntry{
		{ID: "relay-1", Domain: "r1.example.com", PublicKey: "key1", Signature: "sig1"},
	}

	if err := cache.Save(entries, "master-key"); err != nil {
		t.Fatalf("save error: %v", err)
	}

	// Verify the .tmp file does not exist after successful save.
	tmpPath := cachePath + ".tmp"
	if _, err := os.Stat(tmpPath); !os.IsNotExist(err) {
		t.Errorf("expected .tmp file to not exist after save, got err: %v", err)
	}

	// Verify the main file exists.
	if _, err := os.Stat(cachePath); err != nil {
		t.Errorf("expected cache file to exist: %v", err)
	}
}

func TestCache_SaveAndLoadLatencies(t *testing.T) {
	dir := t.TempDir()
	cachePath := filepath.Join(dir, "relay-cache.toml")
	cache := NewCache(cachePath)

	// First save some relays so the cache file exists.
	entries := []RelayEntry{
		{ID: "relay-1", Domain: "r1.example.com", PublicKey: "key1", Signature: "sig1"},
		{ID: "relay-2", Domain: "r2.example.com", PublicKey: "key2", Signature: "sig2"},
		{ID: "relay-3", Domain: "r3.example.com", PublicKey: "key3", Signature: "sig3"},
	}
	if err := cache.Save(entries, "master-key"); err != nil {
		t.Fatalf("save relays error: %v", err)
	}

	// Save latency rankings.
	rankings := []LatencyResult{
		{Relay: entries[0], Latency: 42 * time.Millisecond, Reachable: true},
		{Relay: entries[1], Latency: 78 * time.Millisecond, Reachable: true},
		{Relay: entries[2], Latency: 120 * time.Millisecond, Reachable: true},
	}
	if err := cache.SaveLatencies(rankings); err != nil {
		t.Fatalf("save latencies error: %v", err)
	}

	// Load latencies back.
	loaded, err := cache.LoadLatencies()
	if err != nil {
		t.Fatalf("load latencies error: %v", err)
	}
	if len(loaded) != 3 {
		t.Fatalf("loaded latencies: got %d, want 3", len(loaded))
	}
	if loaded[0].RelayID != "relay-1" {
		t.Errorf("first relay ID: got %q, want relay-1", loaded[0].RelayID)
	}
	if loaded[0].Latency != "42ms" {
		t.Errorf("first latency: got %q, want 42ms", loaded[0].Latency)
	}
	if loaded[1].RelayID != "relay-2" {
		t.Errorf("second relay ID: got %q, want relay-2", loaded[1].RelayID)
	}

	// Verify relays section is preserved.
	relays, masterKey, err := cache.Load()
	if err != nil {
		t.Fatalf("load relays error: %v", err)
	}
	if masterKey != "master-key" {
		t.Errorf("master key lost after latency save: got %q", masterKey)
	}
	if len(relays) != 3 {
		t.Errorf("relays lost after latency save: got %d, want 3", len(relays))
	}
}

func TestCache_LoadLatencies_NotFound(t *testing.T) {
	cachePath := filepath.Join(t.TempDir(), "nonexistent.toml")
	cache := NewCache(cachePath)

	latencies, err := cache.LoadLatencies()
	if err != nil {
		t.Errorf("expected no error for nonexistent cache, got %v", err)
	}
	if len(latencies) != 0 {
		t.Errorf("expected empty latencies, got %d", len(latencies))
	}
}
