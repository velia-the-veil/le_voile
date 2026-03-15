//go:build windows

package browser

import (
	"context"
	"fmt"
	"io"
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

	result := &ApplyResult{}
	m.savedState = &policyPersistedState{}

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

		result.Applied = append(result.Applied, b.Name)
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

// applyFirefox writes the WebRTC preference to Firefox's registry policy key.
func (m *windowsPolicyManager) applyFirefox(b BrowserInfo) (browserSavedState, error) {
	saved := browserSavedState{
		Name:       b.Name,
		Family:     b.Family,
		PolicyPath: b.PolicyPath,
	}

	k, err := registry.OpenKey(registry.LOCAL_MACHINE, b.PolicyPath, registry.QUERY_VALUE|registry.SET_VALUE)
	if err != nil {
		k, _, err = registry.CreateKey(registry.LOCAL_MACHINE, b.PolicyPath, registry.ALL_ACCESS)
		if err != nil {
			return saved, fmt.Errorf("browser: firefox: create key %s: %w", b.PolicyPath, err)
		}
	}
	defer k.Close()

	// Save original DWORD value if it exists.
	original, _, err := k.GetIntegerValue(firefoxPrefKey)
	if err == nil {
		saved.OriginalValue = fmt.Sprintf("%d", original)
		saved.HadOriginal = true
	}

	if err := k.SetDWordValue(firefoxPrefKey, 1); err != nil {
		return saved, fmt.Errorf("browser: firefox: set value: %w", err)
	}

	return saved, nil
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
		val, _, err := k.GetIntegerValue(firefoxPrefKey)
		return err == nil && val == 1
	}
	return false
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

	m.savedState = nil
	removePersistedState()
	m.releaseLock()
	return lastErr
}

// restoreOne restores a single browser's original policy.
func (m *windowsPolicyManager) restoreOne(b browserSavedState) error {
	k, err := registry.OpenKey(registry.LOCAL_MACHINE, b.PolicyPath, registry.SET_VALUE|registry.QUERY_VALUE)
	if err != nil {
		return nil // key gone — nothing to restore
	}
	defer k.Close()

	switch b.Family {
	case Chromium:
		if b.HadOriginal {
			return k.SetStringValue(chromiumPolicyKey, b.OriginalValue)
		}
		return k.DeleteValue(chromiumPolicyKey)
	case Firefox:
		if b.HadOriginal {
			var val uint32
			if _, err := fmt.Sscanf(b.OriginalValue, "%d", &val); err != nil {
				return fmt.Errorf("browser: firefox restore: parse original value %q: %w", b.OriginalValue, err)
			}
			return k.SetDWordValue(firefoxPrefKey, val)
		}
		return k.DeleteValue(firefoxPrefKey)
	}
	return nil
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

	removePersistedState()
	return lastErr
}
