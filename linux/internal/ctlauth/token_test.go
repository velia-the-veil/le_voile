//go:build linux

package ctlauth

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// LoadOrCreate generates a fresh token on first call and returns the same
// value on subsequent calls (idempotent persistence).
func TestLoadOrCreate_Idempotent(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "ctl.token")

	first, err := LoadOrCreate(path)
	if err != nil {
		t.Fatalf("first LoadOrCreate: %v", err)
	}
	if len(first) != tokenLength {
		t.Errorf("token len = %d, want %d", len(first), tokenLength)
	}

	second, err := LoadOrCreate(path)
	if err != nil {
		t.Fatalf("second LoadOrCreate: %v", err)
	}
	if !bytes.Equal(first, second) {
		t.Error("LoadOrCreate must return the same token across calls")
	}
}

// LoadOrCreate persists hex-encoded data to disk.
func TestLoadOrCreate_PersistsHex(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "ctl.token")

	if _, err := LoadOrCreate(path); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(raw) != hexLength {
		t.Errorf("on-disk len = %d, want %d", len(raw), hexLength)
	}
	for _, b := range raw {
		if !((b >= '0' && b <= '9') || (b >= 'a' && b <= 'f')) {
			t.Errorf("non-hex byte 0x%x in token file", b)
			break
		}
	}
}

// On Linux the token file is 0640 (owner rw, group r, no world) — see
// perms_unix.go header: service user `levoile` writes, desktop UI user reads
// via group membership. /etc/levoile/ itself is 2770 root:levoile so "other"
// cannot traverse anyway.
func TestLoadOrCreate_PermsRestricted(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("permission bits are not POSIX on Windows; ACL inherits from parent dir")
	}
	dir := t.TempDir()
	path := filepath.Join(dir, "ctl.token")

	if _, err := LoadOrCreate(path); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if mode := info.Mode().Perm(); mode != 0o640 {
		t.Errorf("file mode = %o, want 0640", mode)
	}
}

// Load returns ErrTokenAbsent when the file does not exist.
func TestLoad_AbsentReturnsSentinel(t *testing.T) {
	_, err := Load(filepath.Join(t.TempDir(), "missing.token"))
	if !errors.Is(err, ErrTokenAbsent) {
		t.Errorf("err = %v, want ErrTokenAbsent", err)
	}
}

// Load tolerates a trailing newline (manual edits are common).
func TestLoad_TrimsTrailingNewline(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "ctl.token")

	tok, err := LoadOrCreate(path)
	if err != nil {
		t.Fatal(err)
	}
	hex := Hex(tok)
	if err := os.WriteFile(path, []byte(hex+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	loaded, err := Load(path)
	if err != nil {
		t.Fatalf("Load with trailing newline: %v", err)
	}
	if !bytes.Equal(loaded, tok) {
		t.Error("Load did not return the original token after re-write")
	}
}

// Load reports ErrTokenMalformed for invalid contents.
func TestLoad_MalformedRejected(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "ctl.token")
	if err := os.WriteFile(path, []byte("not hex at all"), 0o600); err != nil {
		t.Fatal(err)
	}
	_, err := Load(path)
	if !errors.Is(err, ErrTokenMalformed) {
		t.Errorf("err = %v, want ErrTokenMalformed", err)
	}
}

// DefaultPath returns a non-empty conventional location for the running OS.
func TestDefaultPath_NonEmpty(t *testing.T) {
	p := DefaultPath()
	if p == "" {
		t.Fatal("DefaultPath returned empty")
	}
	switch runtime.GOOS {
	case "linux":
		if p != "/etc/levoile/ctl.token" {
			t.Errorf("Linux DefaultPath = %q, want /etc/levoile/ctl.token", p)
		}
	case "windows":
		if !strings.Contains(strings.ToLower(p), `levoile\ctl.token`) {
			t.Errorf("Windows DefaultPath = %q, want path containing levoile\\ctl.token", p)
		}
	}
}
