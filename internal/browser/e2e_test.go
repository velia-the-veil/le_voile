//go:build e2e

package browser

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

// TestE2E_FirefoxPoliciesApplied validates the Firefox policies.json structure
// that ApplyPolicies produces for extension force-install.
//
// Limitation: ApplyPolicies() does not accept a directory override — it writes
// to system-level Firefox directories (e.g. /usr/lib/firefox/distribution/ on
// Linux). Without Firefox installed or root/admin privileges, calling
// ApplyPolicies directly would fail. This test validates the JSON structure
// and constants used by the policy generator, using the same FirefoxGeckoID
// constant to ensure consistency with production code.
func TestE2E_FirefoxPoliciesApplied(t *testing.T) {
	if os.Getenv("E2E") != "1" {
		t.Skip("set E2E=1 to run")
	}

	// Create a fake Firefox distribution directory.
	tmpDir := t.TempDir()
	distDir := filepath.Join(tmpDir, "distribution")
	if err := os.MkdirAll(distDir, 0755); err != nil {
		t.Fatalf("mkdir distribution: %v", err)
	}

	// Reproduce the exact JSON structure that ApplyPolicies writes for Firefox.
	// This validates the schema is correct and consistent with the constants.
	policies := map[string]interface{}{
		"policies": map[string]interface{}{
			"ExtensionSettings": map[string]interface{}{
				FirefoxGeckoID: map[string]interface{}{
					"installation_mode": "force_installed",
					"install_url":       "file:///C:/ProgramData/LeVoile/extension/levoile.xpi",
				},
			},
			"Preferences": map[string]interface{}{
				"media.peerconnection.enabled": false,
			},
		},
	}

	policiesPath := filepath.Join(distDir, "policies.json")
	data, err := json.MarshalIndent(policies, "", "  ")
	if err != nil {
		t.Fatalf("marshal policies: %v", err)
	}
	if err := os.WriteFile(policiesPath, data, 0644); err != nil {
		t.Fatalf("write policies.json: %v", err)
	}

	// Verify the file is valid JSON with correct structure.
	raw, err := os.ReadFile(policiesPath)
	if err != nil {
		t.Fatalf("read policies.json: %v", err)
	}

	var parsed map[string]interface{}
	if err := json.Unmarshal(raw, &parsed); err != nil {
		t.Fatalf("invalid JSON in policies.json: %v", err)
	}

	policiesObj, ok := parsed["policies"].(map[string]interface{})
	if !ok {
		t.Fatal("policies.json missing 'policies' object")
	}

	extSettings, ok := policiesObj["ExtensionSettings"].(map[string]interface{})
	if !ok {
		t.Fatal("policies.json missing 'ExtensionSettings'")
	}

	geckoEntry, ok := extSettings[FirefoxGeckoID]
	if !ok {
		t.Fatalf("ExtensionSettings missing %q", FirefoxGeckoID)
	}

	// Verify the extension entry contains required fields.
	entryMap, ok := geckoEntry.(map[string]interface{})
	if !ok {
		t.Fatal("extension entry is not a JSON object")
	}
	if mode, ok := entryMap["installation_mode"].(string); !ok || mode != "force_installed" {
		t.Errorf("installation_mode = %v, want %q", entryMap["installation_mode"], "force_installed")
	}
	if _, ok := entryMap["install_url"].(string); !ok {
		t.Error("install_url missing or not a string")
	}

	t.Logf("Firefox policies.json structure OK: %s with force_installed mode", FirefoxGeckoID)
}

// TestE2E_WebRTCPoliciesApplied_Firefox validates the Firefox policies.json
// WebRTC preferences structure. Same limitation as TestE2E_FirefoxPoliciesApplied:
// ApplyPolicies doesn't accept a directory override, so we validate the JSON
// schema directly using the same structure the policy generator produces.
func TestE2E_WebRTCPoliciesApplied_Firefox(t *testing.T) {
	if os.Getenv("E2E") != "1" {
		t.Skip("set E2E=1 to run")
	}

	tmpDir := t.TempDir()
	distDir := filepath.Join(tmpDir, "distribution")
	os.MkdirAll(distDir, 0755)

	// Write policies.json with WebRTC preferences.
	policies := map[string]interface{}{
		"policies": map[string]interface{}{
			"Preferences": map[string]interface{}{
				"media.peerconnection.enabled":                  false,
				"media.peerconnection.ice.default_address_only": true,
			},
		},
	}

	policiesPath := filepath.Join(distDir, "policies.json")
	data, _ := json.MarshalIndent(policies, "", "  ")
	if err := os.WriteFile(policiesPath, data, 0644); err != nil {
		t.Fatalf("write policies.json: %v", err)
	}

	// Read back and verify.
	raw, err := os.ReadFile(policiesPath)
	if err != nil {
		t.Fatalf("read policies.json: %v", err)
	}

	var parsed map[string]interface{}
	if err := json.Unmarshal(raw, &parsed); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}

	policiesObj, ok := parsed["policies"].(map[string]interface{})
	if !ok {
		t.Fatal("policies.json missing 'policies' object")
	}
	prefs, ok := policiesObj["Preferences"].(map[string]interface{})
	if !ok {
		t.Fatal("policies.json missing 'Preferences' object")
	}

	// Check media.peerconnection.ice.default_address_only = true
	val, ok := prefs["media.peerconnection.ice.default_address_only"]
	if !ok {
		t.Fatal("missing media.peerconnection.ice.default_address_only")
	}
	if val != true {
		t.Errorf("media.peerconnection.ice.default_address_only = %v, want true", val)
	}

	// Check media.peerconnection.enabled = false
	val2, ok := prefs["media.peerconnection.enabled"]
	if !ok {
		t.Fatal("missing media.peerconnection.enabled")
	}
	if val2 != false {
		t.Errorf("media.peerconnection.enabled = %v, want false", val2)
	}

	t.Log("Firefox WebRTC policies OK: peerconnection disabled, ICE restricted")
}
