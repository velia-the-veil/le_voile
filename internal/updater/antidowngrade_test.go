package updater

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

// TestMaxSeenVersion_ReadWrite exercises the persist-and-reload roundtrip for
// the anti-downgrade marker introduced by fix C1. The file must land at
// max_seen_version.txt with mode 0600 and be recoverable byte-for-byte.
func TestMaxSeenVersion_ReadWrite(t *testing.T) {
	dir := t.TempDir()
	if err := WriteMaxSeenVersion(dir, "1.4.2"); err != nil {
		t.Fatalf("WriteMaxSeenVersion: %v", err)
	}

	got, err := ReadMaxSeenVersion(dir)
	if err != nil {
		t.Fatalf("ReadMaxSeenVersion: %v", err)
	}
	if got != "1.4.2" {
		t.Errorf("ReadMaxSeenVersion = %q, want %q", got, "1.4.2")
	}

	// Mode must be restrictive (0600) — the marker is security-relevant and
	// world-readability would let a same-user attacker learn the downgrade
	// baseline and avoid triggering it.
	//
	// Skipped on Windows: NTFS perms are governed by DACLs, not POSIX mode
	// bits, and os.Chmod there only approximates read/write/execute.
	if runtime.GOOS == "windows" {
		return
	}
	info, err := os.Stat(filepath.Join(dir, maxSeenVersionFile))
	if err != nil {
		t.Fatalf("stat max-seen: %v", err)
	}
	if mode := info.Mode().Perm(); mode != 0o600 {
		t.Errorf("max-seen-version mode = %v, want 0o600", mode)
	}
}

// TestMaxSeenVersion_Missing returns an empty string with no error when the
// marker has never been written — first-run behaviour.
func TestMaxSeenVersion_Missing(t *testing.T) {
	dir := t.TempDir()
	got, err := ReadMaxSeenVersion(dir)
	if err != nil {
		t.Fatalf("ReadMaxSeenVersion on empty dir: %v", err)
	}
	if got != "" {
		t.Errorf("ReadMaxSeenVersion on empty dir = %q, want empty", got)
	}
}

// TestDowngradeDetection verifies that compareVersions correctly flags older
// candidates as downgrades. This is the decision logic wired into
// CheckAndDownload to return ErrDowngradeRejected.
func TestDowngradeDetection(t *testing.T) {
	tests := []struct {
		maxSeen   string
		candidate string
		isDowngrade bool
	}{
		{"1.4.0", "1.3.9", true},
		{"1.4.0", "1.4.0", false},
		{"1.4.0", "1.4.1", false},
		{"2.0.0", "1.99.99", true},
		{"1.4.0", "1.4.0-beta.1", true}, // stable > prerelease of same nums
		{"1.4.0-beta.1", "1.4.0", false},
	}

	for _, tt := range tests {
		got := compareVersions(tt.candidate, tt.maxSeen)
		isDowngrade := got < 0
		if isDowngrade != tt.isDowngrade {
			t.Errorf("candidate=%s max_seen=%s: got compare=%d (downgrade=%v), want downgrade=%v",
				tt.candidate, tt.maxSeen, got, isDowngrade, tt.isDowngrade)
		}
	}
}
