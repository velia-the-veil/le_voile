package main

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/velia-the-veil/le_voile/internal/config"
)

func TestDialIPC_ConflictDetected(t *testing.T) {
	origFn := dialIPC
	t.Cleanup(func() { dialIPC = origFn })

	// Simulate installed version running (dialIPC succeeds → conflict).
	dialIPC = func() error { return nil }
	if err := dialIPC(); err != nil {
		t.Error("expected nil error when installed version is running")
	}
}

func TestDialIPC_NoConflict(t *testing.T) {
	origFn := dialIPC
	t.Cleanup(func() { dialIPC = origFn })

	// Simulate no installed version (dialIPC fails → no conflict).
	dialIPC = func() error { return errors.New("connection refused") }
	if err := dialIPC(); err == nil {
		t.Error("expected error when no installed version running")
	}
}

func TestPortableConfigLoading_WithConfigFile(t *testing.T) {
	tmpDir := t.TempDir()
	configFile := filepath.Join(tmpDir, "config.toml")
	content := "[relay]\ndomain = \"test.dev\"\npublic_key_ed25519 = \"dGVzdA==\"\n"
	if err := os.WriteFile(configFile, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}

	origFn := config.ExeDir
	t.Cleanup(func() { config.ExeDir = origFn })
	config.ExeDir = func() string { return tmpDir }

	cfgPath := config.DiscoverPortablePath()
	if cfgPath == "" {
		t.Fatal("expected config path, got empty")
	}

	cfg, err := config.Load(cfgPath)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Relay.Domain != "test.dev" {
		t.Errorf("expected test.dev, got %q", cfg.Relay.Domain)
	}
	if cfg.Relay.PublicKeyEd25519 != "dGVzdA==" {
		t.Errorf("expected dGVzdA==, got %q", cfg.Relay.PublicKeyEd25519)
	}
}

func TestPortableConfigLoading_NoConfigFile(t *testing.T) {
	tmpDir := t.TempDir()

	origFn := config.ExeDir
	t.Cleanup(func() { config.ExeDir = origFn })
	config.ExeDir = func() string { return tmpDir }

	cfgPath := config.DiscoverPortablePath()
	if cfgPath != "" {
		t.Errorf("expected empty path when no config.toml, got %q", cfgPath)
	}
}
