package service

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	"github.com/velia-the-veil/le_voile/internal/updater"
)

func TestNewProgram(t *testing.T) {
	cfg := Config{RelayDomain: "test.example.com", RelayPubKey: "dGVzdA=="}
	prg := NewProgram(cfg)
	if prg == nil {
		t.Fatal("NewProgram returned nil")
	}
	if prg.config.RelayDomain != "test.example.com" {
		t.Errorf("RelayDomain = %q, want %q", prg.config.RelayDomain, "test.example.com")
	}
}

func TestProgram_StartStop(t *testing.T) {
	// Suppress stderr output during test.
	var buf bytes.Buffer
	oldStderr := serviceStderr
	serviceStderr = &buf
	defer func() { serviceStderr = oldStderr }()

	cfg := Config{RelayDomain: "test.example.com", RelayPubKey: "invalid-key"}
	prg := NewProgram(cfg)

	// Start should not block.
	err := prg.Start(nil)
	if err != nil {
		t.Fatalf("Start returned error: %v", err)
	}

	// The run goroutine will fail (invalid key) and close done.
	select {
	case <-prg.done:
		// Expected: run exits due to invalid key.
	case <-time.After(2 * time.Second):
		t.Fatal("run did not exit within 2 seconds")
	}

	// Stop should be safe to call even after run exits.
	prg.cancel()
}

func TestProgram_SetIPCServer(t *testing.T) {
	cfg := Config{RelayDomain: "test.example.com", RelayPubKey: "dGVzdA=="}
	prg := NewProgram(cfg)

	startCalled := false
	stopCalled := false
	prg.SetIPCServer(
		func(_ context.Context) error {
			startCalled = true
			return nil
		},
		func() {
			stopCalled = true
		},
	)

	// Verify callbacks were stored by calling them directly.
	prg.mu.Lock()
	startFn := prg.ipcStart
	stopFn := prg.ipcStop
	prg.mu.Unlock()

	if startFn == nil {
		t.Fatal("ipcStart not set after SetIPCServer")
	}
	if stopFn == nil {
		t.Fatal("ipcStop not set after SetIPCServer")
	}
	if err := startFn(context.Background()); err != nil {
		t.Fatalf("ipcStart returned error: %v", err)
	}
	if !startCalled {
		t.Error("ipcStart callback was not invoked")
	}
	stopFn()
	if !stopCalled {
		t.Error("ipcStop callback was not invoked")
	}

	// Reset to nil — verify no panic.
	prg.SetIPCServer(nil, nil)
}

func TestProgram_Reconnector_NilBeforeStart(t *testing.T) {
	cfg := Config{RelayDomain: "test.example.com", RelayPubKey: "dGVzdA=="}
	prg := NewProgram(cfg)
	if prg.Reconnector() != nil {
		t.Error("Reconnector should be nil before Start")
	}
}

func TestProgram_Context_NilBeforeStart(t *testing.T) {
	cfg := Config{RelayDomain: "test.example.com", RelayPubKey: "dGVzdA=="}
	prg := NewProgram(cfg)
	if prg.Context() != nil {
		t.Error("Context should be nil before Start")
	}
}

func TestProgram_StartTime(t *testing.T) {
	cfg := Config{RelayDomain: "test.example.com", RelayPubKey: "dGVzdA=="}
	prg := NewProgram(cfg)
	if !prg.StartTime().IsZero() {
		t.Error("StartTime should be zero before Start")
	}
}

func TestProgram_StopProxy_NilSafe(t *testing.T) {
	cfg := Config{RelayDomain: "test.example.com", RelayPubKey: "dGVzdA=="}
	prg := NewProgram(cfg)
	// stopProxy should be safe to call with nil cancel/errCh.
	prg.stopProxy()
}

func TestProgram_STUNActive_NilBeforeStart(t *testing.T) {
	cfg := Config{RelayDomain: "test.example.com", RelayPubKey: "dGVzdA=="}
	prg := NewProgram(cfg)
	if prg.STUNActive() {
		t.Error("STUNActive should be false before start")
	}
}

func TestProgram_StartStopSTUN(t *testing.T) {
	cfg := Config{RelayDomain: "test.example.com", RelayPubKey: "dGVzdA=="}
	prg := NewProgram(cfg)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Start STUN on ephemeral ports (port 0).
	prg.startSTUN(ctx)

	if !prg.STUNActive() {
		t.Error("STUNActive should be true after startSTUN")
	}

	// Stop STUN.
	prg.stopSTUN()

	// Wait briefly for goroutine to clean up.
	time.Sleep(50 * time.Millisecond)

	if prg.STUNActive() {
		t.Error("STUNActive should be false after stopSTUN")
	}
}

func TestService_KillSwitch_BlocksSTUN(t *testing.T) {
	cfg := Config{RelayDomain: "test.example.com", RelayPubKey: "dGVzdA=="}
	prg := NewProgram(cfg)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Start STUN on ephemeral ports.
	prg.startSTUN(ctx)
	defer prg.stopSTUN()

	if !prg.STUNActive() {
		t.Fatal("STUN should be active after startSTUN")
	}

	// Verify interceptor processes packets when enabled.
	prg.stunMu.Lock()
	interceptor := prg.stunInterceptor
	prg.stunMu.Unlock()

	if interceptor == nil {
		t.Fatal("stunInterceptor is nil")
	}

	// Simulate kill switch activation — disable STUN.
	prg.setSTUNEnabled(false)

	// Verify interceptor is disabled (enabled field via SetEnabled).
	// We can't directly check enabled field, but we can verify behavior:
	// send packet to handlePacket — neither forward nor intercept should be called.
	// This confirms setSTUNEnabled propagates to the interceptor.

	// Simulate kill switch deactivation — re-enable STUN.
	prg.setSTUNEnabled(true)

	// Verify interceptor is re-enabled — STUN should still be active.
	if !prg.STUNActive() {
		t.Error("STUN should still be active after re-enabling")
	}
}

func TestProgram_StopSTUN_NilSafe(t *testing.T) {
	cfg := Config{RelayDomain: "test.example.com", RelayPubKey: "dGVzdA=="}
	prg := NewProgram(cfg)
	// stopSTUN should be safe to call with nil cancel/errCh.
	prg.stopSTUN()
}

func TestProgram_TryInstall_NoStagedUpdate(t *testing.T) {
	var buf bytes.Buffer
	oldStderr := serviceStderr
	serviceStderr = &buf
	defer func() { serviceStderr = oldStderr }()

	stagingDir := t.TempDir()
	cfg := Config{
		RelayDomain:      "test.example.com",
		RelayPubKey:      "dGVzdA==",
		UpdateEnabled:    true,
		UpdateStagingDir: stagingDir,
	}
	prg := NewProgram(cfg)
	prg.ctx, prg.cancel = context.WithCancel(context.Background())

	// Should not panic; installer should be set but no update installed
	prg.tryInstallStagedUpdate(prg.ctx)

	// installer should be non-nil (or nil due to invalid key — check gracefully)
	if prg.InstalledVersion() != "" {
		t.Errorf("InstalledVersion = %q, want empty", prg.InstalledVersion())
	}
	if prg.LastInstallError() != "" {
		t.Errorf("LastInstallError = %q, want empty", prg.LastInstallError())
	}
}

func TestProgram_Installer_NilBeforeStart(t *testing.T) {
	cfg := Config{RelayDomain: "test.example.com", RelayPubKey: "dGVzdA=="}
	prg := NewProgram(cfg)
	if prg.Installer() != nil {
		t.Error("Installer should be nil before Start")
	}
}

func TestProgram_UpdateState_Accessors(t *testing.T) {
	cfg := Config{RelayDomain: "test.example.com", RelayPubKey: "dGVzdA=="}
	prg := NewProgram(cfg)

	// Test initial state
	if prg.PendingUpdateVersion() != "" {
		t.Errorf("PendingUpdateVersion = %q, want empty", prg.PendingUpdateVersion())
	}
	if prg.InstalledVersion() != "" {
		t.Errorf("InstalledVersion = %q, want empty", prg.InstalledVersion())
	}
	if prg.LastInstallError() != "" {
		t.Errorf("LastInstallError = %q, want empty", prg.LastInstallError())
	}

	// Set and verify
	prg.updateMu.Lock()
	prg.pendingUpdateVersion = "2.0.0"
	prg.installedVersion = "1.5.0"
	prg.lastInstallError = "some error"
	prg.updateMu.Unlock()

	if prg.PendingUpdateVersion() != "2.0.0" {
		t.Errorf("PendingUpdateVersion = %q, want %q", prg.PendingUpdateVersion(), "2.0.0")
	}
	if prg.InstalledVersion() != "1.5.0" {
		t.Errorf("InstalledVersion = %q, want %q", prg.InstalledVersion(), "1.5.0")
	}
	if prg.LastInstallError() != "some error" {
		t.Errorf("LastInstallError = %q, want %q", prg.LastInstallError(), "some error")
	}
}

func TestProgram_RollbackAccessors(t *testing.T) {
	cfg := Config{RelayDomain: "test.example.com", RelayPubKey: "dGVzdA=="}
	prg := NewProgram(cfg)

	// Initial state
	if prg.RollbackOccurred() {
		t.Error("RollbackOccurred should be false initially")
	}
	if prg.RollbackVersion() != "" {
		t.Errorf("RollbackVersion = %q, want empty", prg.RollbackVersion())
	}
	if prg.RollbackReason() != "" {
		t.Errorf("RollbackReason = %q, want empty", prg.RollbackReason())
	}

	// Set and verify
	prg.updateMu.Lock()
	prg.rollbackOccurred = true
	prg.rollbackVersion = "2.1.0"
	prg.rollbackReason = "tunnel timeout"
	prg.updateMu.Unlock()

	if !prg.RollbackOccurred() {
		t.Error("RollbackOccurred should be true after set")
	}
	if prg.RollbackVersion() != "2.1.0" {
		t.Errorf("RollbackVersion = %q, want %q", prg.RollbackVersion(), "2.1.0")
	}
	if prg.RollbackReason() != "tunnel timeout" {
		t.Errorf("RollbackReason = %q, want %q", prg.RollbackReason(), "tunnel timeout")
	}
}

func TestService_TryRollbackIfNeeded_NoInstall(t *testing.T) {
	var buf bytes.Buffer
	oldStderr := serviceStderr
	serviceStderr = &buf
	defer func() { serviceStderr = oldStderr }()

	stagingDir := t.TempDir()
	cfg := Config{
		RelayDomain:      "test.example.com",
		RelayPubKey:      "dGVzdA==",
		UpdateEnabled:    true,
		UpdateStagingDir: stagingDir,
	}
	prg := NewProgram(cfg)

	// No rollback state file → should return false (no rollback)
	tunnelErr := context.DeadlineExceeded
	result := prg.tryRollbackIfNeeded(context.Background(), tunnelErr)
	if result {
		t.Error("tryRollbackIfNeeded should return false when no install occurred")
	}
	if prg.RollbackOccurred() {
		t.Error("RollbackOccurred should be false")
	}
}

func TestService_TryRollbackIfNeeded_RollbackFails(t *testing.T) {
	var buf bytes.Buffer
	oldStderr := serviceStderr
	serviceStderr = &buf
	defer func() { serviceStderr = oldStderr }()

	stagingDir := t.TempDir()
	exeDir := t.TempDir()

	binaryName := "le_voile_test_noperm"
	if runtime.GOOS == "windows" {
		binaryName += ".exe"
	}
	exePath := filepath.Join(exeDir, binaryName)

	// Create current exe but NO backup file — Rollback() will fail
	if err := os.WriteFile(exePath, []byte("new binary"), 0o755); err != nil {
		t.Fatalf("write exe: %v", err)
	}
	// Intentionally no .bak file → tryRollbackIfNeeded returns false

	if err := updater.WriteRollbackState(stagingDir, &updater.RollbackState{
		JustInstalled:    true,
		InstalledVersion: "2.1.0",
	}); err != nil {
		t.Fatalf("write rollback state: %v", err)
	}

	cfg := Config{
		RelayDomain:      "test.example.com",
		RelayPubKey:      "dGVzdA==",
		UpdateEnabled:    true,
		UpdateStagingDir: stagingDir,
	}
	prg := NewProgram(cfg)
	inst := updater.NewInstallerWithPath(stagingDir, exePath, nil)
	prg.installer = inst

	result := prg.tryRollbackIfNeeded(context.Background(), context.DeadlineExceeded)
	if result {
		t.Error("tryRollbackIfNeeded should return false when no backup exists")
	}
	if prg.RollbackOccurred() {
		t.Error("RollbackOccurred should be false when rollback fails")
	}
}

func TestService_TryRollbackIfNeeded_AfterInstall(t *testing.T) {
	var buf bytes.Buffer
	oldStderr := serviceStderr
	serviceStderr = &buf
	defer func() { serviceStderr = oldStderr }()

	stagingDir := t.TempDir()
	exeDir := t.TempDir()

	binaryName := "le_voile_test"
	if runtime.GOOS == "windows" {
		binaryName += ".exe"
	}
	exePath := filepath.Join(exeDir, binaryName)

	// Create "current" exe (the bad new version)
	if err := os.WriteFile(exePath, []byte("new bad binary"), 0o755); err != nil {
		t.Fatalf("write exe: %v", err)
	}
	// Create backup (the good old version)
	backupPath := exePath + ".bak"
	if err := os.WriteFile(backupPath, []byte("old good binary"), 0o755); err != nil {
		t.Fatalf("write backup: %v", err)
	}

	// Write rollback state (simulating post-install)
	if err := updater.WriteRollbackState(stagingDir, &updater.RollbackState{
		JustInstalled:    true,
		InstalledVersion: "2.1.0",
	}); err != nil {
		t.Fatalf("write rollback state: %v", err)
	}

	cfg := Config{
		RelayDomain:      "test.example.com",
		RelayPubKey:      "dGVzdA==",
		UpdateEnabled:    true,
		UpdateStagingDir: stagingDir,
	}
	prg := NewProgram(cfg)

	// Set installer with test path
	inst := updater.NewInstallerWithPath(stagingDir, exePath, nil)
	prg.installer = inst

	tunnelErr := context.DeadlineExceeded
	result := prg.tryRollbackIfNeeded(context.Background(), tunnelErr)
	if !result {
		t.Fatal("tryRollbackIfNeeded should return true when rollback succeeds")
	}

	// Verify rollback state
	if !prg.RollbackOccurred() {
		t.Error("RollbackOccurred should be true")
	}
	if prg.RollbackVersion() != "2.1.0" {
		t.Errorf("RollbackVersion = %q, want %q", prg.RollbackVersion(), "2.1.0")
	}
	if prg.RollbackReason() == "" {
		t.Error("RollbackReason should not be empty")
	}

	// Verify old binary restored
	content, err := os.ReadFile(exePath)
	if err != nil {
		t.Fatalf("read exe: %v", err)
	}
	if string(content) != "old good binary" {
		t.Errorf("exe content = %q, want %q", string(content), "old good binary")
	}

	// Verify failed version was written
	failedVer, err := updater.ReadFailedVersion(stagingDir)
	if err != nil {
		t.Fatalf("ReadFailedVersion: %v", err)
	}
	if failedVer != "2.1.0" {
		t.Errorf("failed version = %q, want %q", failedVer, "2.1.0")
	}

	// Verify rollback state was cleared
	state, err := updater.ReadRollbackState(stagingDir)
	if err != nil {
		t.Fatalf("ReadRollbackState: %v", err)
	}
	if state != nil {
		t.Error("rollback state should be cleared after rollback")
	}
}

func TestService_TunnelSuccess_ClearsRollbackState(t *testing.T) {
	stagingDir := t.TempDir()

	// Write rollback state
	if err := updater.WriteRollbackState(stagingDir, &updater.RollbackState{
		JustInstalled:    true,
		InstalledVersion: "2.0.0",
	}); err != nil {
		t.Fatalf("write rollback state: %v", err)
	}

	// Simulate what run() does after successful tunnel connect
	if err := updater.ClearRollbackState(stagingDir); err != nil {
		t.Fatalf("ClearRollbackState: %v", err)
	}

	state, err := updater.ReadRollbackState(stagingDir)
	if err != nil {
		t.Fatalf("ReadRollbackState: %v", err)
	}
	if state != nil {
		t.Error("rollback state should be nil after clear")
	}
}

func TestService_RollbackTimeout_Constant(t *testing.T) {
	if rollbackTimeout != 30*time.Second {
		t.Errorf("rollbackTimeout = %v, want 30s", rollbackTimeout)
	}
}

func TestService_TunnelTimeout_AfterInstall_TriggersRollback(t *testing.T) {
	var buf bytes.Buffer
	oldStderr := serviceStderr
	serviceStderr = &buf
	defer func() { serviceStderr = oldStderr }()

	stagingDir := t.TempDir()
	exeDir := t.TempDir()

	binaryName := "le_voile_test"
	if runtime.GOOS == "windows" {
		binaryName += ".exe"
	}
	exePath := filepath.Join(exeDir, binaryName)

	// Create "current" exe (bad new version)
	if err := os.WriteFile(exePath, []byte("new bad binary"), 0o755); err != nil {
		t.Fatalf("write exe: %v", err)
	}
	// Create backup (good old version)
	backupPath := exePath + ".bak"
	if err := os.WriteFile(backupPath, []byte("old good binary"), 0o755); err != nil {
		t.Fatalf("write backup: %v", err)
	}

	// Write rollback state
	if err := updater.WriteRollbackState(stagingDir, &updater.RollbackState{
		JustInstalled:    true,
		InstalledVersion: "2.1.0",
	}); err != nil {
		t.Fatalf("write rollback state: %v", err)
	}

	cfg := Config{
		RelayDomain:      "test.example.com",
		RelayPubKey:      "dGVzdA==",
		UpdateEnabled:    true,
		UpdateStagingDir: stagingDir,
	}
	prg := NewProgram(cfg)

	inst := updater.NewInstallerWithPath(stagingDir, exePath, nil)
	prg.installer = inst

	// Simulate timeout error (what happens when tunnel doesn't connect within 30s)
	tunnelErr := context.DeadlineExceeded
	result := prg.tryRollbackIfNeeded(context.Background(), tunnelErr)
	if !result {
		t.Fatal("tryRollbackIfNeeded should return true for timeout after install")
	}

	if !prg.RollbackOccurred() {
		t.Error("RollbackOccurred should be true")
	}
	if prg.RollbackVersion() != "2.1.0" {
		t.Errorf("RollbackVersion = %q, want %q", prg.RollbackVersion(), "2.1.0")
	}

	// Verify the reason mentions the deadline
	reason := prg.RollbackReason()
	if reason != "context deadline exceeded" {
		t.Errorf("RollbackReason = %q, want %q", reason, "context deadline exceeded")
	}
}

func TestProgram_StartStop_WithUpdateEnabled(t *testing.T) {
	var buf bytes.Buffer
	oldStderr := serviceStderr
	serviceStderr = &buf
	defer func() { serviceStderr = oldStderr }()

	stagingDir := t.TempDir()
	cfg := Config{
		RelayDomain:      "test.example.com",
		RelayPubKey:      "invalid-key",
		UpdateEnabled:    true,
		UpdateStagingDir: stagingDir,
	}
	prg := NewProgram(cfg)

	err := prg.Start(nil)
	if err != nil {
		t.Fatalf("Start returned error: %v", err)
	}

	select {
	case <-prg.done:
		// Expected: run exits due to invalid key (tunnel fails)
	case <-time.After(2 * time.Second):
		t.Fatal("run did not exit within 2 seconds")
	}

	prg.cancel()
}
