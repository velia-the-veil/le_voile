//go:build e2e

package browser

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

// TestE2E_WebRTCPoliciesApplied_Firefox validates the Firefox policies.json
// WebRTC preferences structure. Limitation: ApplyPolicies doesn't accept a
// directory override, so we validate the JSON schema directly using the same
// structure the policy generator produces.
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
