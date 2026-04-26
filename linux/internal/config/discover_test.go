//go:build linux

package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestDiscoverPath_FlagTakesPriority(t *testing.T) {
	got := DiscoverPath("/custom/path/config.toml")
	if got != "/custom/path/config.toml" {
		t.Errorf("expected flag path, got %q", got)
	}
}

func TestDiscoverPath_ExeDirConfig(t *testing.T) {
	tmpDir := t.TempDir()
	configFile := filepath.Join(tmpDir, "config.toml")
	if err := os.WriteFile(configFile, []byte("[relay]\ndomain = \"test.dev\"\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	origFn := ExeDir
	t.Cleanup(func() { ExeDir = origFn })
	ExeDir = func() string { return tmpDir }

	got := DiscoverPath("")
	if got != configFile {
		t.Errorf("expected exe dir config %q, got %q", configFile, got)
	}
}

func TestDiscoverPath_FallbackToDefaultPath(t *testing.T) {
	tmpDir := t.TempDir()

	origFn := ExeDir
	t.Cleanup(func() { ExeDir = origFn })
	ExeDir = func() string { return tmpDir }

	got := DiscoverPath("")
	if got == "" {
		t.Error("expected non-empty fallback path")
	}
	if got == filepath.Join(tmpDir, "config.toml") {
		t.Error("should not return exe dir path when config.toml doesn't exist there")
	}
}

func TestDiscoverPortablePath_ExeDirConfig(t *testing.T) {
	tmpDir := t.TempDir()
	configFile := filepath.Join(tmpDir, "config.toml")
	if err := os.WriteFile(configFile, []byte("[relay]\ndomain = \"test.dev\"\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	origFn := ExeDir
	t.Cleanup(func() { ExeDir = origFn })
	ExeDir = func() string { return tmpDir }

	got := DiscoverPortablePath()
	if got != configFile {
		t.Errorf("expected exe dir config %q, got %q", configFile, got)
	}
}

func TestDiscoverPortablePath_NoConfig_ReturnsEmpty(t *testing.T) {
	tmpDir := t.TempDir()

	origFn := ExeDir
	t.Cleanup(func() { ExeDir = origFn })
	ExeDir = func() string { return tmpDir }

	got := DiscoverPortablePath()
	if got != "" {
		t.Errorf("expected empty string when no config.toml exists, got %q", got)
	}
}

func TestDiscoverPortablePath_EmptyExeDir_ReturnsEmpty(t *testing.T) {
	origFn := ExeDir
	t.Cleanup(func() { ExeDir = origFn })
	ExeDir = func() string { return "" }

	got := DiscoverPortablePath()
	if got != "" {
		t.Errorf("expected empty string when ExeDir is empty, got %q", got)
	}
}
