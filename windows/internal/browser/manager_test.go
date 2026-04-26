//go:build windows

package browser

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestPersistStateIncremental(t *testing.T) {
	// Override policyStatePath for testing.
	dir := t.TempDir()
	origFunc := policyStatePathOverride
	policyStatePathOverride = func() string { return dir }
	defer func() { policyStatePathOverride = origFunc }()

	state := &policyPersistedState{
		Browsers: []browserSavedState{
			{
				Name:          BrowserChrome,
				Family:        Chromium,
				PolicyPath:    `SOFTWARE\Policies\Google\Chrome`,
				OriginalValue: "",
				HadOriginal:   false,
			},
		},
	}

	if err := persistStateIncremental(state); err != nil {
		t.Fatalf("persistStateIncremental: %v", err)
	}

	// Verify file exists and is valid JSON.
	data, err := os.ReadFile(filepath.Join(dir, policyStateFile))
	if err != nil {
		t.Fatalf("read persisted file: %v", err)
	}

	var loaded policyPersistedState
	if err := json.Unmarshal(data, &loaded); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	if len(loaded.Browsers) != 1 {
		t.Fatalf("expected 1 browser, got %d", len(loaded.Browsers))
	}
	if loaded.Browsers[0].Name != BrowserChrome {
		t.Errorf("expected Chrome, got %s", loaded.Browsers[0].Name)
	}
}

func TestPersistStateIncrementalMultiple(t *testing.T) {
	dir := t.TempDir()
	origFunc := policyStatePathOverride
	policyStatePathOverride = func() string { return dir }
	defer func() { policyStatePathOverride = origFunc }()

	state := &policyPersistedState{}

	// Add Chrome.
	state.Browsers = append(state.Browsers, browserSavedState{
		Name:   BrowserChrome,
		Family: Chromium,
	})
	if err := persistStateIncremental(state); err != nil {
		t.Fatalf("persist Chrome: %v", err)
	}

	// Add Firefox.
	state.Browsers = append(state.Browsers, browserSavedState{
		Name:   BrowserFirefox,
		Family: Firefox,
	})
	if err := persistStateIncremental(state); err != nil {
		t.Fatalf("persist Firefox: %v", err)
	}

	// Load and verify both.
	loaded, err := loadPersistedState()
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if len(loaded.Browsers) != 2 {
		t.Fatalf("expected 2 browsers, got %d", len(loaded.Browsers))
	}
}

func TestLoadPersistedStateNotFound(t *testing.T) {
	dir := t.TempDir()
	origFunc := policyStatePathOverride
	policyStatePathOverride = func() string { return dir }
	defer func() { policyStatePathOverride = origFunc }()

	_, err := loadPersistedState()
	if err == nil {
		t.Fatal("expected error for missing state, got nil")
	}
}

func TestCleanOrphanTemps_PromoteTemp(t *testing.T) {
	dir := t.TempDir()
	origFunc := policyStatePathOverride
	policyStatePathOverride = func() string { return dir }
	defer func() { policyStatePathOverride = origFunc }()

	// Create an orphan temp file.
	state := &policyPersistedState{
		Browsers: []browserSavedState{{Name: BrowserEdge, Family: Chromium}},
	}
	data, _ := json.Marshal(state)
	tmpPath := filepath.Join(dir, "browser-policies-123456.tmp")
	os.WriteFile(tmpPath, data, 0644)

	cleanOrphanTemps()

	// Temp should be promoted to final.
	loaded, err := loadPersistedState()
	if err != nil {
		t.Fatalf("load after promote: %v", err)
	}
	if len(loaded.Browsers) != 1 || loaded.Browsers[0].Name != BrowserEdge {
		t.Errorf("unexpected state after promote: %+v", loaded)
	}

	// Temp file should be gone.
	if _, err := os.Stat(tmpPath); err == nil {
		t.Error("orphan temp file was not cleaned up")
	}
}

func TestCleanOrphanTemps_RemoveWhenFinalExists(t *testing.T) {
	dir := t.TempDir()
	origFunc := policyStatePathOverride
	policyStatePathOverride = func() string { return dir }
	defer func() { policyStatePathOverride = origFunc }()

	// Create both final and temp.
	state := &policyPersistedState{
		Browsers: []browserSavedState{{Name: BrowserChrome}},
	}
	data, _ := json.Marshal(state)
	os.WriteFile(filepath.Join(dir, policyStateFile), data, 0644)

	tmpPath := filepath.Join(dir, "browser-policies-orphan.tmp")
	os.WriteFile(tmpPath, []byte("old"), 0644)

	cleanOrphanTemps()

	// Temp should be removed.
	if _, err := os.Stat(tmpPath); err == nil {
		t.Error("orphan temp not removed when final exists")
	}
}

func TestRemovePersistedState(t *testing.T) {
	dir := t.TempDir()
	origFunc := policyStatePathOverride
	policyStatePathOverride = func() string { return dir }
	defer func() { policyStatePathOverride = origFunc }()

	// Create the file.
	state := &policyPersistedState{Browsers: []browserSavedState{{Name: BrowserChrome}}}
	persistStateIncremental(state)

	removePersistedState()

	if _, err := os.Stat(filepath.Join(dir, policyStateFile)); err == nil {
		t.Error("persisted state not removed")
	}
}

func TestApplyResultEmpty(t *testing.T) {
	r := &ApplyResult{}
	if len(r.Applied) != 0 {
		t.Errorf("expected empty Applied, got %v", r.Applied)
	}
	if len(r.Failed) != 0 {
		t.Errorf("expected empty Failed, got %v", r.Failed)
	}
}

func TestBrowserFamilyConstants(t *testing.T) {
	if Chromium == Firefox {
		t.Error("Chromium and Firefox should be different")
	}
}

func TestBrowserSavedStateRoundtrip(t *testing.T) {
	original := browserSavedState{
		Name:          BrowserFirefox,
		Family:        Firefox,
		PolicyPath:    "/usr/lib/firefox/distribution/policies.json",
		OriginalValue: `{"policies":{"DisableTelemetry":true}}`,
		HadOriginal:   true,
	}

	data, err := json.Marshal(original)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	var decoded browserSavedState
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	if decoded.Name != original.Name {
		t.Errorf("Name: got %s, want %s", decoded.Name, original.Name)
	}
	if decoded.Family != original.Family {
		t.Errorf("Family: got %d, want %d", decoded.Family, original.Family)
	}
	if decoded.OriginalValue != original.OriginalValue {
		t.Errorf("OriginalValue mismatch")
	}
	if decoded.HadOriginal != original.HadOriginal {
		t.Errorf("HadOriginal: got %v, want %v", decoded.HadOriginal, original.HadOriginal)
	}
}
