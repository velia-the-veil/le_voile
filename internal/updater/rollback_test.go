package updater

import (
	"os"
	"path/filepath"
	"testing"
)

func TestRollbackState_WriteRead(t *testing.T) {
	dir := t.TempDir()

	state := &RollbackState{
		JustInstalled:    true,
		InstalledVersion: "2.1.0",
	}

	if err := WriteRollbackState(dir, state); err != nil {
		t.Fatalf("WriteRollbackState: %v", err)
	}

	got, err := ReadRollbackState(dir)
	if err != nil {
		t.Fatalf("ReadRollbackState: %v", err)
	}
	if got == nil {
		t.Fatal("expected non-nil state")
	}
	if !got.JustInstalled {
		t.Error("JustInstalled = false, want true")
	}
	if got.InstalledVersion != "2.1.0" {
		t.Errorf("InstalledVersion = %q, want %q", got.InstalledVersion, "2.1.0")
	}
}

func TestRollbackState_ReadAbsent(t *testing.T) {
	dir := t.TempDir()

	got, err := ReadRollbackState(dir)
	if err != nil {
		t.Fatalf("ReadRollbackState: %v", err)
	}
	if got != nil {
		t.Errorf("expected nil, got %+v", got)
	}
}

func TestRollbackState_Clear(t *testing.T) {
	dir := t.TempDir()

	state := &RollbackState{JustInstalled: true, InstalledVersion: "1.0.0"}
	if err := WriteRollbackState(dir, state); err != nil {
		t.Fatalf("WriteRollbackState: %v", err)
	}

	if err := ClearRollbackState(dir); err != nil {
		t.Fatalf("ClearRollbackState: %v", err)
	}

	got, err := ReadRollbackState(dir)
	if err != nil {
		t.Fatalf("ReadRollbackState after clear: %v", err)
	}
	if got != nil {
		t.Errorf("expected nil after clear, got %+v", got)
	}
}

func TestRollbackState_ClearAbsent(t *testing.T) {
	dir := t.TempDir()

	if err := ClearRollbackState(dir); err != nil {
		t.Errorf("ClearRollbackState on absent file should not error: %v", err)
	}
}

func TestRollbackState_AtomicWrite(t *testing.T) {
	dir := t.TempDir()

	state := &RollbackState{JustInstalled: true, InstalledVersion: "3.0.0"}
	if err := WriteRollbackState(dir, state); err != nil {
		t.Fatalf("WriteRollbackState: %v", err)
	}

	// Verify no .tmp file remains
	tmpPath := filepath.Join(dir, rollbackStateFile+".tmp")
	if _, err := os.Stat(tmpPath); !os.IsNotExist(err) {
		t.Error("tmp file should not exist after successful write")
	}

	// Verify the actual file contains valid JSON
	data, err := os.ReadFile(filepath.Join(dir, rollbackStateFile))
	if err != nil {
		t.Fatalf("read file: %v", err)
	}
	if len(data) == 0 {
		t.Error("file should not be empty")
	}
}

func TestFailedVersion_WriteRead(t *testing.T) {
	dir := t.TempDir()

	if err := WriteFailedVersion(dir, "2.1.0"); err != nil {
		t.Fatalf("WriteFailedVersion: %v", err)
	}

	got, err := ReadFailedVersion(dir)
	if err != nil {
		t.Fatalf("ReadFailedVersion: %v", err)
	}
	if got != "2.1.0" {
		t.Errorf("got %q, want %q", got, "2.1.0")
	}
}

func TestFailedVersion_ReadAbsent(t *testing.T) {
	dir := t.TempDir()

	got, err := ReadFailedVersion(dir)
	if err != nil {
		t.Fatalf("ReadFailedVersion: %v", err)
	}
	if got != "" {
		t.Errorf("expected empty string, got %q", got)
	}
}

func TestFailedVersion_Clear(t *testing.T) {
	dir := t.TempDir()

	if err := WriteFailedVersion(dir, "2.1.0"); err != nil {
		t.Fatalf("WriteFailedVersion: %v", err)
	}

	if err := ClearFailedVersion(dir); err != nil {
		t.Fatalf("ClearFailedVersion: %v", err)
	}

	got, err := ReadFailedVersion(dir)
	if err != nil {
		t.Fatalf("ReadFailedVersion after clear: %v", err)
	}
	if got != "" {
		t.Errorf("expected empty string after clear, got %q", got)
	}
}

func TestFailedVersion_ClearAbsent(t *testing.T) {
	dir := t.TempDir()

	if err := ClearFailedVersion(dir); err != nil {
		t.Errorf("ClearFailedVersion on absent file should not error: %v", err)
	}
}

func TestFailedVersion_AtomicWrite(t *testing.T) {
	dir := t.TempDir()

	if err := WriteFailedVersion(dir, "4.0.0"); err != nil {
		t.Fatalf("WriteFailedVersion: %v", err)
	}

	// Verify no .tmp file remains
	tmpPath := filepath.Join(dir, failedVersionFile+".tmp")
	if _, err := os.Stat(tmpPath); !os.IsNotExist(err) {
		t.Error("tmp file should not exist after successful write")
	}
}
