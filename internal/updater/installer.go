package updater

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"
)

var (
	// ErrNoStagedUpdate indicates no staged update was found.
	ErrNoStagedUpdate = errors.New("updater: install: no staged update")
	// ErrReadOnlyTarget indicates the target executable is on a read-only filesystem.
	ErrReadOnlyTarget = errors.New("updater: install: target is read-only")
	// ErrBackupFailed indicates the backup of the current executable failed.
	ErrBackupFailed = errors.New("updater: install: backup failed")
	// ErrPackageManaged indicates the binary is installed by a system package
	// manager (dpkg/rpm/pacman). Auto-update is skipped to avoid conflicts with
	// the package manager's own update mechanism.
	ErrPackageManaged = errors.New("updater: binary is package-managed, auto-update disabled")
	// ErrDowngradeRejected is returned when a candidate release is older than
	// the persisted max-seen-version. Defends against a compromised release key
	// being used to force clients back to a vulnerable prior version.
	ErrDowngradeRejected = errors.New("updater: candidate release is older than max-seen-version")
)

const (
	stagedVersionFile = "staged_version.txt"
	// renameRetries is the number of retries for os.Rename on Windows (antivirus lock).
	renameRetries    = 3
	renameRetryDelay = 500 * time.Millisecond
)

// Installer handles replacing the current executable with a staged update.
type Installer struct {
	stagingDir     string
	executablePath string
	verifier       *Verifier
}

// NewInstaller creates an Installer for the given staging directory.
// It resolves the current executable path and follows symlinks.
func NewInstaller(stagingDir string, verifier *Verifier) (*Installer, error) {
	exePath, err := os.Executable()
	if err != nil {
		return nil, fmt.Errorf("updater: install: resolve executable: %w", err)
	}
	exePath, err = filepath.EvalSymlinks(exePath)
	if err != nil {
		return nil, fmt.Errorf("updater: install: eval symlinks: %w", err)
	}
	return &Installer{
		stagingDir:     stagingDir,
		executablePath: exePath,
		verifier:       verifier,
	}, nil
}

// NewInstallerWithPath creates an Installer with an explicit executable path (for testing).
func NewInstallerWithPath(stagingDir, executablePath string, verifier *Verifier) *Installer {
	return &Installer{
		stagingDir:     stagingDir,
		executablePath: executablePath,
		verifier:       verifier,
	}
}

// ExecutablePath returns the resolved path to the current executable.
func (inst *Installer) ExecutablePath() string {
	return inst.executablePath
}

// IsPackageManaged reports whether the binary appears to be installed by a
// system package manager (dpkg/rpm/pacman/Homebrew). Heuristic on Linux/Darwin:
//   - /usr/bin, /usr/local/bin, /usr/sbin     → dpkg/rpm/pacman
//   - /opt/homebrew/bin                        → macOS Apple Silicon brew
//   - /home/linuxbrew/.linuxbrew/bin           → Linuxbrew
//
// On Windows always returns false (no system package manager contract; NSIS
// installer is handled by the updater's normal swap path).
func (inst *Installer) IsPackageManaged() bool {
	return isPackageManagedPath(inst.executablePath)
}

// packageManagedPrefixes is the set of path prefixes treated as "installed
// by a system package manager" on Unix. Kept as a package-level var so tests
// and downstream callers can reference the same source of truth.
var packageManagedPrefixes = []string{
	"/usr/bin/",
	"/usr/local/bin/",
	"/usr/sbin/",
	"/opt/homebrew/bin/",           // Homebrew (macOS Apple Silicon default)
	"/home/linuxbrew/.linuxbrew/bin/", // Linuxbrew (Linux)
}

// isPackageManagedPath is the pure path heuristic, factored out for testing.
func isPackageManagedPath(exePath string) bool {
	if runtime.GOOS == "windows" {
		return false
	}
	// Normalize to forward-slash so the prefix check is stable across platforms
	// during tests that feed synthetic paths.
	p := filepath.ToSlash(exePath)
	for _, prefix := range packageManagedPrefixes {
		if strings.HasPrefix(p, prefix) {
			return true
		}
	}
	return false
}

// HasStagedUpdate checks if a staged binary exists in the staging directory.
// Returns nil if no staged update is found (not an error).
func (inst *Installer) HasStagedUpdate() (*StagedUpdate, error) {
	binaryName := fmt.Sprintf("le_voile_%s_%s", runtime.GOOS, runtime.GOARCH)
	if runtime.GOOS == "windows" {
		binaryName += ".exe"
	}

	binaryPath := filepath.Join(inst.stagingDir, binaryName)
	if _, err := os.Stat(binaryPath); err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("updater: install: stat staged binary: %w", err)
	}

	// Read staged version
	versionPath := filepath.Join(inst.stagingDir, stagedVersionFile)
	versionData, err := os.ReadFile(versionPath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil // No version file means incomplete staging
		}
		return nil, fmt.Errorf("updater: install: read staged version: %w", err)
	}

	version := strings.TrimSpace(string(versionData))
	if version == "" {
		return nil, nil
	}

	checksumPath := filepath.Join(inst.stagingDir, "checksums.txt")
	signaturePath := filepath.Join(inst.stagingDir, "checksums.txt.sig")

	return &StagedUpdate{
		BinaryPath:    binaryPath,
		ChecksumPath:  checksumPath,
		SignaturePath: signaturePath,
		Version:       version,
		VersionFile:   versionPath,
	}, nil
}

// Install performs the staged update installation sequence:
// 1. Re-verify integrity (SHA256 + Ed25519)
// 2. Backup current executable to .bak
// 3. Atomic copy staged binary to executable path
// 4. Verify new binary is accessible
// 5. Clean up staging directory
func (inst *Installer) Install(ctx context.Context, staged *StagedUpdate) error {
	if staged == nil {
		return ErrNoStagedUpdate
	}

	// Check context before starting
	if err := ctx.Err(); err != nil {
		return fmt.Errorf("updater: install: %w", err)
	}

	// Check target is writable
	if err := inst.checkWritable(); err != nil {
		return err
	}

	// 1. Re-verify integrity (mandatory — never install without verification)
	if inst.verifier == nil {
		return fmt.Errorf("updater: install: verifier is required")
	}
	if err := inst.verifier.VerifyChecksum(staged.BinaryPath, staged.ChecksumPath); err != nil {
		return fmt.Errorf("updater: install: re-verify: %w", err)
	}
	if err := inst.verifier.VerifySignature(staged.ChecksumPath, staged.SignaturePath); err != nil {
		return fmt.Errorf("updater: install: re-verify: %w", err)
	}

	// 2. Backup current executable
	backupPath := inst.executablePath + ".bak"
	if err := atomicCopyFile(inst.executablePath, backupPath); err != nil {
		return fmt.Errorf("%w: %v", ErrBackupFailed, err)
	}

	// 3. Atomic copy staged binary to executable path
	if err := atomicCopyFile(staged.BinaryPath, inst.executablePath); err != nil {
		// Restore backup on failure (with retry for Windows antivirus locks)
		restoreErr := renameWithRetry(backupPath, inst.executablePath)
		if restoreErr != nil {
			return fmt.Errorf("updater: install: copy failed: %v; restore also failed: %w", err, restoreErr)
		}
		return fmt.Errorf("updater: install: copy staged binary: %w", err)
	}

	// 4. Verify new binary is accessible, non-empty, and executable
	info, err := os.Stat(inst.executablePath)
	if err != nil {
		renameWithRetry(backupPath, inst.executablePath)
		return fmt.Errorf("updater: install: verify new binary: %w", err)
	}
	if info.Size() == 0 {
		renameWithRetry(backupPath, inst.executablePath)
		return fmt.Errorf("updater: install: new binary is empty")
	}
	// Ensure executable permission on Unix (atomicCopyFile preserves mode,
	// but verify in case the staged binary was created without +x).
	if runtime.GOOS != "windows" && info.Mode()&0o111 == 0 {
		if chmodErr := os.Chmod(inst.executablePath, info.Mode()|0o755); chmodErr != nil {
			renameWithRetry(backupPath, inst.executablePath)
			return fmt.Errorf("updater: install: set executable permission: %w", chmodErr)
		}
	}

	// 5. Clean up staging directory
	inst.cleanStaging(staged)

	// 6. Commit anti-downgrade baseline. Best-effort: a failure to write the
	// max-seen marker must not roll back a successful install — at worst, a
	// future check re-seeds from CurrentVersion() and no downgrade slips
	// through because the running binary IS the new version by the time the
	// next cycle runs.
	if err := WriteMaxSeenVersion(inst.stagingDir, staged.Version); err != nil {
		// Logging is handled by the updater cycle caller; swallow here so
		// install itself is reported successful.
		_ = err
	}

	return nil
}

// Rollback restores the backup executable if it exists.
func (inst *Installer) Rollback() error {
	backupPath := inst.executablePath + ".bak"
	if _, err := os.Stat(backupPath); err != nil {
		if os.IsNotExist(err) {
			return fmt.Errorf("updater: rollback: no backup found")
		}
		return fmt.Errorf("updater: rollback: %w", err)
	}

	if err := renameWithRetry(backupPath, inst.executablePath); err != nil {
		return fmt.Errorf("updater: rollback: %w", err)
	}
	return nil
}

// CleanupBackup removes the .bak file after successful startup.
func (inst *Installer) CleanupBackup() error {
	backupPath := inst.executablePath + ".bak"
	if err := os.Remove(backupPath); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("updater: cleanup backup: %w", err)
	}
	return nil
}

// checkWritable verifies the target directory is writable.
func (inst *Installer) checkWritable() error {
	dir := filepath.Dir(inst.executablePath)
	testFile := filepath.Join(dir, ".le_voile_write_test")
	f, err := os.Create(testFile)
	if err != nil {
		return ErrReadOnlyTarget
	}
	f.Close()
	os.Remove(testFile)
	return nil
}

// cleanStaging removes all staged files.
func (inst *Installer) cleanStaging(staged *StagedUpdate) {
	os.Remove(staged.BinaryPath)
	os.Remove(staged.ChecksumPath)
	os.Remove(staged.SignaturePath)
	if staged.VersionFile != "" {
		os.Remove(staged.VersionFile)
	} else {
		os.Remove(filepath.Join(inst.stagingDir, stagedVersionFile))
	}
}

// writeStagedVersion writes the staged version to staged_version.txt atomically.
func writeStagedVersion(stagingDir, version string) error {
	path := filepath.Join(stagingDir, stagedVersionFile)
	tmpPath := path + ".tmp"

	if err := os.WriteFile(tmpPath, []byte(version), 0o600); err != nil {
		return fmt.Errorf("updater: write staged version: write tmp: %w", err)
	}

	if err := renameWithRetry(tmpPath, path); err != nil {
		os.Remove(tmpPath)
		return fmt.Errorf("updater: write staged version: rename: %w", err)
	}

	return nil
}

// renameWithRetry renames src to dst with retry for Windows antivirus locks.
func renameWithRetry(src, dst string) error {
	var err error
	for i := 0; i < renameRetries; i++ {
		err = os.Rename(src, dst)
		if err == nil {
			return nil
		}
		if i < renameRetries-1 {
			time.Sleep(renameRetryDelay)
		}
	}
	return err
}

// atomicCopyFile copies src to dst using a temporary file and rename.
func atomicCopyFile(src, dst string) error {
	tmpDst := dst + ".tmp"

	srcFile, err := os.Open(src)
	if err != nil {
		return fmt.Errorf("open source: %w", err)
	}
	defer srcFile.Close()

	srcInfo, err := srcFile.Stat()
	if err != nil {
		return fmt.Errorf("stat source: %w", err)
	}

	dstFile, err := os.OpenFile(tmpDst, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, srcInfo.Mode()|0o755)
	if err != nil {
		return fmt.Errorf("create temp: %w", err)
	}

	if _, err := io.Copy(dstFile, srcFile); err != nil {
		dstFile.Close()
		os.Remove(tmpDst)
		return fmt.Errorf("copy: %w", err)
	}

	if err := dstFile.Close(); err != nil {
		os.Remove(tmpDst)
		return fmt.Errorf("close temp: %w", err)
	}

	// Rename with retry for Windows (antivirus may temporarily lock the file)
	if err := renameWithRetry(tmpDst, dst); err != nil {
		os.Remove(tmpDst)
		return fmt.Errorf("rename: %w", err)
	}

	return nil
}
