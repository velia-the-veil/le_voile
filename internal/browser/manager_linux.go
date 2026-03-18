//go:build linux

package browser

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
)

// linuxPolicyManager implements PolicyManager via filesystem policies.
type linuxPolicyManager struct {
	mu         sync.Mutex
	savedState *policyPersistedState
	lockCloser io.Closer
}

// NewPolicyManager creates a Linux policy manager.
func NewPolicyManager() PolicyManager {
	return &linuxPolicyManager{}
}

// ApplyPolicies detects browsers and applies WebRTC policies via filesystem.
func (m *linuxPolicyManager) ApplyPolicies(ctx context.Context) (*ApplyResult, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	lock, err := acquireLock()
	if err != nil {
		return &ApplyResult{}, fmt.Errorf("browser: apply: %w", err)
	}
	m.lockCloser = lock

	browsers, err := DetectBrowsers()
	if err != nil {
		m.releaseLock()
		return &ApplyResult{}, fmt.Errorf("browser: apply: detect: %w", err)
	}

	if len(browsers) == 0 {
		m.releaseLock()
		return &ApplyResult{}, nil
	}

	// Deploy extension files before writing policies that reference them.
	if err := deployExtensionFiles(); err != nil {
		m.releaseLock()
		return &ApplyResult{}, fmt.Errorf("browser: apply: deploy extension: %w", err)
	}

	result := &ApplyResult{}
	m.savedState = &policyPersistedState{
		Extension: &extensionDeployState{DeployDir: extensionDeployDir()},
	}

	for _, b := range browsers {
		var applyErr error
		var saved browserSavedState

		switch b.Family {
		case Chromium:
			saved, applyErr = m.applyChromium(b)
		case Firefox:
			saved, applyErr = m.applyFirefox(b)
		}

		if applyErr != nil {
			result.Failed = append(result.Failed, BrowserError{
				Name:   b.Name,
				Reason: applyErr.Error(),
			})
			continue
		}

		// Apply extension installation policies (after WebRTC policies).
		extFailed := false
		var extErr error
		switch b.Family {
		case Chromium:
			extErr = m.applyExtensionChromium(b, &saved)
		case Firefox:
			extErr = m.applyExtensionFirefox(b, &saved)
		}
		if extErr != nil {
			extFailed = true
			result.Failed = append(result.Failed, BrowserError{
				Name:   b.Name,
				Reason: "extension: " + extErr.Error(),
			})
		}

		m.savedState.Browsers = append(m.savedState.Browsers, saved)
		if err := persistStateIncremental(m.savedState); err != nil {
			m.rollbackFromMemory()
			m.releaseLock()
			return &ApplyResult{}, fmt.Errorf("browser: apply: persist failed, rolled back: %w", err)
		}

		if !extFailed {
			result.Applied = append(result.Applied, b.Name)
		}
	}

	return result, nil
}

// applyChromium writes a policy file into the managed policies directory.
func (m *linuxPolicyManager) applyChromium(b BrowserInfo) (browserSavedState, error) {
	saved := browserSavedState{
		Name:       b.Name,
		Family:     b.Family,
		PolicyPath: b.PolicyPath,
	}

	policyFile := filepath.Join(b.PolicyPath, chromiumPolicyFileName)

	// Create directory if needed.
	if err := os.MkdirAll(b.PolicyPath, 0755); err != nil {
		return saved, fmt.Errorf("browser: chromium: mkdir %s: %w", b.PolicyPath, err)
	}

	// Save original if exists.
	if data, err := os.ReadFile(policyFile); err == nil {
		saved.OriginalValue = string(data)
		saved.HadOriginal = true
	}

	content := fmt.Sprintf(`{"WebRtcIPHandlingPolicy": "disable_non_proxied_udp"}`)
	if err := atomicWriteFile(policyFile, []byte(content+"\n"), 0644); err != nil {
		return saved, fmt.Errorf("browser: chromium: write %s: %w", policyFile, err)
	}

	return saved, nil
}

// applyFirefox performs a deep merge into the Firefox policies.json file.
func (m *linuxPolicyManager) applyFirefox(b BrowserInfo) (browserSavedState, error) {
	saved := browserSavedState{
		Name:       b.Name,
		Family:     b.Family,
		PolicyPath: b.PolicyPath,
	}

	// Create parent directory if needed.
	dir := filepath.Dir(b.PolicyPath)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return saved, fmt.Errorf("browser: firefox: mkdir %s: %w", dir, err)
	}

	// Read existing policies.json (if exists).
	var root map[string]interface{}
	existingData, readErr := os.ReadFile(b.PolicyPath)
	if readErr == nil {
		saved.OriginalValue = string(existingData)
		saved.HadOriginal = true

		if err := json.Unmarshal(existingData, &root); err != nil {
			return saved, fmt.Errorf("browser: firefox: invalid JSON in %s: %w", b.PolicyPath, err)
		}
	} else {
		root = make(map[string]interface{})
	}

	// Deep merge: set all firefoxPrefs in policies.Preferences
	policies, ok := root["policies"].(map[string]interface{})
	if !ok {
		policies = make(map[string]interface{})
		root["policies"] = policies
	}

	prefs, ok := policies["Preferences"].(map[string]interface{})
	if !ok {
		prefs = make(map[string]interface{})
		policies["Preferences"] = prefs
	}

	for prefKey, prefVal := range firefoxPrefs {
		prefs[prefKey] = prefVal
	}

	// Marshal and write atomically.
	data, err := json.MarshalIndent(root, "", "  ")
	if err != nil {
		return saved, fmt.Errorf("browser: firefox: marshal: %w", err)
	}

	// Validate the result is valid JSON.
	var validate interface{}
	if err := json.Unmarshal(data, &validate); err != nil {
		return saved, fmt.Errorf("browser: firefox: validation failed: %w", err)
	}

	if err := atomicWriteFile(b.PolicyPath, append(data, '\n'), 0644); err != nil {
		return saved, fmt.Errorf("browser: firefox: write %s: %w", b.PolicyPath, err)
	}

	return saved, nil
}

// chromiumExtPolicyFileName is the policy file for Chromium extension installation (Linux).
const chromiumExtPolicyFileName = "levoile-extension.json"

// applyExtensionChromium writes a separate extension policy file in the Chromium managed dir.
// Merges with existing ExtensionSettings if the file already exists.
func (m *linuxPolicyManager) applyExtensionChromium(b BrowserInfo, saved *browserSavedState) error {
	if ExtensionID == "" {
		return fmt.Errorf("browser: extension: no extension ID configured")
	}

	policyFile := filepath.Join(b.PolicyPath, chromiumExtPolicyFileName)
	saved.ExtPolicyPath = policyFile

	// Read existing file if present.
	if data, err := os.ReadFile(policyFile); err == nil {
		saved.ExtOriginalValue = string(data)
		saved.ExtHadOriginal = true
	}

	// Use Chrome Web Store URL if configured (required for non-managed devices),
	// otherwise fall back to local file:// URL (works on AD-joined machines).
	updateURL := ChromeStoreUpdateURL
	if updateURL == "" {
		deployDir := extensionDeployDir()
		updatesXMLPath := filepath.Join(deployDir, "chrome", "updates.xml")
		updateURL = "file://" + updatesXMLPath
	}

	// Build ExtensionSettings JSON.
	var root map[string]interface{}
	if saved.ExtHadOriginal {
		if err := json.Unmarshal([]byte(saved.ExtOriginalValue), &root); err != nil {
			return fmt.Errorf("browser: extension chromium: invalid JSON in %s: %w", policyFile, err)
		}
	} else {
		root = make(map[string]interface{})
	}

	extSettings, ok := root["ExtensionSettings"].(map[string]interface{})
	if !ok {
		extSettings = make(map[string]interface{})
		root["ExtensionSettings"] = extSettings
	}

	extSettings[ExtensionID] = map[string]interface{}{
		"installation_mode": "force_installed",
		"update_url":        updateURL,
	}

	data, err := json.MarshalIndent(root, "", "  ")
	if err != nil {
		return fmt.Errorf("browser: extension chromium: marshal: %w", err)
	}

	if err := os.MkdirAll(b.PolicyPath, 0755); err != nil {
		return fmt.Errorf("browser: extension chromium: mkdir %s: %w", b.PolicyPath, err)
	}

	if err := atomicWriteFile(policyFile, append(data, '\n'), 0644); err != nil {
		return fmt.Errorf("browser: extension chromium: write %s: %w", policyFile, err)
	}

	return nil
}

// applyExtensionFirefox performs a deep merge to add ExtensionSettings to Firefox policies.json.
func (m *linuxPolicyManager) applyExtensionFirefox(b BrowserInfo, saved *browserSavedState) error {
	saved.ExtPolicyPath = b.PolicyPath

	// Read current policies.json (may already contain our WebRTC changes + other policies).
	var root map[string]interface{}
	existingData, readErr := os.ReadFile(b.PolicyPath)
	if readErr == nil {
		if err := json.Unmarshal(existingData, &root); err != nil {
			return fmt.Errorf("browser: extension firefox: invalid JSON in %s: %w", b.PolicyPath, err)
		}
	} else {
		root = make(map[string]interface{})
	}

	// Note: We don't save ExtOriginalValue here because the WebRTC applyFirefox
	// already saved the original in OriginalValue. Extension restore uses reverse merge.

	deployDir := extensionDeployDir()
	xpiPath := filepath.Join(deployDir, "levoile.xpi")
	installURL := "file://" + xpiPath

	// Deep merge: policies → ExtensionSettings → levoile entry.
	policies, ok := root["policies"].(map[string]interface{})
	if !ok {
		policies = make(map[string]interface{})
		root["policies"] = policies
	}

	extSettings, ok := policies["ExtensionSettings"].(map[string]interface{})
	if !ok {
		extSettings = make(map[string]interface{})
		policies["ExtensionSettings"] = extSettings
	}

	extSettings[FirefoxGeckoID] = map[string]interface{}{
		"installation_mode": "force_installed",
		"install_url":       installURL,
	}

	data, err := json.MarshalIndent(root, "", "  ")
	if err != nil {
		return fmt.Errorf("browser: extension firefox: marshal: %w", err)
	}

	dir := filepath.Dir(b.PolicyPath)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return fmt.Errorf("browser: extension firefox: mkdir %s: %w", dir, err)
	}

	if err := atomicWriteFile(b.PolicyPath, append(data, '\n'), 0644); err != nil {
		return fmt.Errorf("browser: extension firefox: write %s: %w", b.PolicyPath, err)
	}

	return nil
}

// RestorePolicies restores original browser policies and cleans up.
func (m *linuxPolicyManager) RestorePolicies(ctx context.Context) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.savedState == nil {
		m.releaseLock()
		return nil
	}

	var lastErr error
	for _, b := range m.savedState.Browsers {
		if err := restoreOneLinux(b); err != nil {
			lastErr = err
		}
	}

	cleanupExtensionFilesLinux()

	m.savedState = nil
	removePersistedState()
	m.releaseLock()
	return lastErr
}

// cleanupExtensionFilesLinux removes the deployed extension files directory.
func cleanupExtensionFilesLinux() {
	os.RemoveAll(extensionDeployDir())
}

// restoreOneLinux restores a single browser's original policy on Linux.
func restoreOneLinux(b browserSavedState) error {
	var lastErr error

	// Restore WebRTC policies.
	switch b.Family {
	case Chromium:
		policyFile := filepath.Join(b.PolicyPath, chromiumPolicyFileName)
		if b.HadOriginal {
			lastErr = atomicWriteFile(policyFile, []byte(b.OriginalValue), 0644)
		} else {
			if err := os.Remove(policyFile); err != nil && !os.IsNotExist(err) {
				lastErr = err
			}
		}
	case Firefox:
		lastErr = restoreFirefoxLinux(b)
	}

	// Restore extension policies.
	switch b.Family {
	case Chromium:
		if err := restoreExtensionChromiumLinux(b); err != nil {
			lastErr = err
		}
	case Firefox:
		// Firefox extension settings are in the same policies.json — restored by restoreFirefoxLinux.
		// The reverse merge in restoreFirefoxLinux restores OriginalValue which didn't have ExtensionSettings.
	}

	return lastErr
}

// restoreExtensionChromiumLinux removes or restores the extension policy file.
func restoreExtensionChromiumLinux(b browserSavedState) error {
	if b.ExtPolicyPath == "" {
		return nil
	}
	if b.ExtHadOriginal {
		return atomicWriteFile(b.ExtPolicyPath, []byte(b.ExtOriginalValue), 0644)
	}
	if err := os.Remove(b.ExtPolicyPath); err != nil && !os.IsNotExist(err) {
		return err
	}
	return nil
}

// restoreFirefoxLinux performs a reverse merge on the Firefox policies.json.
func restoreFirefoxLinux(b browserSavedState) error {
	if !b.HadOriginal {
		// File didn't exist before Le Voile — remove it.
		return os.Remove(b.PolicyPath)
	}

	// Read current file (may have been modified by admin during session).
	currentData, err := os.ReadFile(b.PolicyPath)
	if err != nil {
		// File gone — write back original.
		return atomicWriteFile(b.PolicyPath, []byte(b.OriginalValue), 0644)
	}

	var root map[string]interface{}
	if err := json.Unmarshal(currentData, &root); err != nil {
		// Current file is corrupt — restore original.
		return atomicWriteFile(b.PolicyPath, []byte(b.OriginalValue), 0644)
	}

	// Reverse merge: remove only our key, preserve everything else.
	policies, ok := root["policies"].(map[string]interface{})
	if !ok {
		return nil
	}
	prefs, ok := policies["Preferences"].(map[string]interface{})
	if !ok {
		return nil
	}

	// Reverse merge: for each pref we set, restore original or delete.
	var origPrefs map[string]interface{}
	var origExtSettings map[string]interface{}
	var origRoot map[string]interface{}
	if err := json.Unmarshal([]byte(b.OriginalValue), &origRoot); err == nil {
		if origPolicies, ok := origRoot["policies"].(map[string]interface{}); ok {
			origPrefs, _ = origPolicies["Preferences"].(map[string]interface{})
			origExtSettings, _ = origPolicies["ExtensionSettings"].(map[string]interface{})
		}
	}

	for prefKey := range firefoxPrefs {
		if origPrefs != nil {
			if origVal, exists := origPrefs[prefKey]; exists {
				prefs[prefKey] = origVal
				continue
			}
		}
		delete(prefs, prefKey)
	}

	// Reverse merge ExtensionSettings: remove Le Voile entry added by applyExtensionFirefox.
	if extSettings, ok := policies["ExtensionSettings"].(map[string]interface{}); ok {
		delete(extSettings, FirefoxGeckoID)
		// Restore original ExtensionSettings entries if they existed before Le Voile.
		if origExtSettings != nil {
			for k, v := range origExtSettings {
				extSettings[k] = v
			}
		}
		if len(extSettings) == 0 {
			delete(policies, "ExtensionSettings")
		}
	}

	// Clean up empty containers to avoid leaving cruft.
	if len(prefs) == 0 {
		delete(policies, "Preferences")
	}
	if len(policies) == 0 {
		delete(root, "policies")
	}

	data, err := json.MarshalIndent(root, "", "  ")
	if err != nil {
		return fmt.Errorf("browser: firefox restore: marshal: %w", err)
	}

	return atomicWriteFile(b.PolicyPath, append(data, '\n'), 0644)
}

// rollbackFromMemory reverses all applied policies using in-memory state.
func (m *linuxPolicyManager) rollbackFromMemory() {
	if m.savedState == nil {
		return
	}
	for _, b := range m.savedState.Browsers {
		restoreOneLinux(b)
	}
	m.savedState = nil
}

// releaseLock releases the advisory lock.
func (m *linuxPolicyManager) releaseLock() {
	if m.lockCloser != nil {
		m.lockCloser.Close()
		m.lockCloser = nil
	}
}

// atomicWriteFile writes data to a file atomically via temp+rename.
func atomicWriteFile(path string, data []byte, perm os.FileMode) error {
	dir := filepath.Dir(path)
	tmp, err := os.CreateTemp(dir, ".levoile-*.tmp")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()

	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		os.Remove(tmpName)
		return err
	}
	if err := tmp.Chmod(perm); err != nil {
		tmp.Close()
		os.Remove(tmpName)
		return err
	}
	if err := tmp.Close(); err != nil {
		os.Remove(tmpName)
		return err
	}
	if err := os.Rename(tmpName, path); err != nil {
		os.Remove(tmpName)
		return err
	}
	return nil
}

// RecoverOrphanPolicies restores policies from a previous crashed session.
// Called from the service at startup, before tunnel connect.
func RecoverOrphanPolicies(_ context.Context) error {
	cleanOrphanTemps()

	state, err := loadPersistedState()
	if err != nil {
		if strings.Contains(err.Error(), "no persisted state found") {
			return nil
		}
		removePersistedState()
		return fmt.Errorf("browser: recover orphan: load state: %w", err)
	}

	if len(state.Browsers) == 0 {
		removePersistedState()
		return nil
	}

	var lastErr error
	for _, b := range state.Browsers {
		if err := restoreOneLinux(b); err != nil {
			lastErr = fmt.Errorf("browser: recover orphan %s: %w", b.Name, err)
		}
	}

	cleanupExtensionFilesLinux()
	removePersistedState()
	return lastErr
}
