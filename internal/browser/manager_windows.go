//go:build windows

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

	"golang.org/x/sys/windows/registry"
)

// windowsPolicyManager implements PolicyManager via HKLM registry.
type windowsPolicyManager struct {
	mu         sync.Mutex
	savedState *policyPersistedState
	lockCloser io.Closer
}

// NewPolicyManager creates a Windows policy manager.
func NewPolicyManager() PolicyManager {
	return &windowsPolicyManager{}
}

// ApplyPolicies detects browsers and applies WebRTC policies via registry.
func (m *windowsPolicyManager) ApplyPolicies(ctx context.Context) (*ApplyResult, error) {
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
			extErr = m.applyExtensionFirefox(&saved)
		}
		if extErr != nil {
			extFailed = true
			result.Failed = append(result.Failed, BrowserError{
				Name:   b.Name,
				Reason: "extension: " + extErr.Error(),
			})
		}

		// Persist incrementally after each browser.
		m.savedState.Browsers = append(m.savedState.Browsers, saved)
		if err := persistStateIncremental(m.savedState); err != nil {
			// Persistence failed — rollback everything from memory.
			m.rollbackFromMemory()
			m.releaseLock()
			return &ApplyResult{}, fmt.Errorf("browser: apply: persist failed, rolled back: %w", err)
		}

		// Verify post-apply. Keep in savedState regardless so RestorePolicies
		// can undo the write even if verification fails (avoids orphaning).
		if !m.verifyPolicy(b) {
			result.Failed = append(result.Failed, BrowserError{
				Name:   b.Name,
				Reason: "post-apply verification failed (GPO or permissions)",
			})
			continue
		}

		// Verify extension policy was written correctly.
		if !extFailed && !m.verifyExtensionPolicy(b) {
			extFailed = true
			result.Failed = append(result.Failed, BrowserError{
				Name:   b.Name,
				Reason: "extension: post-apply verification failed (GPO or permissions)",
			})
		}

		if !extFailed {
			result.Applied = append(result.Applied, b.Name)
		}
	}

	return result, nil
}

// applyChromium writes the WebRTC policy to a Chromium browser's registry key.
func (m *windowsPolicyManager) applyChromium(b BrowserInfo) (browserSavedState, error) {
	saved := browserSavedState{
		Name:       b.Name,
		Family:     b.Family,
		PolicyPath: b.PolicyPath,
	}

	k, err := registry.OpenKey(registry.LOCAL_MACHINE, b.PolicyPath, registry.QUERY_VALUE|registry.SET_VALUE)
	if err != nil {
		// Key doesn't exist — create it.
		k, _, err = registry.CreateKey(registry.LOCAL_MACHINE, b.PolicyPath, registry.ALL_ACCESS)
		if err != nil {
			return saved, fmt.Errorf("browser: chromium: create key %s: %w", b.PolicyPath, err)
		}
	}
	defer k.Close()

	// Save original value if it exists.
	original, _, err := k.GetStringValue(chromiumPolicyKey)
	if err == nil {
		saved.OriginalValue = original
		saved.HadOriginal = true
	}

	if err := k.SetStringValue(chromiumPolicyKey, chromiumPolicyValue); err != nil {
		return saved, fmt.Errorf("browser: chromium: set value %s: %w", b.Name, err)
	}

	return saved, nil
}

// applyFirefox writes the WebRTC preferences to Firefox's registry policy key.
func (m *windowsPolicyManager) applyFirefox(b BrowserInfo) (browserSavedState, error) {
	saved := browserSavedState{
		Name:           b.Name,
		Family:         b.Family,
		PolicyPath:     b.PolicyPath,
		OriginalValues: make(map[string]string),
	}

	k, err := registry.OpenKey(registry.LOCAL_MACHINE, b.PolicyPath, registry.QUERY_VALUE|registry.SET_VALUE)
	if err != nil {
		k, _, err = registry.CreateKey(registry.LOCAL_MACHINE, b.PolicyPath, registry.ALL_ACCESS)
		if err != nil {
			return saved, fmt.Errorf("browser: firefox: create key %s: %w", b.PolicyPath, err)
		}
	}
	defer k.Close()

	for prefKey, prefVal := range firefoxPrefs {
		// Save original DWORD value if it exists.
		if original, _, err := k.GetIntegerValue(prefKey); err == nil {
			saved.OriginalValues[prefKey] = fmt.Sprintf("%d", original)
			saved.HadOriginal = true
		}

		// Convert bool → DWORD (false=0, true=1).
		var dword uint32
		if b, ok := prefVal.(bool); ok && b {
			dword = 1
		}
		if err := k.SetDWordValue(prefKey, dword); err != nil {
			return saved, fmt.Errorf("browser: firefox: set %s: %w", prefKey, err)
		}
	}

	return saved, nil
}

// chromiumExtPolicyKey is the ExtensionSettings policy name for Chromium browsers.
const chromiumExtPolicyKey = "ExtensionSettings"

// firefoxExtPolicyPath is the HKLM registry path for Firefox ExtensionSettings.
const firefoxExtPolicyPath = `SOFTWARE\Policies\Mozilla\Firefox`

// firefoxExtPolicyKey is the ExtensionSettings value name under the Firefox policy key.
const firefoxExtPolicyKey = "ExtensionSettings"

// applyExtensionChromium writes the ExtensionSettings policy to force-install the extension.
// It merges Le Voile's entry into any existing ExtensionSettings JSON to preserve
// enterprise extension policies already in place.
func (m *windowsPolicyManager) applyExtensionChromium(b BrowserInfo, saved *browserSavedState) error {
	if ExtensionID == "" {
		return fmt.Errorf("browser: extension: no extension ID configured")
	}

	k, err := registry.OpenKey(registry.LOCAL_MACHINE, b.PolicyPath, registry.QUERY_VALUE|registry.SET_VALUE)
	if err != nil {
		k, _, err = registry.CreateKey(registry.LOCAL_MACHINE, b.PolicyPath, registry.ALL_ACCESS)
		if err != nil {
			return fmt.Errorf("browser: extension chromium: create key %s: %w", b.PolicyPath, err)
		}
	}
	defer k.Close()

	saved.ExtPolicyPath = b.PolicyPath

	// Read existing ExtensionSettings value.
	existing, _, readErr := k.GetStringValue(chromiumExtPolicyKey)
	if readErr == nil {
		saved.ExtOriginalValue = existing
		saved.ExtHadOriginal = true
	}

	// Build the merged JSON.
	// Use Chrome Web Store URL if configured (required for non-managed devices),
	// otherwise fall back to local file:// URL (works on AD-joined machines).
	updateURL := ChromeStoreUpdateURL
	if updateURL == "" {
		deployDir := extensionDeployDir()
		updatesXMLPath := strings.ReplaceAll(filepath.Join(deployDir, "chrome", "updates.xml"), `\`, `/`)
		updateURL = "file:///" + updatesXMLPath
	}

	merged, err := mergeExtensionSettings(existing, readErr == nil, ExtensionID, map[string]string{
		"installation_mode": "force_installed",
		"update_url":        updateURL,
	})
	if err != nil {
		return fmt.Errorf("browser: extension chromium: merge %s: %w", b.Name, err)
	}

	if err := k.SetStringValue(chromiumExtPolicyKey, merged); err != nil {
		return fmt.Errorf("browser: extension chromium: set %s: %w", b.Name, err)
	}

	return nil
}

// applyExtensionFirefox writes the ExtensionSettings policy to force-install the Firefox extension.
// Uses the Firefox-specific registry path (SOFTWARE\Policies\Mozilla\Firefox\ExtensionSettings).
func (m *windowsPolicyManager) applyExtensionFirefox(saved *browserSavedState) error {
	k, err := registry.OpenKey(registry.LOCAL_MACHINE, firefoxExtPolicyPath, registry.QUERY_VALUE|registry.SET_VALUE)
	if err != nil {
		k, _, err = registry.CreateKey(registry.LOCAL_MACHINE, firefoxExtPolicyPath, registry.ALL_ACCESS)
		if err != nil {
			return fmt.Errorf("browser: extension firefox: create key %s: %w", firefoxExtPolicyPath, err)
		}
	}
	defer k.Close()

	saved.ExtPolicyPath = firefoxExtPolicyPath

	// Read existing ExtensionSettings value.
	existing, _, readErr := k.GetStringValue(firefoxExtPolicyKey)
	if readErr == nil {
		saved.ExtOriginalValue = existing
		saved.ExtHadOriginal = true
	}

	deployDir := extensionDeployDir()
	xpiPath := strings.ReplaceAll(filepath.Join(deployDir, "levoile.xpi"), `\`, `/`)
	installURL := "file:///" + xpiPath

	merged, err := mergeExtensionSettings(existing, readErr == nil, FirefoxGeckoID, map[string]string{
		"installation_mode": "force_installed",
		"install_url":       installURL,
	})
	if err != nil {
		return fmt.Errorf("browser: extension firefox: merge: %w", err)
	}

	if err := k.SetStringValue(firefoxExtPolicyKey, merged); err != nil {
		return fmt.Errorf("browser: extension firefox: set: %w", err)
	}

	return nil
}

// mergeExtensionSettings merges a Le Voile extension entry into an existing
// ExtensionSettings JSON string. If the existing JSON is invalid, returns an error
// (E4 guard — never corrupt pre-existing enterprise policies).
func mergeExtensionSettings(existing string, hadExisting bool, extID string, entry map[string]string) (string, error) {
	settings := make(map[string]interface{})

	if hadExisting && existing != "" {
		if err := json.Unmarshal([]byte(existing), &settings); err != nil {
			return "", fmt.Errorf("existing ExtensionSettings JSON is invalid: %w", err)
		}
	}

	// Add Le Voile entry.
	entryMap := make(map[string]interface{}, len(entry))
	for k, v := range entry {
		entryMap[k] = v
	}
	settings[extID] = entryMap

	data, err := json.Marshal(settings)
	if err != nil {
		return "", fmt.Errorf("marshal merged ExtensionSettings: %w", err)
	}
	return string(data), nil
}

// restoreExtensionChromium restores the original ExtensionSettings for a Chromium browser.
func restoreExtensionChromium(b browserSavedState) error {
	if b.ExtPolicyPath == "" {
		return nil
	}
	k, err := registry.OpenKey(registry.LOCAL_MACHINE, b.ExtPolicyPath, registry.SET_VALUE|registry.QUERY_VALUE)
	if err != nil {
		return nil
	}
	defer k.Close()

	if b.ExtHadOriginal {
		return k.SetStringValue(chromiumExtPolicyKey, b.ExtOriginalValue)
	}
	return k.DeleteValue(chromiumExtPolicyKey)
}

// restoreExtensionFirefox restores the original ExtensionSettings for Firefox.
func restoreExtensionFirefox(b browserSavedState) error {
	if b.ExtPolicyPath == "" {
		return nil
	}
	k, err := registry.OpenKey(registry.LOCAL_MACHINE, b.ExtPolicyPath, registry.SET_VALUE|registry.QUERY_VALUE)
	if err != nil {
		return nil
	}
	defer k.Close()

	if b.ExtHadOriginal {
		return k.SetStringValue(firefoxExtPolicyKey, b.ExtOriginalValue)
	}
	return k.DeleteValue(firefoxExtPolicyKey)
}

// verifyPolicy re-reads the written policy to confirm it stuck.
func (m *windowsPolicyManager) verifyPolicy(b BrowserInfo) bool {
	k, err := registry.OpenKey(registry.LOCAL_MACHINE, b.PolicyPath, registry.QUERY_VALUE)
	if err != nil {
		return false
	}
	defer k.Close()

	switch b.Family {
	case Chromium:
		val, _, err := k.GetStringValue(chromiumPolicyKey)
		return err == nil && val == chromiumPolicyValue
	case Firefox:
		for prefKey, prefVal := range firefoxPrefs {
			val, _, err := k.GetIntegerValue(prefKey)
			if err != nil {
				return false
			}
			var expected uint32
			if b, ok := prefVal.(bool); ok && b {
				expected = 1
			}
			if val != uint64(expected) {
				return false
			}
		}
		return true
	}
	return false
}

// verifyExtensionPolicy re-reads the ExtensionSettings value to confirm it was
// written with the correct installation_mode. For Chromium, reuses the already-open
// PolicyPath key when provided; otherwise opens its own.
func (m *windowsPolicyManager) verifyExtensionPolicy(b BrowserInfo) bool {
	switch b.Family {
	case Chromium:
		// Reuse same key path as verifyPolicy — open once for both checks.
		k, err := registry.OpenKey(registry.LOCAL_MACHINE, b.PolicyPath, registry.QUERY_VALUE)
		if err != nil {
			return false
		}
		defer k.Close()
		return verifyExtensionEntry(k, chromiumExtPolicyKey, ExtensionID)
	case Firefox:
		k, err := registry.OpenKey(registry.LOCAL_MACHINE, firefoxExtPolicyPath, registry.QUERY_VALUE)
		if err != nil {
			return false
		}
		defer k.Close()
		return verifyExtensionEntry(k, firefoxExtPolicyKey, FirefoxGeckoID)
	}
	return false
}

// verifyExtensionEntry checks that the given registry value contains a JSON object
// with the expected extension ID and installation_mode == "force_installed".
func verifyExtensionEntry(k registry.Key, valueName, extID string) bool {
	val, _, err := k.GetStringValue(valueName)
	if err != nil {
		return false
	}
	var settings map[string]json.RawMessage
	if json.Unmarshal([]byte(val), &settings) != nil {
		return false
	}
	raw, ok := settings[extID]
	if !ok {
		return false
	}
	var entry map[string]interface{}
	if json.Unmarshal(raw, &entry) != nil {
		return false
	}
	return entry["installation_mode"] == "force_installed"
}

// RestorePolicies restores original browser policies and cleans up.
func (m *windowsPolicyManager) RestorePolicies(ctx context.Context) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.savedState == nil {
		m.releaseLock()
		return nil
	}

	var lastErr error
	for _, b := range m.savedState.Browsers {
		if err := m.restoreOne(b); err != nil {
			lastErr = err
		}
	}

	cleanupExtensionFiles()

	m.savedState = nil
	removePersistedState()
	m.releaseLock()
	return lastErr
}

// cleanupExtensionFiles removes the deployed extension files directory.
func cleanupExtensionFiles() {
	os.RemoveAll(extensionDeployDir())
}

// restoreOne restores a single browser's original policy (WebRTC + extension).
func (m *windowsPolicyManager) restoreOne(b browserSavedState) error {
	var lastErr error

	// Restore WebRTC policy.
	k, err := registry.OpenKey(registry.LOCAL_MACHINE, b.PolicyPath, registry.SET_VALUE|registry.QUERY_VALUE)
	if err == nil {
		switch b.Family {
		case Chromium:
			if b.HadOriginal {
				lastErr = k.SetStringValue(chromiumPolicyKey, b.OriginalValue)
			} else {
				lastErr = k.DeleteValue(chromiumPolicyKey)
			}
		case Firefox:
			for prefKey := range firefoxPrefs {
				if origStr, ok := b.OriginalValues[prefKey]; ok {
					var val uint32
					if _, err := fmt.Sscanf(origStr, "%d", &val); err != nil {
						lastErr = fmt.Errorf("browser: firefox restore: parse %q=%q: %w", prefKey, origStr, err)
						continue
					}
					if err := k.SetDWordValue(prefKey, val); err != nil {
						lastErr = err
					}
				} else {
					_ = k.DeleteValue(prefKey)
				}
			}
		}
		k.Close()
	}

	// Restore extension policies.
	switch b.Family {
	case Chromium:
		if err := restoreExtensionChromium(b); err != nil {
			lastErr = err
		}
	case Firefox:
		if err := restoreExtensionFirefox(b); err != nil {
			lastErr = err
		}
	}

	return lastErr
}

// rollbackFromMemory reverses all applied policies using in-memory state.
func (m *windowsPolicyManager) rollbackFromMemory() {
	if m.savedState == nil {
		return
	}
	for _, b := range m.savedState.Browsers {
		m.restoreOne(b)
	}
	m.savedState = nil
}

// releaseLock releases the advisory lock.
func (m *windowsPolicyManager) releaseLock() {
	if m.lockCloser != nil {
		m.lockCloser.Close()
		m.lockCloser = nil
	}
}

// RecoverOrphanPolicies restores policies from a previous crashed session.
// Called from the service at startup, before tunnel connect.
func RecoverOrphanPolicies(_ context.Context) error {
	cleanOrphanTemps()

	state, err := loadPersistedState()
	if err != nil {
		if strings.Contains(err.Error(), "no persisted state found") {
			return nil // nothing to recover
		}
		// Corrupt state file — remove it so it doesn't block future startups.
		removePersistedState()
		return fmt.Errorf("browser: recover orphan: load state: %w", err)
	}

	if len(state.Browsers) == 0 {
		removePersistedState()
		return nil
	}

	var lastErr error
	mgr := &windowsPolicyManager{}
	for _, b := range state.Browsers {
		if err := mgr.restoreOne(b); err != nil {
			lastErr = fmt.Errorf("browser: recover orphan %s: %w", b.Name, err)
		}
	}

	cleanupExtensionFiles()
	removePersistedState()
	return lastErr
}
