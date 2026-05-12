package browser

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
)

// ApplyResult reports which browsers had policies applied or failed.
type ApplyResult struct {
	Applied []string       // browsers with policy successfully applied
	Failed  []BrowserError // browsers that failed + reason
}

// BrowserError pairs a browser name with a failure reason.
type BrowserError struct {
	Name   string
	Reason string
}

// PolicyManager manages WebRTC browser policies.
type PolicyManager interface {
	ApplyPolicies(ctx context.Context) (*ApplyResult, error)
	RestorePolicies(ctx context.Context) error
}

// policyPersistedState is the JSON structure saved to disk for crash recovery.
type policyPersistedState struct {
	Browsers []browserSavedState `json:"browsers"`
}

// browserSavedState stores the original policy state for one browser.
type browserSavedState struct {
	Name       string        `json:"name"`
	Family     BrowserFamily `json:"family"`
	PolicyPath string        `json:"policy_path"`
	// OriginalValue stores the original value (Chromium string or Firefox file content).
	OriginalValue string `json:"original_value"`
	// HadOriginal indicates whether a value existed before Le Voile wrote it.
	HadOriginal bool `json:"had_original"`
	// OriginalValues stores multiple original values for Firefox registry prefs.
	// Key = pref name, value = original DWORD as string. Missing key = didn't exist.
	OriginalValues map[string]string `json:"original_values,omitempty"`
}

const policyStateFile = "browser-policies-original.json"

// policyStatePathOverride allows tests to redirect the state directory.
var policyStatePathOverride func() string

// policyStatePath returns the directory for persisted browser policy state.
func policyStatePath() string {
	if policyStatePathOverride != nil {
		return policyStatePathOverride()
	}
	if runtime.GOOS == "windows" {
		dir := os.Getenv("ProgramData")
		if dir == "" {
			dir = `C:\ProgramData`
		}
		return filepath.Join(dir, "LeVoile")
	}
	return "/var/lib/levoile"
}

// policyStateFilePath returns the full path to the persisted state file.
func policyStateFilePath() string {
	return filepath.Join(policyStatePath(), policyStateFile)
}

// persistStateIncremental writes the full state atomically (temp → rename).
func persistStateIncremental(state *policyPersistedState) error {
	dir := policyStatePath()
	if err := os.MkdirAll(dir, 0755); err != nil {
		return fmt.Errorf("browser: persist state: mkdir: %w", err)
	}

	data, err := json.Marshal(state)
	if err != nil {
		return fmt.Errorf("browser: persist state: marshal: %w", err)
	}

	tmp, err := os.CreateTemp(dir, "browser-policies-*.tmp")
	if err != nil {
		return fmt.Errorf("browser: persist state: create temp: %w", err)
	}
	tmpName := tmp.Name()

	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		os.Remove(tmpName)
		return fmt.Errorf("browser: persist state: write: %w", err)
	}
	if err := tmp.Close(); err != nil {
		os.Remove(tmpName)
		return fmt.Errorf("browser: persist state: close: %w", err)
	}
	finalPath := policyStateFilePath()
	// On Windows, os.Rename fails if target exists. Remove it first.
	if runtime.GOOS == "windows" {
		os.Remove(finalPath)
	}
	if err := os.Rename(tmpName, finalPath); err != nil {
		os.Remove(tmpName)
		return fmt.Errorf("browser: persist state: rename: %w", err)
	}
	return nil
}

// loadPersistedState reads the persisted state from disk or a temp orphan.
func loadPersistedState() (*policyPersistedState, error) {
	path := policyStateFilePath()
	data, err := os.ReadFile(path)
	if err != nil {
		// Try to find an orphan temp file.
		data, err = loadOrphanTemp()
		if err != nil {
			return nil, err
		}
	}

	var state policyPersistedState
	if err := json.Unmarshal(data, &state); err != nil {
		return nil, fmt.Errorf("browser: load state: unmarshal: %w", err)
	}
	return &state, nil
}

// loadOrphanTemp looks for a browser-policies-*.tmp file and returns its content.
func loadOrphanTemp() ([]byte, error) {
	dir := policyStatePath()
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, fmt.Errorf("browser: load orphan temp: %w", err)
	}
	for _, e := range entries {
		if strings.HasPrefix(e.Name(), "browser-policies-") && strings.HasSuffix(e.Name(), ".tmp") {
			data, err := os.ReadFile(filepath.Join(dir, e.Name()))
			if err == nil {
				return data, nil
			}
		}
	}
	return nil, fmt.Errorf("browser: no persisted state found")
}

// removePersistedState deletes the persisted state file.
func removePersistedState() {
	os.Remove(policyStateFilePath())
}

// cleanOrphanTemps removes any browser-policies-*.tmp files.
// If a temp exists but the final file doesn't, promote the temp.
func cleanOrphanTemps() {
	dir := policyStatePath()
	entries, err := os.ReadDir(dir)
	if err != nil {
		return
	}

	finalPath := policyStateFilePath()
	_, finalErr := os.Stat(finalPath)
	finalExists := finalErr == nil

	for _, e := range entries {
		if !strings.HasPrefix(e.Name(), "browser-policies-") || !strings.HasSuffix(e.Name(), ".tmp") {
			continue
		}
		tmpPath := filepath.Join(dir, e.Name())
		if !finalExists {
			// Promote first temp file to final.
			if err := os.Rename(tmpPath, finalPath); err == nil {
				finalExists = true
				continue
			}
		}
		os.Remove(tmpPath)
	}
}
