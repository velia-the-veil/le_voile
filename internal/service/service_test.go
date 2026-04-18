package service

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"runtime"
	"sync"
	"testing"
	"time"

	"github.com/velia-the-veil/le_voile/internal/browser"
	"github.com/velia-the-veil/le_voile/internal/tunnel"
	"github.com/velia-the-veil/le_voile/internal/updater"
)

func TestProgram_CircuitBreakerHook(t *testing.T) {
	// Validate the contract between the reconnector hook and Program state:
	// tripCircuitBreaker must set the tripped flag, capture the message,
	// and transition the tunnel state to StateFailed so IPC polling sees it.
	prg := NewProgram(Config{RelayDomain: "test.example.com", RelayPubKey: "dGVzdA=="})

	// A tunnel client is required for the StateFailed transition; build one
	// through the public helper. Any valid-length Ed25519 key works — we
	// never dial. 32 zero bytes base64-encoded is a valid-length stand-in.
	const zeroKey32 = "AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA="
	client, err := tunnel.NewClient("127.0.0.1:1", zeroKey32, tunnel.WithInsecure(true))
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}
	prg.tunnelClient = client

	if prg.CircuitBreakerTripped() {
		t.Fatal("CircuitBreakerTripped = true before trip, want false")
	}
	if prg.CircuitBreakerMessage() != "" {
		t.Fatalf("CircuitBreakerMessage = %q before trip, want empty", prg.CircuitBreakerMessage())
	}

	prg.tripCircuitBreaker(CircuitBreakerAlertMessage)

	if !prg.CircuitBreakerTripped() {
		t.Error("CircuitBreakerTripped = false after trip, want true")
	}
	if got := prg.CircuitBreakerMessage(); got != CircuitBreakerAlertMessage {
		t.Errorf("CircuitBreakerMessage = %q, want %q", got, CircuitBreakerAlertMessage)
	}
	if got := client.State().Get(); got != tunnel.StateFailed {
		t.Errorf("tunnel state = %q, want %q", got, tunnel.StateFailed)
	}

	// Reset must clear all three: flag, message, and reconnector.Reset()
	// (we don't assert reconnector here since it's nil — covered by tunnel tests).
	prg.ResetCircuitBreaker()
	if prg.CircuitBreakerTripped() {
		t.Error("CircuitBreakerTripped = true after Reset, want false")
	}
	if prg.CircuitBreakerMessage() != "" {
		t.Errorf("CircuitBreakerMessage = %q after Reset, want empty", prg.CircuitBreakerMessage())
	}
}

// TestProgram_FailoverAlertLifecycle exercises the Story 4.4 contract on
// Program state: the failover banner must be set via the wrapper, surfaced
// through the public getter, and cleared both by ResetCircuitBreaker (AC6 —
// manual reconnect path) and by an explicit SetCurrentCountry call
// sequence-equivalent to SelectCountry.
func TestProgram_FailoverAlertLifecycle(t *testing.T) {
	prg := NewProgram(Config{RelayDomain: "test.example.com", RelayPubKey: "dGVzdA=="})

	// Initial state: both getters return empty.
	if got := prg.FailoverAlert(); got != "" {
		t.Fatalf("FailoverAlert = %q before any set, want empty", got)
	}
	if got := prg.CurrentCountryCode(); got != "" {
		t.Fatalf("CurrentCountryCode = %q before any set, want empty", got)
	}

	// Simulate an inter-country failover completing: the wrapper writes the
	// message through setFailoverAlert and the country through SetCurrentCountry.
	prg.setFailoverAlert("Tous les relais Allemagne indisponibles, basculement vers Royaume-Uni")
	prg.SetCurrentCountry("gb")

	if got := prg.FailoverAlert(); got != "Tous les relais Allemagne indisponibles, basculement vers Royaume-Uni" {
		t.Errorf("FailoverAlert = %q, want the set message", got)
	}
	if got := prg.CurrentCountryCode(); got != "gb" {
		t.Errorf("CurrentCountryCode = %q, want gb", got)
	}

	// AC6 — manual reconnect via ResetCircuitBreaker clears the banner.
	prg.ResetCircuitBreaker()
	if got := prg.FailoverAlert(); got != "" {
		t.Errorf("FailoverAlert = %q after ResetCircuitBreaker, want empty (AC6)", got)
	}
	// CurrentCountryCode is NOT reset by ResetCircuitBreaker — the country
	// remains the active one until a new failover or SelectCountry happens.
	if got := prg.CurrentCountryCode(); got != "gb" {
		t.Errorf("CurrentCountryCode = %q after ResetCircuitBreaker, want gb (not reset)", got)
	}

	// AC6 — SelectCountry sequence: ClearFailoverAlert + SetCurrentCountry.
	prg.setFailoverAlert("stale banner")
	prg.ClearFailoverAlert()
	prg.SetCurrentCountry("fr")
	if got := prg.FailoverAlert(); got != "" {
		t.Errorf("FailoverAlert = %q after ClearFailoverAlert, want empty", got)
	}
	if got := prg.CurrentCountryCode(); got != "fr" {
		t.Errorf("CurrentCountryCode = %q after SelectCountry sequence, want fr", got)
	}
}

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

// Story 6.1: removed tests tied to the ex-STUN interceptor (STUNActive,
// startSTUN, stopSTUN, setSTUNEnabled, TestService_KillSwitch_BlocksSTUN).
// Post-Epic-2 L3 capture + Story-6.1 net.DialUDP leakcheck make those paths
// obsolete. STUN traffic now flows structurally through levoile0.

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

// TestTryInstallStagedUpdate_AbandonsAfterMaxRetries verifies the install-retry
// cap (Story 8.2 Task 6). When the persisted retry counter already meets or
// exceeds the configured maximum, tryInstallStagedUpdate must clear the staged
// payload and return without attempting Install() again, so we don't loop on
// every boot.
func TestTryInstallStagedUpdate_AbandonsAfterMaxRetries(t *testing.T) {
	var buf bytes.Buffer
	oldStderr := serviceStderr
	serviceStderr = &buf
	defer func() { serviceStderr = oldStderr }()

	stagingDir := t.TempDir()

	// Pre-write a retry counter at the cap.
	if err := updater.WriteInstallRetries(stagingDir, 3); err != nil {
		t.Fatalf("seed retries: %v", err)
	}

	// Create fake staged files so HasStagedUpdate returns non-nil.
	binaryName := "le_voile_" + runtime.GOOS + "_" + runtime.GOARCH
	if runtime.GOOS == "windows" {
		binaryName += ".exe"
	}
	stagedBinary := filepath.Join(stagingDir, binaryName)
	if err := os.WriteFile(stagedBinary, []byte("fake staged binary"), 0o644); err != nil {
		t.Fatalf("write staged binary: %v", err)
	}
	if err := os.WriteFile(filepath.Join(stagingDir, "staged_version.txt"), []byte("9.9.9"), 0o600); err != nil {
		t.Fatalf("write staged version: %v", err)
	}
	// checksums + signature files must exist (they're path references).
	os.WriteFile(filepath.Join(stagingDir, "checksums.txt"), []byte("x"), 0o600)
	os.WriteFile(filepath.Join(stagingDir, "checksums.txt.sig"), []byte("x"), 0o600)

	cfg := Config{
		RelayDomain:             "test.example.com",
		RelayPubKey:             "AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA=",
		UpdateEnabled:           true,
		UpdateStagingDir:        stagingDir,
		UpdateMaxInstallRetries: 3,
	}
	prg := NewProgram(cfg)

	// tryInstallStagedUpdate must abandon the staged payload.
	prg.tryInstallStagedUpdate(context.Background())

	// Staged binary should be wiped.
	if _, err := os.Stat(stagedBinary); !os.IsNotExist(err) {
		t.Errorf("expected staged binary removed, err=%v", err)
	}
	// Retry counter should be cleared.
	n, err := updater.ReadInstallRetries(stagingDir)
	if err != nil {
		t.Fatalf("ReadInstallRetries: %v", err)
	}
	if n != 0 {
		t.Errorf("expected retry counter cleared after abandon, got %d", n)
	}
	if !bytes.Contains(buf.Bytes(), []byte("abandoned after")) {
		t.Errorf("expected 'abandoned after' log line, got: %s", buf.String())
	}
}

// TestScheduleServiceRestart_NoSvc_NoOp verifies that the rollback restart path
// is a no-op when the program has no kardianos service reference attached
// (portable mode, direct invocation, or test harness). This guards against a
// nil-pointer panic on the rollback code path.
func TestScheduleServiceRestart_NoSvc_NoOp(t *testing.T) {
	cfg := Config{
		RelayDomain: "test.example.com",
		RelayPubKey: "dGVzdA==",
	}
	prg := NewProgram(cfg)
	// prg.svc is nil (never Start()ed under a service.Service); scheduleServiceRestart
	// must return immediately without starting a goroutine or panicking.
	prg.scheduleServiceRestart()
	// If we reach this line the function returned cleanly.
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

	// Simulate that tryInstallStagedUpdate set installedVersion (as in real flow)
	prg.SetInstalledVersion("2.1.0")

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
	// installedVersion must be cleared after rollback — the version is no longer
	// installed and a stale value would cause a spurious 30s tunnel timeout.
	if prg.InstalledVersion() != "" {
		t.Errorf("InstalledVersion should be empty after rollback, got %q", prg.InstalledVersion())
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

// --- Story 12.1 tests ---

// TestStop_Idempotent verifies that Stop() called concurrently only executes
// the context cancel once (AC6 — sync.Once guard).
func TestStop_Idempotent(t *testing.T) {
	var buf bytes.Buffer
	oldStderr := serviceStderr
	serviceStderr = &buf
	defer func() { serviceStderr = oldStderr }()

	cfg := Config{RelayDomain: "test.example.com", RelayPubKey: "invalid-key"}
	prg := NewProgram(cfg)

	prg.Start(nil)

	// Wait for run() to exit (invalid key fails quickly).
	select {
	case <-prg.done:
	case <-time.After(2 * time.Second):
		t.Fatal("run did not exit within 2 seconds")
	}

	// Call Stop concurrently — should not panic or race.
	var wg sync.WaitGroup
	for i := 0; i < 5; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			prg.Stop(nil)
		}()
	}
	wg.Wait()
}

// TestShutdownSequence_IPCServerLast verifies that the IPC server stop callback
// is called AFTER DNS/browser policy restore callbacks in shutdown() (AC4, AC8).
func TestShutdownSequence_IPCServerLast(t *testing.T) {
	var buf bytes.Buffer
	oldStderr := serviceStderr
	serviceStderr = &buf
	defer func() { serviceStderr = oldStderr }()

	cfg := Config{RelayDomain: "test.example.com", RelayPubKey: "dGVzdA=="}
	prg := NewProgram(cfg)

	// Track call order via a shared slice.
	var mu sync.Mutex
	var order []string

	// Inject a mock DNS manager to track RestoreResolver call order.
	prg.dnsManager = &mockDNSManager{
		onRestore: func() {
			mu.Lock()
			order = append(order, "dns_restore")
			mu.Unlock()
		},
	}

	// Inject a mock browser policy manager to track RestorePolicies call order.
	prg.browserPolicyMu.Lock()
	prg.browserPolicyMgr = &mockPolicyManager{
		onRestore: func() {
			mu.Lock()
			order = append(order, "browser_restore")
			mu.Unlock()
		},
	}
	prg.browserPolicyMu.Unlock()

	prg.SetIPCServer(
		func(_ context.Context) error { return nil },
		func() {
			mu.Lock()
			order = append(order, "ipc_stop")
			mu.Unlock()
		},
	)

	// Call shutdown directly (no need to start the full service).
	prg.shutdown()

	mu.Lock()
	defer mu.Unlock()

	// All three should have been called.
	if len(order) < 3 {
		t.Fatalf("expected at least 3 shutdown events, got %d: %v", len(order), order)
	}

	// Browser restore must come before IPC stop.
	// DNS restore must come before IPC stop.
	// IPC stop must be last.
	if order[len(order)-1] != "ipc_stop" {
		t.Errorf("expected ipc_stop to be last, got order: %v", order)
	}

	// Verify browser_restore and dns_restore both appear before ipc_stop.
	ipcIdx := -1
	dnsIdx := -1
	browserIdx := -1
	for i, name := range order {
		switch name {
		case "ipc_stop":
			ipcIdx = i
		case "dns_restore":
			dnsIdx = i
		case "browser_restore":
			browserIdx = i
		}
	}

	if browserIdx < 0 {
		t.Error("browser_restore was not called during shutdown")
	} else if browserIdx > ipcIdx {
		t.Errorf("browser_restore (%d) must come before ipc_stop (%d), order: %v", browserIdx, ipcIdx, order)
	}

	if dnsIdx < 0 {
		t.Error("dns_restore was not called during shutdown")
	} else if dnsIdx > ipcIdx {
		t.Errorf("dns_restore (%d) must come before ipc_stop (%d), order: %v", dnsIdx, ipcIdx, order)
	}
}

// mockDNSManager implements dns.DNSManager for tracking shutdown ordering.
type mockDNSManager struct {
	onRestore func()
}

func (m *mockDNSManager) SetResolver(_ context.Context, _ string) error { return nil }
func (m *mockDNSManager) RestoreResolver(_ context.Context) error {
	if m.onRestore != nil {
		m.onRestore()
	}
	return nil
}
func (m *mockDNSManager) OriginalResolver() string { return "8.8.8.8" }

// mockPolicyManager implements browser.PolicyManager for tracking shutdown ordering.
type mockPolicyManager struct {
	onRestore func()
}

func (m *mockPolicyManager) ApplyPolicies(_ context.Context) (*browser.ApplyResult, error) {
	return nil, nil
}
func (m *mockPolicyManager) RestorePolicies(_ context.Context) error {
	if m.onRestore != nil {
		m.onRestore()
	}
	return nil
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
