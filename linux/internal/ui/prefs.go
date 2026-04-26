//go:build linux

package ui

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
)

// UIPrefs holds frontend-only preferences that must survive app restarts.
// Stored server-side because the UI HTTP server binds to 127.0.0.1:0 (dynamic
// port) — localStorage is origin-scoped and would be lost on every relaunch.
type UIPrefs struct {
	// QuitPromptEnabled controls whether ✕ shows a confirmation modal.
	// Defaults to true; set to false when the user checks "Ne plus montrer".
	QuitPromptEnabled bool `json:"quit_prompt_enabled"`
}

// defaultUIPrefs returns the factory defaults.
func defaultUIPrefs() UIPrefs {
	return UIPrefs{QuitPromptEnabled: true}
}

// prefsPath returns the on-disk location of ui-prefs.json.
// Windows: %APPDATA%\LeVoile\ui-prefs.json
// Linux:   $XDG_CONFIG_HOME/levoile/ui-prefs.json (falls back to ~/.config/...)
func prefsPath() (string, error) {
	dir, err := os.UserConfigDir()
	if err != nil {
		return "", fmt.Errorf("ui: locate config dir: %w", err)
	}
	return filepath.Join(dir, "LeVoile", "ui-prefs.json"), nil
}

// PrefsStore serializes reads and writes to the prefs file.
type PrefsStore struct {
	mu   sync.Mutex
	path string // overridable for tests; empty = use prefsPath()
}

// NewPrefsStore returns a store that reads/writes at the default path.
func NewPrefsStore() *PrefsStore {
	return &PrefsStore{}
}

// resolvePath returns the explicit path if set, otherwise the default.
func (s *PrefsStore) resolvePath() (string, error) {
	if s.path != "" {
		return s.path, nil
	}
	return prefsPath()
}

// Load reads prefs from disk. Missing file returns defaults with no error.
func (s *PrefsStore) Load() (UIPrefs, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	path, err := s.resolvePath()
	if err != nil {
		return defaultUIPrefs(), err
	}
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return defaultUIPrefs(), nil
		}
		return defaultUIPrefs(), fmt.Errorf("ui: read prefs: %w", err)
	}
	// Pre-populate with defaults so a partial file still yields a valid struct.
	prefs := defaultUIPrefs()
	if err := json.Unmarshal(data, &prefs); err != nil {
		return defaultUIPrefs(), fmt.Errorf("ui: parse prefs: %w", err)
	}
	return prefs, nil
}

// Save writes prefs atomically (temp file + rename) so a crash mid-write can
// never leave a truncated JSON on disk.
func (s *PrefsStore) Save(prefs UIPrefs) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	path, err := s.resolvePath()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return fmt.Errorf("ui: create prefs dir: %w", err)
	}
	data, err := json.MarshalIndent(prefs, "", "  ")
	if err != nil {
		return fmt.Errorf("ui: marshal prefs: %w", err)
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o600); err != nil {
		return fmt.Errorf("ui: write prefs tmp: %w", err)
	}
	if err := os.Rename(tmp, path); err != nil {
		_ = os.Remove(tmp)
		return fmt.Errorf("ui: rename prefs: %w", err)
	}
	return nil
}
