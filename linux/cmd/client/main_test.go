//go:build linux

package main

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/velia-the-veil/le_voile/linux/internal/config"
)

func TestDiscoverConfigPath_FlagTakesPriority(t *testing.T) {
	got := config.DiscoverPath("/custom/path/config.toml")
	if got != "/custom/path/config.toml" {
		t.Errorf("expected flag path, got %q", got)
	}
}

func TestDiscoverConfigPath_ExeDirConfig(t *testing.T) {
	tmpDir := t.TempDir()
	configFile := filepath.Join(tmpDir, "config.toml")
	if err := os.WriteFile(configFile, []byte("[relay]\ndomain = \"test.dev\"\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	origFn := config.ExeDir
	t.Cleanup(func() { config.ExeDir = origFn })
	config.ExeDir = func() string { return tmpDir }

	got := config.DiscoverPath("")
	if got != configFile {
		t.Errorf("expected exe dir config %q, got %q", configFile, got)
	}
}

func TestDiscoverConfigPath_FallbackToDefaultPath(t *testing.T) {
	tmpDir := t.TempDir()

	origFn := config.ExeDir
	t.Cleanup(func() { config.ExeDir = origFn })
	config.ExeDir = func() string { return tmpDir }

	got := config.DiscoverPath("")
	if got == "" {
		t.Error("expected non-empty fallback path")
	}
	if got == filepath.Join(tmpDir, "config.toml") {
		t.Error("should not return exe dir path when config.toml doesn't exist there")
	}
}

func TestResolveConfig_FlagOverridesFile(t *testing.T) {
	tmpDir := t.TempDir()
	configFile := filepath.Join(tmpDir, "config.toml")
	if err := os.WriteFile(configFile, []byte("[relay]\ndomain = \"file.dev\"\npublic_key_ed25519 = \"filekey\"\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	rc, err := resolveConfig(configFile, "flag.dev", "flagkey", false)
	if err != nil {
		t.Fatal(err)
	}
	if rc.relayDomain != "flag.dev" {
		t.Errorf("expected flag domain, got %q", rc.relayDomain)
	}
	if rc.relayPubKey != "flagkey" {
		t.Errorf("expected flag pubkey, got %q", rc.relayPubKey)
	}
	if rc.insecure {
		t.Error("expected insecure=false")
	}
}

func TestResolveConfig_FileValues(t *testing.T) {
	tmpDir := t.TempDir()
	configFile := filepath.Join(tmpDir, "config.toml")
	if err := os.WriteFile(configFile, []byte("[relay]\ndomain = \"file.dev\"\npublic_key_ed25519 = \"filekey\"\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	rc, err := resolveConfig(configFile, "", "", false)
	if err != nil {
		t.Fatal(err)
	}
	if rc.relayDomain != "file.dev" {
		t.Errorf("expected file domain, got %q", rc.relayDomain)
	}
	if rc.relayPubKey != "filekey" {
		t.Errorf("expected file pubkey, got %q", rc.relayPubKey)
	}
}

func TestResolveConfig_EmptyPubKeyReturnsError(t *testing.T) {
	tmpDir := t.TempDir()
	configFile := filepath.Join(tmpDir, "config.toml")
	if err := os.WriteFile(configFile, []byte("[relay]\ndomain = \"test.dev\"\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	_, err := resolveConfig(configFile, "", "", false)
	if err == nil {
		t.Error("expected error for empty pubkey")
	}
}

func TestResolveConfig_DefaultDomain(t *testing.T) {
	tmpDir := t.TempDir()
	configFile := filepath.Join(tmpDir, "config.toml")
	if err := os.WriteFile(configFile, []byte("[relay]\npublic_key_ed25519 = \"testkey\"\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	rc, err := resolveConfig(configFile, "", "", false)
	if err != nil {
		t.Fatal(err)
	}
	if rc.relayDomain != defaultRelayDomain {
		t.Errorf("expected default domain %q, got %q", defaultRelayDomain, rc.relayDomain)
	}
}

func TestResolveConfig_InsecureFlagOverridesFile(t *testing.T) {
	tmpDir := t.TempDir()
	configFile := filepath.Join(tmpDir, "config.toml")
	if err := os.WriteFile(configFile, []byte("[relay]\ndomain = \"test.dev\"\npublic_key_ed25519 = \"testkey\"\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	rc, err := resolveConfig(configFile, "", "", true)
	if err != nil {
		t.Fatal(err)
	}
	if !rc.insecure {
		t.Error("expected insecure=true when flag is set")
	}
}

func TestResolveConfig_InsecureFromFile(t *testing.T) {
	tmpDir := t.TempDir()
	configFile := filepath.Join(tmpDir, "config.toml")
	if err := os.WriteFile(configFile, []byte("[relay]\ndomain = \"test.dev\"\npublic_key_ed25519 = \"testkey\"\ninsecure = true\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	rc, err := resolveConfig(configFile, "", "", false)
	if err != nil {
		t.Fatal(err)
	}
	if !rc.insecure {
		t.Error("expected insecure=true from config file")
	}
}

func TestResolveConfig_InvalidConfigPath(t *testing.T) {
	_, err := resolveConfig("/nonexistent/config.toml", "", "", false)
	if err == nil {
		t.Error("expected error for invalid config path")
	}
}

func TestNewServiceConfig(t *testing.T) {
	cfg := newServiceConfig()
	if cfg.Name == "" {
		t.Error("expected non-empty service name")
	}
	if cfg.DisplayName != "Le Voile" {
		t.Errorf("expected display name 'Le Voile', got %q", cfg.DisplayName)
	}
	if cfg.Description == "" {
		t.Error("expected non-empty description")
	}
}

