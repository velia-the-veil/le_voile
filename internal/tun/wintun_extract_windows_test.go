//go:build windows

package tun

import (
	"os"
	"path/filepath"
	"testing"
)

// withTestSeams stubs exeDir, loadDLL, and resets extractOnce so each test
// can drive doEnsureWintunDLL through a fresh resolution.
func withTestSeams(t *testing.T, exe string, load func(string) error) (loaded *string) {
	t.Helper()
	origExe, origLoad, origEmbed := exeDir, loadDLL, embeddedWintunDLL
	t.Cleanup(func() {
		exeDir = origExe
		loadDLL = origLoad
		embeddedWintunDLL = origEmbed
	})
	exeDir = func() string { return exe }
	var captured string
	loaded = &captured
	loadDLL = func(p string) error {
		captured = p
		if load != nil {
			return load(p)
		}
		return nil
	}
	return loaded
}

// TestEnsureWintunDLL_ExeDirPriority — Story 7.1 Task 3.5/3.6:
// when wintun.dll exists alongside the executable (NSIS install path),
// the resolver MUST load it directly without writing to %ProgramData%.
func TestEnsureWintunDLL_ExeDirPriority(t *testing.T) {
	// Isolate PROGRAMDATA so the no-write assertion below cannot false-
	// positive on a developer machine where %PROGRAMDATA%\LeVoile already
	// holds a real wintun.dll cache from a prior service run.
	t.Setenv("PROGRAMDATA", t.TempDir())

	exeDir := t.TempDir()
	dllPath := filepath.Join(exeDir, "wintun.dll")
	if err := os.WriteFile(dllPath, []byte("FAKE_DLL_FOR_TEST"), 0o644); err != nil {
		t.Fatal(err)
	}

	loaded := withTestSeams(t, exeDir, nil)
	embeddedWintunDLL = []byte("EMBED_SHOULD_NOT_BE_USED")

	if err := doEnsureWintunDLL(); err != nil {
		t.Fatalf("expected success, got %v", err)
	}
	if *loaded != dllPath {
		t.Errorf("expected loadDLL(%q), got %q", dllPath, *loaded)
	}

	// Cache must NOT have been written when exe-dir resolution succeeds —
	// the entire LeVoile subdir should be absent from the redirected
	// PROGRAMDATA tempdir.
	cacheDir := filepath.Join(os.Getenv("PROGRAMDATA"), "LeVoile")
	if _, err := os.Stat(cacheDir); err == nil {
		t.Error("expected no %ProgramData%\\LeVoile dir when exe-dir DLL found")
	}
}

// TestEnsureWintunDLL_FallbackToCacheWhenEmbedEmpty — when no DLL is next
// to the exe AND the embed is empty, the cached %ProgramData% file is the
// last resort. This covers the "dev build without `make wintun`" path
// where another component (NSIS install or prior run) populated the cache.
func TestEnsureWintunDLL_FallbackToCacheWhenEmbedEmpty(t *testing.T) {
	t.Setenv("PROGRAMDATA", t.TempDir())
	cacheDir := filepath.Join(os.Getenv("PROGRAMDATA"), "LeVoile")
	if err := os.MkdirAll(cacheDir, 0o755); err != nil {
		t.Fatal(err)
	}
	cache := filepath.Join(cacheDir, "wintun.dll")
	if err := os.WriteFile(cache, []byte("CACHED"), 0o644); err != nil {
		t.Fatal(err)
	}

	loaded := withTestSeams(t, t.TempDir() /* empty exe dir */, nil)
	embeddedWintunDLL = nil // simulate dev build

	if err := doEnsureWintunDLL(); err != nil {
		t.Fatalf("expected success via cache, got %v", err)
	}
	if *loaded != cache {
		t.Errorf("expected loadDLL(%q), got %q", cache, *loaded)
	}
}

// TestEnsureWintunDLL_HardFailWhenNoSourceAvailable — embed empty AND no
// cache AND no exe-dir DLL → return a clear error. Documents the boundary
// where a dev forgot to run `make wintun` and there's no installed DLL.
func TestEnsureWintunDLL_HardFailWhenNoSourceAvailable(t *testing.T) {
	t.Setenv("PROGRAMDATA", t.TempDir())

	loaded := withTestSeams(t, t.TempDir(), nil)
	embeddedWintunDLL = nil

	err := doEnsureWintunDLL()
	if err == nil {
		t.Fatal("expected error when no DLL source available")
	}
	if *loaded != "" {
		t.Errorf("loadDLL must NOT be called when no source available, got %q", *loaded)
	}
}

// TestEnsureWintunDLL_EmbedExtractionWhenNoExeDirDLL — when embed is non-
// empty and no DLL exists alongside the exe, the resolver extracts to
// %ProgramData% and loads from there. Preserves the legacy behavior.
func TestEnsureWintunDLL_EmbedExtractionWhenNoExeDirDLL(t *testing.T) {
	t.Setenv("PROGRAMDATA", t.TempDir())
	cache := filepath.Join(os.Getenv("PROGRAMDATA"), "LeVoile", "wintun.dll")

	loaded := withTestSeams(t, t.TempDir(), nil)
	embeddedWintunDLL = []byte("EMBED_PAYLOAD")

	if err := doEnsureWintunDLL(); err != nil {
		t.Fatalf("expected success via embed extraction, got %v", err)
	}
	if *loaded != cache {
		t.Errorf("expected loadDLL(%q), got %q", cache, *loaded)
	}
	got, err := os.ReadFile(cache)
	if err != nil {
		t.Fatalf("expected cache written, got %v", err)
	}
	if string(got) != "EMBED_PAYLOAD" {
		t.Errorf("cache contents %q != embed payload", string(got))
	}
}
