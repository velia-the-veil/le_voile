package browser

import (
	"archive/zip"
	"bytes"
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

func TestExtensionDeployStatePersistence(t *testing.T) {
	dir := t.TempDir()
	origFunc := policyStatePathOverride
	policyStatePathOverride = func() string { return dir }
	defer func() { policyStatePathOverride = origFunc }()

	state := &policyPersistedState{
		Browsers: []browserSavedState{
			{
				Name:             BrowserChrome,
				Family:           Chromium,
				PolicyPath:       "/test/path",
				ExtPolicyPath:    "/test/ext/path",
				ExtOriginalValue: `{"old":"data"}`,
				ExtHadOriginal:   true,
			},
			{
				Name:             BrowserFirefox,
				Family:           Firefox,
				PolicyPath:       "/test/firefox",
				ExtPolicyPath:    "/test/firefox/ext",
				ExtOriginalValue: "",
				ExtHadOriginal:   false,
			},
		},
		Extension: &extensionDeployState{
			DeployDir: "/opt/levoile/extension",
		},
	}

	if err := persistStateIncremental(state); err != nil {
		t.Fatalf("persist: %v", err)
	}

	loaded, err := loadPersistedState()
	if err != nil {
		t.Fatalf("load: %v", err)
	}

	if len(loaded.Browsers) != 2 {
		t.Fatalf("expected 2 browsers, got %d", len(loaded.Browsers))
	}

	// Verify Chrome extension state.
	chrome := loaded.Browsers[0]
	if chrome.ExtPolicyPath != "/test/ext/path" {
		t.Errorf("Chrome ExtPolicyPath: %s", chrome.ExtPolicyPath)
	}
	if !chrome.ExtHadOriginal {
		t.Error("Chrome ExtHadOriginal should be true")
	}

	// Verify Firefox extension state.
	firefox := loaded.Browsers[1]
	if firefox.ExtHadOriginal {
		t.Error("Firefox ExtHadOriginal should be false")
	}

	// Verify extension deploy state.
	if loaded.Extension == nil {
		t.Fatal("Extension should not be nil")
	}
	if loaded.Extension.DeployDir != "/opt/levoile/extension" {
		t.Errorf("DeployDir: %s", loaded.Extension.DeployDir)
	}
}

func TestEmbeddedXPI(t *testing.T) {
	data, err := extensionFS.ReadFile("extension_assets/build/levoile.xpi")
	if err != nil {
		t.Skip("embedded XPI not available (build/levoile.xpi missing from embed)")
	}
	if len(data) == 0 {
		t.Fatal("embedded XPI is empty")
	}

	r, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		t.Fatalf("open XPI as ZIP: %v", err)
	}

	foundManifest := false
	foundBackground := false
	for _, f := range r.File {
		switch f.Name {
		case "manifest.json":
			foundManifest = true
			rc, err := f.Open()
			if err != nil {
				t.Fatalf("open manifest.json in XPI: %v", err)
			}
			var manifest map[string]interface{}
			if err := json.NewDecoder(rc).Decode(&manifest); err != nil {
				rc.Close()
				t.Fatalf("decode manifest.json: %v", err)
			}
			rc.Close()
			bss, ok := manifest["browser_specific_settings"].(map[string]interface{})
			if !ok {
				t.Error("manifest.json missing browser_specific_settings")
			} else {
				gecko, ok := bss["gecko"].(map[string]interface{})
				if !ok {
					t.Error("missing gecko section")
				} else if gecko["id"] != FirefoxGeckoID {
					t.Errorf("gecko ID: got %v, want %s", gecko["id"], FirefoxGeckoID)
				}
			}
		case "background.js":
			foundBackground = true
		}
	}

	if !foundManifest {
		t.Error("XPI missing manifest.json")
	}
	if !foundBackground {
		t.Error("XPI missing background.js")
	}
}

func TestExtractExtensionIDFromCRX(t *testing.T) {
	id, err := extractExtensionIDFromCRX()
	if err != nil {
		t.Fatalf("extractExtensionIDFromCRX: %v", err)
	}
	if len(id) != 32 {
		t.Errorf("expected 32-char extension ID, got %d chars: %s", len(id), id)
	}
	// Verify all chars are in a-p range.
	for _, c := range id {
		if c < 'a' || c > 'p' {
			t.Errorf("invalid char in extension ID: %c", c)
		}
	}
	t.Logf("Extension ID: %s", id)
}

func TestReadEmbeddedManifestVersion(t *testing.T) {
	version, err := readEmbeddedManifestVersion()
	if err != nil {
		t.Fatalf("readEmbeddedManifestVersion: %v", err)
	}
	if version == "" {
		t.Error("version should not be empty")
	}
	t.Logf("Manifest version: %s", version)
}

func TestDeployExtensionFiles(t *testing.T) {
	dir := t.TempDir()
	origFunc := policyStatePathOverride
	policyStatePathOverride = func() string { return dir }
	defer func() { policyStatePathOverride = origFunc }()

	// Ensure ExtensionID is set (should be from init()).
	if ExtensionID == "" {
		t.Skip("ExtensionID not available (CRX not embedded)")
	}

	if err := deployExtensionFiles(); err != nil {
		t.Fatalf("deployExtensionFiles: %v", err)
	}

	deployDir := extensionDeployDir()

	// Verify CRX deployed.
	crxPath := filepath.Join(deployDir, "chrome", "levoile.crx")
	if _, err := os.Stat(crxPath); err != nil {
		t.Errorf("CRX not deployed: %v", err)
	}

	// Verify updates.xml generated.
	xmlPath := filepath.Join(deployDir, "chrome", "updates.xml")
	xmlData, err := os.ReadFile(xmlPath)
	if err != nil {
		t.Fatalf("read updates.xml: %v", err)
	}
	xmlStr := string(xmlData)
	if !bytes.Contains(xmlData, []byte(ExtensionID)) {
		t.Errorf("updates.xml missing extension ID %s", ExtensionID)
	}
	if !bytes.Contains(xmlData, []byte("levoile.crx")) {
		t.Error("updates.xml missing CRX reference")
	}
	t.Logf("updates.xml:\n%s", xmlStr)

	// Verify XPI generated.
	xpiPath := filepath.Join(deployDir, "levoile.xpi")
	xpiData, err := os.ReadFile(xpiPath)
	if err != nil {
		t.Fatalf("read XPI: %v", err)
	}
	// Verify it's a valid ZIP with manifest.json.
	r, err := zip.NewReader(bytes.NewReader(xpiData), int64(len(xpiData)))
	if err != nil {
		t.Fatalf("XPI is not valid ZIP: %v", err)
	}
	found := false
	for _, f := range r.File {
		if f.Name == "manifest.json" {
			found = true
		}
	}
	if !found {
		t.Error("XPI missing manifest.json")
	}

	// Test cleanup.
	os.RemoveAll(deployDir)
	if _, err := os.Stat(deployDir); err == nil {
		t.Error("deploy dir not cleaned up")
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
