//go:build !windows

package ui

import (
	"os"
	"path/filepath"
	"testing"
)

// TestPrefsStore_DirPermissions confirms fix H3: the directory that holds
// ui-prefs.json is created with 0o700 so a same-user attacker or another
// local account cannot even list its contents.
//
// POSIX-only: NTFS DACLs on Windows are governed by a separate test matrix.
func TestPrefsStore_DirPermissions(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "LeVoile", "ui-prefs.json")
	s := &PrefsStore{path: path}

	if err := s.Save(UIPrefs{QuitPromptEnabled: false}); err != nil {
		t.Fatalf("Save: %v", err)
	}

	info, err := os.Stat(filepath.Dir(path))
	if err != nil {
		t.Fatalf("stat prefs dir: %v", err)
	}
	if mode := info.Mode().Perm(); mode != 0o700 {
		t.Errorf("prefs dir mode = %v, want 0o700", mode)
	}
}
