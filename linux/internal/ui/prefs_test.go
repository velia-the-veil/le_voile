//go:build linux

package ui

import (
	"os"
	"path/filepath"
	"testing"
)

func TestPrefsStore_LoadMissingReturnsDefaults(t *testing.T) {
	dir := t.TempDir()
	store := &PrefsStore{path: filepath.Join(dir, "ui-prefs.json")}

	prefs, err := store.Load()
	if err != nil {
		t.Fatalf("Load with missing file should not error: %v", err)
	}
	if !prefs.QuitPromptEnabled {
		t.Error("QuitPromptEnabled default = true, got false")
	}
}

func TestPrefsStore_SaveThenLoadRoundtrip(t *testing.T) {
	dir := t.TempDir()
	store := &PrefsStore{path: filepath.Join(dir, "ui-prefs.json")}

	if err := store.Save(UIPrefs{QuitPromptEnabled: false}); err != nil {
		t.Fatalf("Save: %v", err)
	}
	prefs, err := store.Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if prefs.QuitPromptEnabled {
		t.Error("QuitPromptEnabled should be false after Save(false)")
	}
}

func TestPrefsStore_SaveCreatesMissingDir(t *testing.T) {
	dir := t.TempDir()
	nested := filepath.Join(dir, "nested", "deep", "ui-prefs.json")
	store := &PrefsStore{path: nested}

	if err := store.Save(UIPrefs{QuitPromptEnabled: false}); err != nil {
		t.Fatalf("Save should create parent dirs: %v", err)
	}
	if _, err := os.Stat(nested); err != nil {
		t.Errorf("prefs file not created: %v", err)
	}
}

func TestPrefsStore_SaveAtomic(t *testing.T) {
	// After a successful Save, no *.tmp file must remain — rename(2) is atomic
	// and the helper cleans up on success.
	dir := t.TempDir()
	path := filepath.Join(dir, "ui-prefs.json")
	store := &PrefsStore{path: path}

	if err := store.Save(UIPrefs{QuitPromptEnabled: false}); err != nil {
		t.Fatalf("Save: %v", err)
	}
	if _, err := os.Stat(path + ".tmp"); !os.IsNotExist(err) {
		t.Errorf("expected no .tmp leftover, stat err = %v", err)
	}
}

func TestPrefsStore_LoadCorruptReturnsDefaults(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "ui-prefs.json")
	if err := os.WriteFile(path, []byte("{not json"), 0o600); err != nil {
		t.Fatalf("seed corrupt file: %v", err)
	}
	store := &PrefsStore{path: path}

	prefs, err := store.Load()
	if err == nil {
		t.Error("expected parse error on corrupt JSON")
	}
	if !prefs.QuitPromptEnabled {
		t.Error("corrupt file should still yield safe defaults")
	}
}
