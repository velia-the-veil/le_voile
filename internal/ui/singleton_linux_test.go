//go:build linux

package ui

import (
	"path/filepath"
	"strings"
	"testing"
)

// setLockOverride redirects the singleton lock to a test-local path and
// guarantees cleanup of the lock + override variable.
func setLockOverride(t *testing.T) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "ui.lock")
	lockPathOverride = path
	t.Cleanup(func() {
		ReleaseSingleton()
		lockPathOverride = ""
	})
	return path
}

func TestAcquireSingleton_FirstInstance(t *testing.T) {
	setLockOverride(t)
	if err := AcquireSingleton(); err != nil {
		t.Fatalf("first acquire: %v", err)
	}
	if lockFile == nil {
		t.Fatal("expected lockFile to be set after acquire")
	}
}

func TestAcquireSingleton_SecondInstanceBlocked(t *testing.T) {
	setLockOverride(t)

	if err := AcquireSingleton(); err != nil {
		t.Fatalf("first acquire: %v", err)
	}
	err := AcquireSingleton()
	if err == nil {
		t.Fatal("second acquire should fail while first lock is held")
	}
	if !strings.Contains(err.Error(), "une autre instance") {
		t.Errorf("err = %q, want French user-facing message", err.Error())
	}
}

func TestReleaseSingleton_IdempotentWithoutAcquire(t *testing.T) {
	setLockOverride(t)
	// Should not panic even when no lock is held.
	ReleaseSingleton()
	ReleaseSingleton()
}

func TestReleaseSingleton_IdempotentAfterAcquire(t *testing.T) {
	setLockOverride(t)
	if err := AcquireSingleton(); err != nil {
		t.Fatalf("acquire: %v", err)
	}
	ReleaseSingleton()
	// Second release must be safe.
	ReleaseSingleton()
	if lockFile != nil {
		t.Fatal("expected lockFile to be nil after release")
	}
}

func TestAcquireSingleton_ReacquireAfterRelease(t *testing.T) {
	setLockOverride(t)

	if err := AcquireSingleton(); err != nil {
		t.Fatalf("first: %v", err)
	}
	ReleaseSingleton()
	if err := AcquireSingleton(); err != nil {
		t.Fatalf("second after release: %v", err)
	}
}

func TestResolveLockPath_XDGStateHome(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", "/tmp/xdg-state")
	lockPathOverride = "" // ensure default path logic runs
	t.Cleanup(func() { lockPathOverride = "" })

	got, err := resolveLockPath()
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	want := "/tmp/xdg-state/levoile/ui.lock"
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestResolveLockPath_HomeFallback(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", "")
	t.Setenv("HOME", "/home/testuser")
	lockPathOverride = ""
	t.Cleanup(func() { lockPathOverride = "" })

	got, err := resolveLockPath()
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	want := "/home/testuser/.local/state/levoile/ui.lock"
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}
