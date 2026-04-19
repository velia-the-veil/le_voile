package updater

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/velia-the-veil/le_voile/internal/crypto"
)

// TestInstaller_IsPackageManaged verifies the heuristic used to decide whether
// the binary was installed by a system package manager (dpkg/rpm/pacman).
// The heuristic is pure-path — it does not touch the filesystem — so we feed
// synthetic paths directly into isPackageManagedPath.
func TestInstaller_IsPackageManaged(t *testing.T) {
	cases := []struct {
		name    string
		path    string
		wantMgd bool
	}{
		{"deb/usr/bin", "/usr/bin/le_voile", true},
		{"rpm/usr/bin", "/usr/bin/le_voile_service", true},
		{"usr/local/bin (make install)", "/usr/local/bin/le_voile", true},
		{"usr/sbin", "/usr/sbin/le_voile", true},
		{"opt (manual tarball)", "/opt/levoile/le_voile", false},
		{"user install ~/.local/bin", "/home/user/.local/bin/le_voile", false},
		{"portable /tmp", "/tmp/le_voile", false},
		{"relative path", "./le_voile", false},
		// Story 8.2 L2 — Homebrew coverage.
		{"brew macOS Apple Silicon", "/opt/homebrew/bin/le_voile", true},
		{"linuxbrew", "/home/linuxbrew/.linuxbrew/bin/le_voile", true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if runtime.GOOS == "windows" {
				// On Windows the heuristic always returns false regardless of input.
				if isPackageManagedPath(tc.path) {
					t.Errorf("isPackageManagedPath(%q) = true on Windows, want false", tc.path)
				}
				return
			}
			got := isPackageManagedPath(tc.path)
			if got != tc.wantMgd {
				t.Errorf("isPackageManagedPath(%q) = %v, want %v", tc.path, got, tc.wantMgd)
			}
		})
	}
}

func TestInstaller_IsPackageManaged_WindowsAlwaysFalse(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("Windows-only assertion")
	}
	for _, p := range []string{
		`C:\Program Files\LeVoile\le_voile.exe`,
		`C:\Windows\System32\le_voile.exe`,
		`/usr/bin/le_voile`, // even POSIX-looking paths
	} {
		if isPackageManagedPath(p) {
			t.Errorf("isPackageManagedPath(%q) = true on Windows, want false", p)
		}
	}
}

// testInstallerEnv sets up a test environment for installer tests.
type testInstallerEnv struct {
	stagingDir string
	exePath    string
	binaryName string
	verifier   *Verifier
	privKey    []byte
}

func setupInstallerEnv(t *testing.T) *testInstallerEnv {
	t.Helper()

	stagingDir := t.TempDir()
	exeDir := t.TempDir()

	binaryName := fmt.Sprintf("le_voile_%s_%s", runtime.GOOS, runtime.GOARCH)
	if runtime.GOOS == "windows" {
		binaryName += ".exe"
	}

	// Create a fake current executable
	exePath := filepath.Join(exeDir, binaryName)
	if err := os.WriteFile(exePath, []byte("current binary content"), 0o755); err != nil {
		t.Fatalf("write exe: %v", err)
	}

	pub, priv, err := crypto.GenerateKeyPair()
	if err != nil {
		t.Fatalf("generate key pair: %v", err)
	}
	pubBase64 := crypto.ExportPublicKeyBase64(pub)

	verifier, err := NewVerifier(pubBase64)
	if err != nil {
		t.Fatalf("new verifier: %v", err)
	}

	return &testInstallerEnv{
		stagingDir: stagingDir,
		exePath:    exePath,
		binaryName: binaryName,
		verifier:   verifier,
		privKey:    priv,
	}
}

// stageBinary creates a valid staged update in the staging directory.
func (e *testInstallerEnv) stageBinary(t *testing.T, content, version string) *StagedUpdate {
	t.Helper()

	binaryPath := filepath.Join(e.stagingDir, e.binaryName)
	if err := os.WriteFile(binaryPath, []byte(content), 0o755); err != nil {
		t.Fatalf("write staged binary: %v", err)
	}

	// Create checksums.txt
	hash := fmt.Sprintf("%x", sha256.Sum256([]byte(content)))
	checksumContent := fmt.Sprintf("%s  %s\n", hash, e.binaryName)
	checksumPath := filepath.Join(e.stagingDir, "checksums.txt")
	if err := os.WriteFile(checksumPath, []byte(checksumContent), 0o600); err != nil {
		t.Fatalf("write checksums: %v", err)
	}

	// Create signature
	sig, err := crypto.Sign(e.privKey, []byte(checksumContent))
	if err != nil {
		t.Fatalf("sign: %v", err)
	}
	sigBase64 := base64.StdEncoding.EncodeToString(sig)
	sigPath := filepath.Join(e.stagingDir, "checksums.txt.sig")
	if err := os.WriteFile(sigPath, []byte(sigBase64), 0o600); err != nil {
		t.Fatalf("write signature: %v", err)
	}

	// Write staged_version.txt
	versionPath := filepath.Join(e.stagingDir, stagedVersionFile)
	if err := os.WriteFile(versionPath, []byte(version), 0o600); err != nil {
		t.Fatalf("write staged version: %v", err)
	}

	return &StagedUpdate{
		BinaryPath:    binaryPath,
		ChecksumPath:  checksumPath,
		SignaturePath: sigPath,
		Version:       version,
		VersionFile:   versionPath,
	}
}

func TestInstaller_HasStagedUpdate_Present(t *testing.T) {
	env := setupInstallerEnv(t)
	env.stageBinary(t, "new binary content", "2.0.0")

	inst := NewInstallerWithPath(env.stagingDir, env.exePath, env.verifier)

	staged, err := inst.HasStagedUpdate()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if staged == nil {
		t.Fatal("expected staged update, got nil")
	}
	if staged.Version != "2.0.0" {
		t.Errorf("Version = %q, want %q", staged.Version, "2.0.0")
	}
	if staged.BinaryPath == "" {
		t.Error("BinaryPath is empty")
	}
}

func TestInstaller_HasStagedUpdate_Absent(t *testing.T) {
	env := setupInstallerEnv(t)
	inst := NewInstallerWithPath(env.stagingDir, env.exePath, env.verifier)

	staged, err := inst.HasStagedUpdate()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if staged != nil {
		t.Errorf("expected nil, got version %s", staged.Version)
	}
}

func TestInstaller_HasStagedUpdate_NoVersionFile(t *testing.T) {
	env := setupInstallerEnv(t)

	// Create binary but no version file
	binaryPath := filepath.Join(env.stagingDir, env.binaryName)
	if err := os.WriteFile(binaryPath, []byte("binary"), 0o755); err != nil {
		t.Fatalf("write binary: %v", err)
	}

	inst := NewInstallerWithPath(env.stagingDir, env.exePath, env.verifier)

	staged, err := inst.HasStagedUpdate()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if staged != nil {
		t.Errorf("expected nil (no version file), got %+v", staged)
	}
}

func TestInstaller_Install_Success(t *testing.T) {
	env := setupInstallerEnv(t)
	staged := env.stageBinary(t, "new binary v2", "2.0.0")

	inst := NewInstallerWithPath(env.stagingDir, env.exePath, env.verifier)

	if err := inst.Install(context.Background(), staged); err != nil {
		t.Fatalf("Install failed: %v", err)
	}

	// Verify backup was created
	backupPath := env.exePath + ".bak"
	backupContent, err := os.ReadFile(backupPath)
	if err != nil {
		t.Fatalf("backup not found: %v", err)
	}
	if string(backupContent) != "current binary content" {
		t.Errorf("backup content = %q, want %q", string(backupContent), "current binary content")
	}

	// Verify new binary is in place
	newContent, err := os.ReadFile(env.exePath)
	if err != nil {
		t.Fatalf("read new binary: %v", err)
	}
	if string(newContent) != "new binary v2" {
		t.Errorf("new binary content = %q, want %q", string(newContent), "new binary v2")
	}

	// Verify staging was cleaned up. Persistent state markers (max seen
	// version for anti-downgrade, rollback/failure markers) survive cleanup
	// by design — filter them out before asserting the payload artefacts
	// are gone.
	entries, _ := os.ReadDir(env.stagingDir)
	persistent := map[string]bool{
		maxSeenVersionFile: true,
		rollbackStateFile:  true,
		failedVersionFile:  true,
		installRetriesFile: true,
	}
	var leftover []string
	for _, e := range entries {
		if persistent[e.Name()] {
			continue
		}
		leftover = append(leftover, e.Name())
	}
	if len(leftover) != 0 {
		t.Errorf("staging dir should be empty of payload files, got: %v", leftover)
	}
}

func TestInstaller_Install_VerificationFailure(t *testing.T) {
	env := setupInstallerEnv(t)
	staged := env.stageBinary(t, "new binary v2", "2.0.0")

	// Corrupt the binary after staging
	if err := os.WriteFile(staged.BinaryPath, []byte("corrupted"), 0o755); err != nil {
		t.Fatalf("corrupt binary: %v", err)
	}

	inst := NewInstallerWithPath(env.stagingDir, env.exePath, env.verifier)

	err := inst.Install(context.Background(), staged)
	if err == nil {
		t.Fatal("expected error from verification failure")
	}

	// Original binary should be unchanged
	content, err := os.ReadFile(env.exePath)
	if err != nil {
		t.Fatalf("read exe: %v", err)
	}
	if string(content) != "current binary content" {
		t.Errorf("original binary changed: %q", string(content))
	}
}

func TestInstaller_Install_CopyFailure_BackupRestored(t *testing.T) {
	env := setupInstallerEnv(t)
	staged := env.stageBinary(t, "new binary", "2.0.0")

	// Place the "current" exe in a separate temp dir
	exeDir := t.TempDir()
	exePath := filepath.Join(exeDir, env.binaryName)
	if err := os.WriteFile(exePath, []byte("current binary content"), 0o755); err != nil {
		t.Fatalf("write exe: %v", err)
	}

	inst := NewInstallerWithPath(env.stagingDir, exePath, env.verifier)

	// After verification, atomicCopyFile reads staged.BinaryPath again.
	// Remove it between Install's verify and copy to trigger copy failure.
	// We achieve this by replacing the staged binary with a directory (unreadable as file).
	origPath := staged.BinaryPath
	os.Remove(origPath)
	// Write a dummy so verification reads original content — but we already verified.
	// Actually: verification reads the file, so removing it fails verification too.
	// Instead, let's truncate the staged binary to 0 bytes AFTER checksum was computed
	// by the staging helper. This makes atomicCopyFile succeed but post-install size
	// check (info.Size() == 0) triggers restore.
	os.WriteFile(origPath, []byte{}, 0o755)

	// Rewrite checksums to match the empty file so verification passes
	emptyHash := fmt.Sprintf("%x", sha256.Sum256([]byte{}))
	checksumContent := fmt.Sprintf("%s  %s\n", emptyHash, env.binaryName)
	os.WriteFile(staged.ChecksumPath, []byte(checksumContent), 0o600)

	sig, err := crypto.Sign(env.privKey, []byte(checksumContent))
	if err != nil {
		t.Fatalf("sign: %v", err)
	}
	sigBase64 := base64.StdEncoding.EncodeToString(sig)
	os.WriteFile(staged.SignaturePath, []byte(sigBase64), 0o600)

	err = inst.Install(context.Background(), staged)
	if err == nil {
		t.Fatal("expected error from empty binary check")
	}

	// The backup should have been restored
	content, readErr := os.ReadFile(exePath)
	if readErr != nil {
		t.Fatalf("read exe: %v", readErr)
	}
	if string(content) != "current binary content" {
		t.Errorf("original binary not restored: %q", string(content))
	}
}

func TestInstaller_Install_NilStaged(t *testing.T) {
	env := setupInstallerEnv(t)
	inst := NewInstallerWithPath(env.stagingDir, env.exePath, env.verifier)

	err := inst.Install(context.Background(), nil)
	if err != ErrNoStagedUpdate {
		t.Errorf("expected ErrNoStagedUpdate, got %v", err)
	}
}

func TestInstaller_Install_CancelledContext(t *testing.T) {
	env := setupInstallerEnv(t)
	staged := env.stageBinary(t, "new binary", "2.0.0")

	inst := NewInstallerWithPath(env.stagingDir, env.exePath, env.verifier)

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // cancel immediately

	err := inst.Install(ctx, staged)
	if err == nil {
		t.Fatal("expected error from cancelled context")
	}

	// Original binary should be unchanged
	content, err := os.ReadFile(env.exePath)
	if err != nil {
		t.Fatalf("read exe: %v", err)
	}
	if string(content) != "current binary content" {
		t.Errorf("original binary changed: %q", string(content))
	}
}

func TestInstaller_Install_NilVerifier(t *testing.T) {
	env := setupInstallerEnv(t)
	staged := env.stageBinary(t, "new binary", "2.0.0")

	inst := NewInstallerWithPath(env.stagingDir, env.exePath, nil)

	err := inst.Install(context.Background(), staged)
	if err == nil {
		t.Fatal("expected error from nil verifier")
	}
}

func TestInstaller_Rollback_BackupExists(t *testing.T) {
	env := setupInstallerEnv(t)

	// Create a backup file
	backupPath := env.exePath + ".bak"
	if err := os.WriteFile(backupPath, []byte("old version"), 0o755); err != nil {
		t.Fatalf("write backup: %v", err)
	}

	inst := NewInstallerWithPath(env.stagingDir, env.exePath, env.verifier)

	if err := inst.Rollback(); err != nil {
		t.Fatalf("Rollback failed: %v", err)
	}

	content, err := os.ReadFile(env.exePath)
	if err != nil {
		t.Fatalf("read exe: %v", err)
	}
	if string(content) != "old version" {
		t.Errorf("rollback content = %q, want %q", string(content), "old version")
	}

	// Backup should be gone (renamed to exe)
	if _, err := os.Stat(backupPath); !os.IsNotExist(err) {
		t.Error("backup file should not exist after rollback")
	}
}

func TestInstaller_Rollback_NoBackup(t *testing.T) {
	env := setupInstallerEnv(t)
	inst := NewInstallerWithPath(env.stagingDir, env.exePath, env.verifier)

	err := inst.Rollback()
	if err == nil {
		t.Fatal("expected error when no backup exists")
	}
}

func TestInstaller_CleanupBackup(t *testing.T) {
	env := setupInstallerEnv(t)

	backupPath := env.exePath + ".bak"
	if err := os.WriteFile(backupPath, []byte("old"), 0o755); err != nil {
		t.Fatalf("write backup: %v", err)
	}

	inst := NewInstallerWithPath(env.stagingDir, env.exePath, env.verifier)

	if err := inst.CleanupBackup(); err != nil {
		t.Fatalf("CleanupBackup failed: %v", err)
	}

	if _, err := os.Stat(backupPath); !os.IsNotExist(err) {
		t.Error("backup should be removed")
	}
}

func TestInstaller_CleanupBackup_NoBackup(t *testing.T) {
	env := setupInstallerEnv(t)
	inst := NewInstallerWithPath(env.stagingDir, env.exePath, env.verifier)

	// Should not error when no backup exists
	if err := inst.CleanupBackup(); err != nil {
		t.Errorf("CleanupBackup should not fail when no backup: %v", err)
	}
}

func TestWriteStagedVersion(t *testing.T) {
	dir := t.TempDir()

	if err := writeStagedVersion(dir, "3.1.0"); err != nil {
		t.Fatalf("writeStagedVersion: %v", err)
	}

	data, err := os.ReadFile(filepath.Join(dir, stagedVersionFile))
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if string(data) != "3.1.0" {
		t.Errorf("version = %q, want %q", string(data), "3.1.0")
	}
}

func TestInstaller_Install_ReadOnly(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("read-only directory test not reliable on Windows")
	}

	env := setupInstallerEnv(t)
	staged := env.stageBinary(t, "new binary", "2.0.0")

	// Make the executable directory read-only
	exeDir := filepath.Dir(env.exePath)
	os.Chmod(exeDir, 0o444)
	t.Cleanup(func() { os.Chmod(exeDir, 0o755) })

	inst := NewInstallerWithPath(env.stagingDir, env.exePath, env.verifier)

	err := inst.Install(context.Background(), staged)
	if err == nil {
		t.Fatal("expected error for read-only target")
	}
}
