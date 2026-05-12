package config

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

// Sentinel errors returned by the integrity API. Callers distinguish
// "first run / migration" (ErrHMACAbsent) from "tampering detected"
// (ErrIntegrityMismatch) so the service only raises the UI alert on the
// latter — the former is the legitimate bootstrap path.
var (
	// ErrKeyAbsent is returned by LoadKey when the key file does not exist.
	// LoadOrCreateKey handles this case internally by generating a fresh key.
	ErrKeyAbsent = errors.New("config: integrity key absent")

	// ErrKeyMalformed is returned when the key file exists but is not the
	// expected 32-byte random payload.
	ErrKeyMalformed = errors.New("config: integrity key malformed")

	// ErrHMACAbsent is returned by Verify when the .hmac sidecar file does
	// not exist. Distinguished from mismatch so first-run / legacy-upgrade
	// paths can self-heal by calling Sign.
	ErrHMACAbsent = errors.New("config: integrity hmac absent")

	// ErrIntegrityMismatch is returned by Verify when the HMAC on disk does
	// NOT match a fresh recomputation over the config contents. Indicates
	// external tampering — service refuses to start normal operations.
	ErrIntegrityMismatch = errors.New("config: integrity mismatch")
)

// integrityKeyLen is the size of the machine-local HMAC key (32 bytes =
// 256 bits = HMAC-SHA256 optimal). Matches ctlauth's tokenLength.
const integrityKeyLen = 32

// hmacHexLen is the hex-encoded size of an HMAC-SHA256 output.
const hmacHexLen = sha256.Size * 2

// LoadOrCreateKey returns the machine-local HMAC key at keyPath. On first
// call it generates a cryptographically-random 32-byte key via crypto/rand,
// writes it to disk with restrictive permissions, and returns it. On
// subsequent calls it loads and validates the existing file.
//
// The parent directory is created with 0700 if absent. Callers must hold
// config.Mu if bootstrapping alongside config writes, though in practice
// key creation happens once before the IPC server starts.
func LoadOrCreateKey(keyPath string) ([]byte, error) {
	if keyPath == "" {
		return nil, errors.New("config: empty key path")
	}
	if data, err := loadKey(keyPath); err == nil {
		return data, nil
	} else if !errors.Is(err, ErrKeyAbsent) {
		return nil, err
	}

	if err := os.MkdirAll(filepath.Dir(keyPath), 0o700); err != nil {
		return nil, fmt.Errorf("config: integrity mkdir: %w", err)
	}

	key := make([]byte, integrityKeyLen)
	if _, err := rand.Read(key); err != nil {
		return nil, fmt.Errorf("config: integrity rand: %w", err)
	}

	tmp, err := os.CreateTemp(filepath.Dir(keyPath), "integrity-key-*.tmp")
	if err != nil {
		return nil, fmt.Errorf("config: integrity create temp: %w", err)
	}
	tmpName := tmp.Name()
	if _, err := tmp.Write(key); err != nil {
		tmp.Close()
		os.Remove(tmpName)
		return nil, fmt.Errorf("config: integrity write: %w", err)
	}
	if err := tmp.Close(); err != nil {
		os.Remove(tmpName)
		return nil, fmt.Errorf("config: integrity close: %w", err)
	}
	if err := os.Rename(tmpName, keyPath); err != nil {
		os.Remove(tmpName)
		return nil, fmt.Errorf("config: integrity rename: %w", err)
	}
	if err := applyRestrictedPerms(keyPath); err != nil {
		return nil, fmt.Errorf("config: integrity tighten perms: %w", err)
	}
	return key, nil
}

// loadKey reads and validates an existing key file. Returns ErrKeyAbsent if
// the file does not exist so LoadOrCreateKey can bootstrap.
func loadKey(keyPath string) ([]byte, error) {
	// #nosec G304 -- keyPath comes from IntegrityKeyPath() (internal derivation
	// from OS-standard config dirs), never from untrusted input.
	data, err := os.ReadFile(keyPath)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, ErrKeyAbsent
		}
		return nil, fmt.Errorf("config: integrity read key: %w", err)
	}
	if len(data) != integrityKeyLen {
		return nil, fmt.Errorf("%w: expected %d bytes, got %d", ErrKeyMalformed, integrityKeyLen, len(data))
	}
	return data, nil
}

// SignBytes computes HMAC-SHA256(key, contents) and writes the hex-encoded
// digest to configPath+".hmac" atomically with restrictive perms. Prefer
// this over Sign in the SaveAndSign hot path — it takes the encoded bytes
// as-written and eliminates the TOCTOU window where an attacker at the
// service's privilege level could overwrite configPath between Save's
// rename and Sign's os.ReadFile.
//
// The caller MUST hold config.Mu to serialize writers against concurrent
// writes (see config.go header comment).
func SignBytes(configPath string, contents, key []byte) error {
	if len(key) != integrityKeyLen {
		return fmt.Errorf("%w: SignBytes expects %d-byte key, got %d", ErrKeyMalformed, integrityKeyLen, len(key))
	}
	mac := hmac.New(sha256.New, key)
	mac.Write(contents)
	digestHex := hex.EncodeToString(mac.Sum(nil))

	hmacPath := configPath + ".hmac"
	dir := filepath.Dir(hmacPath)
	tmp, err := os.CreateTemp(dir, "integrity-hmac-*.tmp")
	if err != nil {
		return fmt.Errorf("config: integrity create temp: %w", err)
	}
	tmpName := tmp.Name()
	if _, err := tmp.WriteString(digestHex); err != nil {
		tmp.Close()
		os.Remove(tmpName)
		return fmt.Errorf("config: integrity write hmac: %w", err)
	}
	if err := tmp.Close(); err != nil {
		os.Remove(tmpName)
		return fmt.Errorf("config: integrity close hmac: %w", err)
	}
	if err := os.Rename(tmpName, hmacPath); err != nil {
		os.Remove(tmpName)
		return fmt.Errorf("config: integrity rename hmac: %w", err)
	}
	if err := applyRestrictedPerms(hmacPath); err != nil {
		return fmt.Errorf("config: integrity tighten hmac perms: %w", err)
	}
	return nil
}

// Sign is the legacy-compat wrapper for call-sites that need to sign
// whatever is currently on disk (migration path from a pre-7.5 install
// where the encoded bytes are not available). All hot-path writers MUST
// use SaveAndSign / SignBytes instead to avoid TOCTOU on configPath.
//
// The caller MUST hold config.Mu. configPath is read via os.ReadFile,
// protected by 0600 / DACL perms applied by prior Save calls (defense in
// depth — the only remaining attacker is one already at the service's
// privilege level, which is out of the NFR9j threat model).
func Sign(configPath string, key []byte) error {
	// #nosec G304 -- configPath is supplied by the caller, typically resolved
	// through config.DiscoverPath (not user-controlled). Reading here is only
	// done on the legacy migration path; the hot path uses SignBytes.
	contents, err := os.ReadFile(configPath)
	if err != nil {
		return fmt.Errorf("config: integrity read config: %w", err)
	}
	return SignBytes(configPath, contents, key)
}

// Verify recomputes HMAC-SHA256(key, configContents) and compares it in
// constant time against the digest persisted at configPath+".hmac". Returns
// ErrHMACAbsent when the sidecar file does not exist (legitimate first-run
// or legacy upgrade), ErrIntegrityMismatch when values diverge (tampering),
// or nil when the integrity check succeeds.
func Verify(configPath string, key []byte) error {
	if len(key) != integrityKeyLen {
		return fmt.Errorf("%w: Verify expects %d-byte key, got %d", ErrKeyMalformed, integrityKeyLen, len(key))
	}
	hmacPath := configPath + ".hmac"
	// #nosec G304 -- hmacPath is derived from configPath (DiscoverPath output),
	// not attacker-controlled.
	stored, err := os.ReadFile(hmacPath)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return ErrHMACAbsent
		}
		return fmt.Errorf("config: integrity read hmac: %w", err)
	}
	if len(stored) != hmacHexLen {
		return fmt.Errorf("%w: hmac file length %d (expected %d)", ErrIntegrityMismatch, len(stored), hmacHexLen)
	}
	// #nosec G304 -- configPath resolved by config.DiscoverPath, not untrusted.
	contents, err := os.ReadFile(configPath)
	if err != nil {
		return fmt.Errorf("config: integrity read config: %w", err)
	}
	mac := hmac.New(sha256.New, key)
	mac.Write(contents)
	expectedHex := []byte(hex.EncodeToString(mac.Sum(nil)))
	if subtle.ConstantTimeCompare(stored, expectedHex) != 1 {
		return ErrIntegrityMismatch
	}
	return nil
}
