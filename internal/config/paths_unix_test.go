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
	if os.Geteuid() != 0 {
		t.Skip("service-mode path only resolves to /var/lib when euid==0; skipping (run as root to cover)")
	}
	t.Setenv("LEVOILE_STAGING_DIR", "")

	dir, err := StagingDir()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if dir != "/var/lib/levoile/updates" {
		t.Errorf("expected service-mode path /var/lib/levoile/updates, got %q", dir)
	}
}

func TestIsServiceMode_FollowsEUID(t *testing.T) {
	got := isServiceMode()
	want := os.Geteuid() == 0
	if got != want {
		t.Errorf("isServiceMode() = %v, want %v (euid=%d)", got, want, os.Geteuid())
	}
}
