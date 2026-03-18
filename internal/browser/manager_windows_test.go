//go:build windows

package browser

import (
	"context"
	"encoding/json"
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

// --- Extension policy tests ---

func TestMergeExtensionSettings_Empty(t *testing.T) {
	merged, err := mergeExtensionSettings("", false, "testextid", map[string]string{
		"installation_mode": "force_installed",
		"update_url":        "file:///test/updates.xml",
	})
	if err != nil {
		t.Fatalf("merge: %v", err)
	}

	var result map[string]interface{}
	if err := json.Unmarshal([]byte(merged), &result); err != nil {
		t.Fatalf("unmarshal result: %v", err)
	}

	entry, ok := result["testextid"].(map[string]interface{})
	if !ok {
		t.Fatal("expected testextid entry in merged result")
	}
	if entry["installation_mode"] != "force_installed" {
		t.Errorf("expected force_installed, got %v", entry["installation_mode"])
	}
}

func TestMergeExtensionSettings_PreExistingValid(t *testing.T) {
	existing := `{"other_ext_id":{"installation_mode":"normal_installed","update_url":"https://example.com"}}`

	merged, err := mergeExtensionSettings(existing, true, "levoile_id", map[string]string{
		"installation_mode": "force_installed",
		"update_url":        "file:///local/updates.xml",
	})
	if err != nil {
		t.Fatalf("merge: %v", err)
	}

	var result map[string]interface{}
	if err := json.Unmarshal([]byte(merged), &result); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	// Verify original entry preserved.
	if _, ok := result["other_ext_id"]; !ok {
		t.Error("existing extension entry was lost during merge")
	}

	// Verify Le Voile entry added.
	entry, ok := result["levoile_id"].(map[string]interface{})
	if !ok {
		t.Fatal("expected levoile_id entry")
	}
	if entry["installation_mode"] != "force_installed" {
		t.Errorf("expected force_installed, got %v", entry["installation_mode"])
	}
}

func TestMergeExtensionSettings_PreExistingInvalidJSON(t *testing.T) {
	// E4 guard: invalid JSON should return error, not corrupt existing value.
	_, err := mergeExtensionSettings("{invalid json!!", true, "testid", map[string]string{
		"installation_mode": "force_installed",
	})
	if err == nil {
		t.Fatal("expected error for invalid JSON, got nil")
	}
}

func TestMergeExtensionSettings_PreExistingCorruptedJSON(t *testing.T) {
	// E4 guard: truncated JSON.
	_, err := mergeExtensionSettings(`{"key": "val`, true, "testid", map[string]string{
		"installation_mode": "force_installed",
	})
	if err == nil {
		t.Fatal("expected error for corrupted JSON, got nil")
	}
}

func TestExtensionStateInPersistedState(t *testing.T) {
	dir := t.TempDir()
	origFunc := policyStatePathOverride
	policyStatePathOverride = func() string { return dir }
	defer func() { policyStatePathOverride = origFunc }()

	state := &policyPersistedState{
		Browsers: []browserSavedState{
			{
				Name:             BrowserChrome,
				Family:           Chromium,
				PolicyPath:       `SOFTWARE\Policies\Google\Chrome`,
				ExtPolicyPath:    `SOFTWARE\Policies\Google\Chrome`,
				ExtOriginalValue: `{"existing":"value"}`,
				ExtHadOriginal:   true,
			},
		},
		Extension: &extensionDeployState{
			DeployDir: dir + `\extension`,
		},
	}

	if err := persistStateIncremental(state); err != nil {
		t.Fatalf("persist: %v", err)
	}

	loaded, err := loadPersistedState()
	if err != nil {
		t.Fatalf("load: %v", err)
	}

	if len(loaded.Browsers) != 1 {
		t.Fatalf("expected 1 browser, got %d", len(loaded.Browsers))
	}

	b := loaded.Browsers[0]
	if b.ExtPolicyPath != `SOFTWARE\Policies\Google\Chrome` {
		t.Errorf("ExtPolicyPath: got %s", b.ExtPolicyPath)
	}
	if b.ExtOriginalValue != `{"existing":"value"}` {
		t.Errorf("ExtOriginalValue: got %s", b.ExtOriginalValue)
	}
	if !b.ExtHadOriginal {
		t.Error("ExtHadOriginal should be true")
	}
	if loaded.Extension == nil {
		t.Fatal("Extension deploy state should not be nil")
	}
	if loaded.Extension.DeployDir != dir+`\extension` {
		t.Errorf("DeployDir: got %s", loaded.Extension.DeployDir)
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
