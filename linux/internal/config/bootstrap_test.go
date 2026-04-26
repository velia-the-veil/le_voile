//go:build linux

package config

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

// TestBootstrap_FirstRun verifies Story 7.5 AC3: on first invocation when no
// config exists, Bootstrap writes the embedded skeleton atomically, tightens
// perms to 0600 (Unix), and creates parent dirs as needed.
func TestBootstrap_FirstRun(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "nested", "levoile", "config.toml")

	if err := Bootstrap(path); err != nil {
		t.Fatalf("Bootstrap: %v", err)
	}

	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("config not persisted: %v", err)
	}
	if info.Size() == 0 {
		t.Fatal("Bootstrap wrote an empty config")
	}

	// Content must match the embedded skeleton exactly.
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != string(exampleTOML) {
		t.Fatal("Bootstrap content diverges from embedded skeleton")
	}

	// Perms: 0600 on Unix, DACL on Windows — we only check the mode on Unix
	// because Windows mode bits don't reflect ACL state.
	if runtime.GOOS != "windows" && info.Mode().Perm() != 0o600 {
		t.Errorf("file mode = %v, want 0600", info.Mode().Perm())
	}

	// Bootstrap must also load cleanly — it's no good if it produces an
	// invalid skeleton that Load rejects.
	if _, err := Load(path); err != nil {
		t.Fatalf("Load after Bootstrap: %v", err)
	}
}

// TestBootstrap_Idempotent verifies a second Bootstrap call on an existing
// config is a no-op: the file must be preserved byte-for-byte, with no
// re-write (so a running service's HMAC stays valid).
func TestBootstrap_Idempotent(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.toml")

	// Seed a custom config that is NOT the skeleton — simulates a user who
	// already filled their values.
	custom := []byte("[relay]\ndomain = \"custom.example\"\npublic_key_ed25519 = \"abc\"\n")
	if err := os.WriteFile(path, custom, 0o600); err != nil {
		t.Fatal(err)
	}
	info1, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}

	if err := Bootstrap(path); err != nil {
		t.Fatalf("Bootstrap (second call): %v", err)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != string(custom) {
		t.Fatal("Bootstrap overwrote an existing config — idempotency violated")
	}

	info2, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	// mtime must not have advanced: no write happened.
	if !info1.ModTime().Equal(info2.ModTime()) {
		t.Errorf("mtime changed: %v → %v (Bootstrap re-wrote an existing file)", info1.ModTime(), info2.ModTime())
	}
}

// TestBootstrap_CreatesParentDirs covers the case where neither the file nor
// the parent dir exist yet (fresh install, /etc/levoile/ not pre-created by
// packaging on some distros).
func TestBootstrap_CreatesParentDirs(t *testing.T) {
	dir := t.TempDir()
	// Parent dir "levoile" + deeper "subdir" deliberately missing.
	path := filepath.Join(dir, "levoile", "subdir", "config.toml")

	if err := Bootstrap(path); err != nil {
		t.Fatalf("Bootstrap: %v", err)
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("config not persisted under newly-created dirs: %v", err)
	}
}

// TestBootstrap_ReturnsErrorOnUnwritablePath verifies failure mode: if the
// target path is unreachable (impossible parent), Bootstrap errors out
// rather than silently swallowing — caller (cmd/client main) must be able
// to distinguish success from failure to decide whether to exit.
func TestBootstrap_ReturnsErrorOnUnwritablePath(t *testing.T) {
	// Construct a path under a regular file (can't mkdir through a file).
	dir := t.TempDir()
	blocker := filepath.Join(dir, "blocker")
	if err := os.WriteFile(blocker, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	// blocker is a file; try to create config under it → mkdir fails.
	path := filepath.Join(blocker, "config.toml")

	if err := Bootstrap(path); err == nil {
		t.Fatal("Bootstrap returned nil on unwritable path, want error")
	}
}
