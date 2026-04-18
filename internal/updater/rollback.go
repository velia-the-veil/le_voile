package updater

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

const (
	rollbackStateFile   = "rollback_state.json"
	failedVersionFile   = "failed_version.txt"
	installRetriesFile  = "install_retries.txt"
)

// RollbackState tracks whether a rollback is possible after a recent installation.
type RollbackState struct {
	JustInstalled    bool   `json:"just_installed"`
	InstalledVersion string `json:"installed_version"`
}

// WriteRollbackState persists the rollback state to rollback_state.json atomically.
func WriteRollbackState(dir string, state *RollbackState) error {
	data, err := json.Marshal(state)
	if err != nil {
		return fmt.Errorf("updater: rollback state: marshal: %w", err)
	}

	path := filepath.Join(dir, rollbackStateFile)
	tmpPath := path + ".tmp"

	if err := os.WriteFile(tmpPath, data, 0o600); err != nil {
		return fmt.Errorf("updater: rollback state: write tmp: %w", err)
	}

	if err := renameWithRetry(tmpPath, path); err != nil {
		os.Remove(tmpPath)
		return fmt.Errorf("updater: rollback state: rename: %w", err)
	}

	return nil
}

// ReadRollbackState reads the rollback state from rollback_state.json.
// Returns nil if the file does not exist (not an error).
func ReadRollbackState(dir string) (*RollbackState, error) {
	path := filepath.Join(dir, rollbackStateFile)

	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("updater: rollback state: read: %w", err)
	}

	var state RollbackState
	if err := json.Unmarshal(data, &state); err != nil {
		return nil, fmt.Errorf("updater: rollback state: parse: %w", err)
	}

	return &state, nil
}

// ClearRollbackState removes rollback_state.json. Idempotent (no error if absent).
func ClearRollbackState(dir string) error {
	path := filepath.Join(dir, rollbackStateFile)
	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("updater: rollback state: remove: %w", err)
	}
	return nil
}

// WriteFailedVersion writes the failed version string to failed_version.txt.
func WriteFailedVersion(dir string, version string) error {
	path := filepath.Join(dir, failedVersionFile)
	tmpPath := path + ".tmp"

	if err := os.WriteFile(tmpPath, []byte(version), 0o600); err != nil {
		return fmt.Errorf("updater: failed version: write tmp: %w", err)
	}

	if err := renameWithRetry(tmpPath, path); err != nil {
		os.Remove(tmpPath)
		return fmt.Errorf("updater: failed version: rename: %w", err)
	}

	return nil
}

// ReadFailedVersion reads the failed version from failed_version.txt.
// Returns "" if the file does not exist (not an error).
func ReadFailedVersion(dir string) (string, error) {
	path := filepath.Join(dir, failedVersionFile)

	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return "", nil
		}
		return "", fmt.Errorf("updater: failed version: read: %w", err)
	}

	return strings.TrimSpace(string(data)), nil
}

// ClearFailedVersion removes failed_version.txt. Idempotent (no error if absent).
// Called when a NEW release (different version) is available.
func ClearFailedVersion(dir string) error {
	path := filepath.Join(dir, failedVersionFile)
	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("updater: failed version: remove: %w", err)
	}
	return nil
}

// ReadInstallRetries reads the install-retry counter for a staged update.
// Returns 0 if the file does not exist (not an error). The counter tracks how
// many times Install() has failed on the currently-staged payload so we can
// abandon it after a configurable cap rather than looping on every boot.
func ReadInstallRetries(dir string) (int, error) {
	path := filepath.Join(dir, installRetriesFile)

	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return 0, nil
		}
		return 0, fmt.Errorf("updater: install retries: read: %w", err)
	}

	s := strings.TrimSpace(string(data))
	if s == "" {
		return 0, nil
	}
	var n int
	if _, err := fmt.Sscanf(s, "%d", &n); err != nil {
		return 0, fmt.Errorf("updater: install retries: parse: %w", err)
	}
	return n, nil
}

// WriteInstallRetries persists the install-retry counter atomically.
func WriteInstallRetries(dir string, n int) error {
	path := filepath.Join(dir, installRetriesFile)
	tmpPath := path + ".tmp"

	if err := os.WriteFile(tmpPath, []byte(fmt.Sprintf("%d", n)), 0o600); err != nil {
		return fmt.Errorf("updater: install retries: write tmp: %w", err)
	}

	if err := renameWithRetry(tmpPath, path); err != nil {
		os.Remove(tmpPath)
		return fmt.Errorf("updater: install retries: rename: %w", err)
	}
	return nil
}

// ClearInstallRetries removes install_retries.txt. Idempotent. Called after a
// successful install or when the staged payload is abandoned.
func ClearInstallRetries(dir string) error {
	path := filepath.Join(dir, installRetriesFile)
	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("updater: install retries: remove: %w", err)
	}
	return nil
}
