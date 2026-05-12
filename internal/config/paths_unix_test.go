//go:build linux || darwin

package config

import (
	"os"
	"runtime"
	"strings"
	"testing"
)

func TestStagingDir_EnvOverride(t *testing.T) {
	t.Setenv("LEVOILE_STAGING_DIR", "/tmp/custom-staging")

	dir, err := StagingDir()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if dir != "/tmp/custom-staging" {
		t.Errorf("expected override path, got %q", dir)
	}
}

func TestStagingDir_UserMode(t *testing.T) {
	if runtime.GOOS == "linux" && os.Geteuid() == 0 {
		t.Skip("cannot test user-mode path from root; skipping")
	}
	t.Setenv("LEVOILE_STAGING_DIR", "")
	// Clear systemd env so the M3 detection path doesn't flip us to service mode.
	t.Setenv("INVOCATION_ID", "")
	t.Setenv("NOTIFY_SOCKET", "")

	dir, err := StagingDir()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(dir, "levoile") {
		t.Errorf("expected path to contain 'levoile', got %q", dir)
	}
	if !strings.HasSuffix(dir, "updates") {
		t.Errorf("expected path to end with 'updates', got %q", dir)
	}
	if strings.HasPrefix(dir, "/var/lib/") {
		t.Errorf("user-mode path should not be in /var/lib, got %q", dir)
	}
}

func TestStagingDir_ServiceMode_SystemPath(t *testing.T) {
	// Service mode now triggers on euid==0 OR systemd env vars (M3).
	// Exercise the path regardless of the invoking user by forcing the env
	// signal, then assert the resolved path.
	t.Setenv("LEVOILE_STAGING_DIR", "")
	t.Setenv("INVOCATION_ID", "deadbeef1234deadbeef1234deadbeef")
	t.Setenv("NOTIFY_SOCKET", "")

	dir, err := StagingDir()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if dir != "/var/lib/levoile/updates" {
		t.Errorf("expected service-mode path /var/lib/levoile/updates, got %q", dir)
	}
}

func TestIsServiceMode_FollowsEUID(t *testing.T) {
	// Clear systemd env vars so we isolate the euid signal.
	t.Setenv("INVOCATION_ID", "")
	t.Setenv("NOTIFY_SOCKET", "")
	got := isServiceMode()
	want := os.Geteuid() == 0
	if got != want {
		t.Errorf("isServiceMode() = %v, want %v (euid=%d)", got, want, os.Geteuid())
	}
}

// TestIsServiceMode_SystemdEnvTriggersEvenWhenNonRoot covers the
// `User=levoile` systemd unit scenario: euid is non-zero but systemd
// sets INVOCATION_ID (and NOTIFY_SOCKET if Type=notify) for every unit
// it launches. Story 8.2 M3 hardening.
func TestIsServiceMode_SystemdEnvTriggersEvenWhenNonRoot(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("only meaningful under non-root euid")
	}

	// INVOCATION_ID alone is sufficient.
	t.Setenv("NOTIFY_SOCKET", "")
	t.Setenv("INVOCATION_ID", "deadbeef1234deadbeef1234deadbeef")
	if !isServiceMode() {
		t.Error("INVOCATION_ID set but isServiceMode() returned false")
	}

	// NOTIFY_SOCKET alone is sufficient.
	t.Setenv("INVOCATION_ID", "")
	t.Setenv("NOTIFY_SOCKET", "/run/systemd/notify")
	if !isServiceMode() {
		t.Error("NOTIFY_SOCKET set but isServiceMode() returned false")
	}

	// Neither → user mode.
	t.Setenv("INVOCATION_ID", "")
	t.Setenv("NOTIFY_SOCKET", "")
	if isServiceMode() {
		t.Error("no systemd env and non-root euid: isServiceMode() should be false")
	}
}
