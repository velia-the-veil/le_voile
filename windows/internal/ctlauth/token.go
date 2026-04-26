//go:build windows

// Package ctlauth manages the machine-local token used by levoile-ctl to
// authenticate privileged IPC actions against the service (Story 5.9).
//
// The token is a 32-byte random value persisted on disk in hex form. Both the
// service (which generates and verifies it) and the levoile-ctl binary (which
// reads it) point at the same file. File permissions / ACL restrict reads
// to root/Administrators so a non-privileged user-space process cannot
// impersonate a CLI request.
package ctlauth

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
)

// tokenLength is the number of random bytes generated. Hex-encoded the file
// holds 64 ASCII characters — short enough to argv if needed, long enough to
// resist brute force well past the lifetime of any single install.
const tokenLength = 32

// hexLength is the on-disk encoded size (2 hex chars per byte).
const hexLength = tokenLength * 2

// Errors returned by package functions.
var (
	// ErrTokenAbsent is returned by Load when the token file does not exist.
	// Distinct from a generic IO error so callers (levoile-ctl) can surface a
	// targeted "service not initialized — start it once first" message.
	ErrTokenAbsent = errors.New("ctlauth: token file absent")

	// ErrTokenMalformed is returned when the file exists but its contents are
	// not a valid hex-encoded 32-byte token. Indicates corruption or tampering.
	ErrTokenMalformed = errors.New("ctlauth: token file malformed")
)

// DefaultPath returns the conventional location of the ctl.token file:
//   - Linux:   /etc/levoile/ctl.token
//   - Windows: %ProgramData%\LeVoile\ctl.token (env-resolved, falls back to
//     C:\ProgramData\LeVoile if ProgramData is unset)
//   - other:   <os.TempDir>/levoile/ctl.token (development convenience)
//
// Returns an empty string only on platforms where no sensible default exists,
// which is treated by callers as "ctl auth disabled".
func DefaultPath() string {
	switch runtime.GOOS {
	case "linux":
		return "/etc/levoile/ctl.token"
	case "windows":
		programData := os.Getenv("ProgramData")
		if programData == "" {
			programData = `C:\ProgramData`
		}
		return filepath.Join(programData, "LeVoile", "ctl.token")
	default:
		return filepath.Join(os.TempDir(), "levoile", "ctl.token")
	}
}

// LoadOrCreate returns the token at path. If the file does not exist, a new
// 32-byte random token is generated, persisted with restrictive permissions,
// and returned. If the file exists, it is parsed and validated.
//
// On generation, the parent directory is created (perms 0700) if missing.
// File permissions are set to 0600 on POSIX. Windows files inherit the
// parent ACL — install scripts must restrict %ProgramData%\LeVoile ACLs to
// LocalSystem + Administrators (NSIS post-install step in Story 7.1).
//
// Returns the raw 32-byte token (hex-decoded). Caller may call Hex() on the
// result to re-encode.
func LoadOrCreate(path string) ([]byte, error) {
	if path == "" {
		return nil, errors.New("ctlauth: empty token path")
	}

	if data, err := Load(path); err == nil {
		return data, nil
	} else if !errors.Is(err, ErrTokenAbsent) {
		return nil, err
	}

	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return nil, fmt.Errorf("ctlauth: mkdir parent: %w", err)
	}

	raw := make([]byte, tokenLength)
	if _, err := rand.Read(raw); err != nil {
		return nil, fmt.Errorf("ctlauth: rand.Read: %w", err)
	}
	encoded := []byte(hex.EncodeToString(raw))

	if err := writeRestrictedFile(path, encoded); err != nil {
		return nil, err
	}
	return raw, nil
}

// Load reads and validates an existing token file. Returns ErrTokenAbsent if
// the file does not exist (so callers can distinguish bootstrap from corruption).
func Load(path string) ([]byte, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, ErrTokenAbsent
		}
		return nil, fmt.Errorf("ctlauth: read: %w", err)
	}
	// Trim whitespace so a manually edited file with a trailing newline still works.
	trimmed := trimASCII(data)
	if len(trimmed) != hexLength {
		return nil, fmt.Errorf("%w: expected %d hex chars, got %d", ErrTokenMalformed, hexLength, len(trimmed))
	}
	out := make([]byte, tokenLength)
	if _, err := hex.Decode(out, trimmed); err != nil {
		return nil, fmt.Errorf("%w: %v", ErrTokenMalformed, err)
	}
	return out, nil
}

// Hex returns the hex-encoded form of a raw token. Convenience for callers
// (notably levoile-ctl) that need to put it in JSON.
func Hex(raw []byte) string {
	return hex.EncodeToString(raw)
}

// trimASCII removes leading/trailing ASCII whitespace from a byte slice.
// Avoids strings.TrimSpace conversions for a tight, allocation-free trim.
func trimASCII(data []byte) []byte {
	start := 0
	end := len(data)
	for start < end && isWhitespace(data[start]) {
		start++
	}
	for end > start && isWhitespace(data[end-1]) {
		end--
	}
	return data[start:end]
}

func isWhitespace(b byte) bool {
	return b == ' ' || b == '\t' || b == '\n' || b == '\r'
}
