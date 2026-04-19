//go:build windows

package browser

import (
	"context"
	"os"
	"testing"
)

func TestDetectBrowsers_NoError(t *testing.T) {
	// DetectBrowsers should not error even if no browsers are found.
	browsers, err := DetectBrowsers()
	if err != nil {
		t.Fatalf("DetectBrowsers: %v", err)
	}
	t.Logf("Detected %d browsers", len(browsers))
	for _, b := range browsers {
		t.Logf("  %s (family=%d, path=%s)", b.Name, b.Family, b.PolicyPath)
	}
}

func TestMatchDisplayName(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"Google Chrome", BrowserChrome},
		{"Google Chrome 120.0.6099.110", BrowserChrome},
		{"Microsoft Edge", BrowserEdge},
		{"Brave", BrowserBrave},
		{"Mozilla Firefox 121.0 (x64 en-US)", BrowserFirefox},
		{"VLC media player", ""},
		{"", ""},
	}

	for _, tt := range tests {
		got := matchDisplayName(tt.input)
		if got != tt.want {
			t.Errorf("matchDisplayName(%q) = %q, want %q", tt.input, got, tt.want)
		}
	}
}

func TestWindowsPolicyManagerApplyRestore_NoBrowsers(t *testing.T) {
	dir := t.TempDir()
	origFunc := policyStatePathOverride
	policyStatePathOverride = func() string { return dir }
	defer func() { policyStatePathOverride = origFunc }()

	mgr := NewPolicyManager()
	ctx := context.Background()

	// On a system with no browsers, Apply should return empty result.
	// Note: this test may find real browsers on dev machines.
	result, err := mgr.ApplyPolicies(ctx)
	if err != nil {
		t.Fatalf("ApplyPolicies: %v", err)
	}

	t.Logf("Applied: %v, Failed: %v", result.Applied, result.Failed)

	// Restore should be clean.
	if err := mgr.RestorePolicies(ctx); err != nil {
		t.Fatalf("RestorePolicies: %v", err)
	}

	// Persisted state should be cleaned up.
	if _, err := os.Stat(policyStateFilePath()); err == nil {
		t.Error("persisted state not cleaned up after restore")
	}
}

func TestRecoverOrphanPolicies_NoState(t *testing.T) {
	dir := t.TempDir()
	origFunc := policyStatePathOverride
	policyStatePathOverride = func() string { return dir }
	defer func() { policyStatePathOverride = origFunc }()

	// Should be a no-op when no state file exists.
	if err := RecoverOrphanPolicies(context.Background()); err != nil {
		t.Fatalf("RecoverOrphanPolicies: %v", err)
	}
}

func TestAdvisoryLock(t *testing.T) {
	dir := t.TempDir()
	origFunc := policyStatePathOverride
	policyStatePathOverride = func() string { return dir }
	defer func() { policyStatePathOverride = origFunc }()

	// Acquire first lock.
	lock1, err := acquireLock()
	if err != nil {
		t.Fatalf("first acquireLock: %v", err)
	}

	// Second acquire should fail (non-blocking).
	_, err = acquireLock()
	if err == nil {
		t.Fatal("second acquireLock should fail, got nil error")
	}

	// Release first lock.
	if err := lock1.Close(); err != nil {
		t.Fatalf("close lock1: %v", err)
	}

	// Now acquire should succeed.
	lock3, err := acquireLock()
	if err != nil {
		t.Fatalf("third acquireLock after release: %v", err)
	}
	lock3.Close()
}
