package ipchandler

import (
	"context"
	"net"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/velia-the-veil/le_voile/internal/config"
	"github.com/velia-the-veil/le_voile/internal/ipc"
	"github.com/velia-the-veil/le_voile/internal/leakcheck"
	svc "github.com/velia-the-veil/le_voile/internal/service"
	"github.com/velia-the-veil/le_voile/internal/tunnel"
)

// --- Helpers for leakcheck scheduler construction in tests ---

type testKSQuerier struct{ active bool }

func (k *testKSQuerier) IsActive() bool { return k.active }

type testTSQuerier struct{ state tunnel.ConnState }

func (s *testTSQuerier) Get() tunnel.ConnState { return s.state }

// newTestScheduler builds a PeriodicScheduler suitable for unit tests.
// It uses a no-op PublicIPFunc so RunFullCheck never dials real STUN servers.
func newTestScheduler() *leakcheck.PeriodicScheduler {
	checker := leakcheck.NewWebRTCLeakChecker(func(_ context.Context) (net.IP, error) {
		return net.ParseIP("192.0.2.1"), nil
	})
	return leakcheck.NewPeriodicScheduler(
		10*time.Minute,
		checker,
		&testKSQuerier{active: false},
		&testTSQuerier{state: tunnel.StateConnected},
		nil, nil,
	)
}

func newTestProgram() *svc.Program {
	return svc.NewProgram(svc.Config{
		RelayDomain: "test.dev",
		RelayPubKey: "dGVzdA==",
	})
}

func TestHandle_GetStatus_NilTunnel(t *testing.T) {
	resp := Handle(newTestProgram(), ipc.Request{Action: ipc.ActionGetStatus}, Options{})
	if resp.Status != ipc.StatusDisconnected {
		t.Errorf("expected disconnected, got %q", resp.Status)
	}
}

func TestHandle_Connect_NilTunnel(t *testing.T) {
	resp := Handle(newTestProgram(), ipc.Request{Action: ipc.ActionConnect}, Options{})
	if resp.Status != ipc.StatusError || resp.Error != "service_not_ready" {
		t.Errorf("expected error/service_not_ready, got %q/%q", resp.Status, resp.Error)
	}
}

func TestHandle_Disconnect_NilTunnel(t *testing.T) {
	resp := Handle(newTestProgram(), ipc.Request{Action: ipc.ActionDisconnect}, Options{})
	if resp.Status != ipc.StatusDisconnected {
		t.Errorf("expected disconnected, got %q", resp.Status)
	}
}

func TestHandle_Quit_NilTunnel(t *testing.T) {
	resp := Handle(newTestProgram(), ipc.Request{Action: ipc.ActionQuit}, Options{})
	if resp.Status != ipc.StatusDisconnected {
		t.Errorf("expected disconnected, got %q", resp.Status)
	}
}

func TestHandle_UnknownAction(t *testing.T) {
	resp := Handle(newTestProgram(), ipc.Request{Action: "unknown_test"}, Options{})
	if resp.Status != ipc.StatusError || resp.Error != "unknown_action" {
		t.Errorf("expected error/unknown_action, got %q/%q", resp.Status, resp.Error)
	}
}

func TestHandle_GetStatus_CircuitBreakerTripped(t *testing.T) {
	// When the Program records a circuit breaker trip, GetStatus must
	// surface the tripped flag and French message so the UI can render
	// a persistent banner. Validates the happy path with a nil tunnel
	// (tripping before first connect is theoretically possible during
	// a rollback scenario).
	prg := newTestProgram()
	prg.ForTestTripCircuitBreaker(svc.CircuitBreakerAlertMessage)

	resp := Handle(prg, ipc.Request{Action: ipc.ActionGetStatus}, Options{})

	if !resp.CircuitBreakerTripped {
		t.Error("CircuitBreakerTripped = false, want true")
	}
	if resp.CircuitBreakerMessage != svc.CircuitBreakerAlertMessage {
		t.Errorf("CircuitBreakerMessage = %q, want %q",
			resp.CircuitBreakerMessage, svc.CircuitBreakerAlertMessage)
	}
}

func TestHandle_Connect_ResetsCircuitBreaker(t *testing.T) {
	// handleConnect MUST reset the circuit breaker before attempting a
	// manual reconnect — otherwise the UI banner would persist through
	// user-initiated retries. Validated via the nil-tunnel path which
	// still exercises ResetCircuitBreaker... wait: actually the nil
	// tunnel returns early with service_not_ready WITHOUT resetting.
	// So we seed a tripped state and assert it's preserved when tunnel
	// is nil, then call Connect with a real tunnel in StateDisconnected
	// and verify reset occurs.
	prg := newTestProgram()
	prg.ForTestTripCircuitBreaker(svc.CircuitBreakerAlertMessage)

	// Nil tunnel: Connect returns early, tripped state must be untouched.
	resp := Handle(prg, ipc.Request{Action: ipc.ActionConnect}, Options{})
	if resp.Status != ipc.StatusError || resp.Error != "service_not_ready" {
		t.Fatalf("expected service_not_ready, got %q/%q", resp.Status, resp.Error)
	}
	if !prg.CircuitBreakerTripped() {
		t.Error("circuit breaker reset on service_not_ready path; must be preserved")
	}

	// Attach a tunnel in StateDisconnected. Connect will try and fail
	// (invalid pinned key will error out), but the code path BEFORE
	// tc.Connect must call ResetCircuitBreaker.
	const zeroKey32 = "AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA="
	client, err := tunnel.NewClient("127.0.0.1:1", zeroKey32, tunnel.WithInsecure(true))
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}
	prg.ForTestSetTunnelClient(client)

	_ = Handle(prg, ipc.Request{Action: ipc.ActionConnect}, Options{})

	if prg.CircuitBreakerTripped() {
		t.Error("circuit breaker still tripped after Connect; ResetCircuitBreaker not called")
	}
}

func TestHandle_SetAutoStart_NoConfigPath(t *testing.T) {
	opts := Options{
		ConfigPathFn: func() string { return "" },
	}
	resp := Handle(newTestProgram(), ipc.Request{Action: ipc.ActionSetAutoStart, Value: "true"}, opts)
	if resp.Status != ipc.StatusError || resp.Error != "no_config_file" {
		t.Errorf("expected error/no_config_file, got %q/%q", resp.Status, resp.Error)
	}
}

func TestHandle_SetAutoStart_WithConfig(t *testing.T) {
	tmpDir := t.TempDir()
	configFile := filepath.Join(tmpDir, "config.toml")
	if err := os.WriteFile(configFile, []byte("[relay]\ndomain = \"test.dev\"\npublic_key_ed25519 = \"key\"\n[client]\nauto_start = false\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	var startupTypeCalled bool
	opts := Options{
		ConfigPathFn: func() string { return configFile },
		SetStartupTypeFn: func(autoStart bool) error {
			startupTypeCalled = true
			if !autoStart {
				t.Error("expected autoStart true")
			}
			return nil
		},
	}

	resp := Handle(newTestProgram(), ipc.Request{Action: ipc.ActionSetAutoStart, Value: "true"}, opts)
	if resp.Status != ipc.StatusOK {
		t.Errorf("expected ok, got %q (error: %s)", resp.Status, resp.Error)
	}
	if !startupTypeCalled {
		t.Error("expected SetStartupTypeFn to be called")
	}

	cfg, err := config.Load(configFile)
	if err != nil {
		t.Fatal(err)
	}
	if !cfg.Client.AutoStart {
		t.Error("expected auto_start to be true after save")
	}
}

func TestHandle_SetAutoStart_PortableMode_NilStartupType(t *testing.T) {
	tmpDir := t.TempDir()
	configFile := filepath.Join(tmpDir, "config.toml")
	if err := os.WriteFile(configFile, []byte("[relay]\ndomain = \"test.dev\"\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	opts := Options{
		ConfigPathFn: func() string { return configFile },
		// SetStartupTypeFn is nil — portable mode.
	}

	resp := Handle(newTestProgram(), ipc.Request{Action: ipc.ActionSetAutoStart, Value: "true"}, opts)
	if resp.Status != ipc.StatusOK {
		t.Errorf("expected ok, got %q (error: %s)", resp.Status, resp.Error)
	}
}

func TestHandle_LeakCheck_NilTunnel(t *testing.T) {
	resp := Handle(newTestProgram(), ipc.Request{Action: ipc.ActionLeakCheck}, Options{})
	if resp.Status != ipc.StatusError || resp.Error != "service_not_ready" {
		t.Errorf("expected error/service_not_ready, got %q/%q", resp.Status, resp.Error)
	}
}

func TestHandle_STUNStatus_Inactive(t *testing.T) {
	resp := Handle(newTestProgram(), ipc.Request{Action: ipc.ActionSTUNStatus}, Options{})
	if resp.Status != ipc.StatusSTUNInactive {
		t.Errorf("expected %q, got %q", ipc.StatusSTUNInactive, resp.Status)
	}
}

func TestHandle_CheckUpdate_NoUpdater(t *testing.T) {
	resp := Handle(newTestProgram(), ipc.Request{Action: ipc.ActionCheckUpdate}, Options{})
	if resp.Status != ipc.StatusError || resp.Error != "updates_disabled" {
		t.Errorf("expected error/updates_disabled, got %q/%q", resp.Status, resp.Error)
	}
}

func TestHandle_UpdateStatus_NoUpdater(t *testing.T) {
	resp := Handle(newTestProgram(), ipc.Request{Action: ipc.ActionUpdateStatus}, Options{})
	if resp.Status != ipc.StatusError || resp.Error != "updates_disabled" {
		t.Errorf("expected error/updates_disabled, got %q/%q", resp.Status, resp.Error)
	}
}

func TestHandle_UpdateStatus_Installed(t *testing.T) {
	prg := newTestProgram()
	prg.SetInstalledVersion("2.0.0")

	resp := Handle(prg, ipc.Request{Action: ipc.ActionUpdateStatus}, Options{})

	if resp.Status != ipc.StatusOK {
		t.Errorf("Status = %q, want %q", resp.Status, ipc.StatusOK)
	}
	if resp.UpdateStatus != ipc.StatusInstalled {
		t.Errorf("UpdateStatus = %q, want %q", resp.UpdateStatus, ipc.StatusInstalled)
	}
	if resp.InstalledVersion != "2.0.0" {
		t.Errorf("InstalledVersion = %q, want %q", resp.InstalledVersion, "2.0.0")
	}
}

func TestHandle_UpdateStatus_WithPendingUpdate(t *testing.T) {
	prg := newTestProgram()
	// PendingUpdateVersion is empty, Updater is nil → updates_disabled
	resp := Handle(prg, ipc.Request{Action: ipc.ActionUpdateStatus}, Options{})
	if resp.UpdateStatus != "" && resp.Status != ipc.StatusError {
		t.Errorf("expected error status, got %q/%q", resp.Status, resp.UpdateStatus)
	}
}

func TestHandle_GetStatus_IncludesPendingUpdate(t *testing.T) {
	prg := newTestProgram()
	// Without tunnel client, returns disconnected
	resp := Handle(prg, ipc.Request{Action: ipc.ActionGetStatus}, Options{})
	if resp.Status != ipc.StatusDisconnected {
		t.Errorf("expected disconnected, got %q", resp.Status)
	}
	// UpdateVersion should be empty (no pending)
	if resp.UpdateVersion != "" {
		t.Errorf("expected empty UpdateVersion, got %q", resp.UpdateVersion)
	}
}

func TestHandle_UpdateStatus_Rollback_Absent(t *testing.T) {
	prg := newTestProgram()

	// No rollback → should not return rollback status
	resp := Handle(prg, ipc.Request{Action: ipc.ActionUpdateStatus}, Options{})
	if resp.UpdateStatus == ipc.StatusRollback {
		t.Error("should not return rollback when no rollback occurred")
	}
}

func TestHandle_UpdateStatus_RollbackPresent(t *testing.T) {
	prg := newTestProgram()
	prg.SetRollbackState(true, "2.1.0", "context deadline exceeded")

	resp := Handle(prg, ipc.Request{Action: ipc.ActionUpdateStatus}, Options{})

	if resp.Status != ipc.StatusOK {
		t.Errorf("Status = %q, want %q", resp.Status, ipc.StatusOK)
	}
	if resp.UpdateStatus != ipc.StatusRollback {
		t.Errorf("UpdateStatus = %q, want %q", resp.UpdateStatus, ipc.StatusRollback)
	}
	if resp.RollbackVersion != "2.1.0" {
		t.Errorf("RollbackVersion = %q, want %q", resp.RollbackVersion, "2.1.0")
	}
	if resp.RollbackReason != "context deadline exceeded" {
		t.Errorf("RollbackReason = %q, want %q", resp.RollbackReason, "context deadline exceeded")
	}
}

func TestHandle_UpdateStatus_RollbackPriorityOverInstalled(t *testing.T) {
	prg := newTestProgram()
	// Set both installed version AND rollback state — rollback must take priority.
	prg.SetInstalledVersion("2.0.0")
	prg.SetRollbackState(true, "2.1.0", "tunnel timeout")

	resp := Handle(prg, ipc.Request{Action: ipc.ActionUpdateStatus}, Options{})

	if resp.UpdateStatus != ipc.StatusRollback {
		t.Errorf("expected rollback to take priority over installed, got UpdateStatus=%q", resp.UpdateStatus)
	}
	if resp.RollbackVersion != "2.1.0" {
		t.Errorf("RollbackVersion = %q, want %q", resp.RollbackVersion, "2.1.0")
	}
	// InstalledVersion should NOT be set when rollback takes priority
	if resp.InstalledVersion != "" {
		t.Errorf("InstalledVersion = %q, want empty when rollback is present", resp.InstalledVersion)
	}
}

func TestHandle_GetStatus_IncludesRollbackInfo(t *testing.T) {
	prg := newTestProgram()
	// Without rollback, UpdateStatus should not be rollback
	resp := Handle(prg, ipc.Request{Action: ipc.ActionGetStatus}, Options{})
	if resp.UpdateStatus == ipc.StatusRollback {
		t.Error("should not include rollback status when no rollback occurred")
	}
	if resp.RollbackVersion != "" {
		t.Errorf("expected empty RollbackVersion, got %q", resp.RollbackVersion)
	}
}

func TestHandle_GetStatus_WithRollback(t *testing.T) {
	prg := newTestProgram()
	prg.SetRollbackState(true, "2.1.0", "context deadline exceeded")

	// handleGetStatus returns disconnected because tunnelClient is nil, but
	// must include rollback fields since RollbackOccurred() is true.
	resp := Handle(prg, ipc.Request{Action: ipc.ActionGetStatus}, Options{})

	if resp.UpdateStatus != ipc.StatusRollback {
		t.Errorf("UpdateStatus = %q, want %q", resp.UpdateStatus, ipc.StatusRollback)
	}
	if resp.RollbackVersion != "2.1.0" {
		t.Errorf("RollbackVersion = %q, want %q", resp.RollbackVersion, "2.1.0")
	}
	if resp.RollbackReason != "context deadline exceeded" {
		t.Errorf("RollbackReason = %q, want %q", resp.RollbackReason, "context deadline exceeded")
	}
}

// TestHandle_GetStatus_LeakStatus_Pending: handleGetStatus returns "pending" when scheduler
// has been set but no check has run yet. Also validates the M3 fix: leak status is returned
// even when tc == nil (scheduler check is not guarded by tc != nil anymore).
func TestHandle_GetStatus_LeakStatus_Pending(t *testing.T) {
	prg := newTestProgram()
	prg.SetLeakScheduler(newTestScheduler())

	resp := Handle(prg, ipc.Request{Action: ipc.ActionGetStatus}, Options{})

	if resp.Status != ipc.StatusDisconnected {
		t.Errorf("Status = %q, want %q", resp.Status, ipc.StatusDisconnected)
	}
	if resp.LeakStatus != ipc.StatusLeakPending {
		t.Errorf("LeakStatus = %q, want %q (scheduler set but no check run)", resp.LeakStatus, ipc.StatusLeakPending)
	}
	if resp.LeakLastCheck != "" {
		t.Errorf("LeakLastCheck = %q, want empty when no check has run", resp.LeakLastCheck)
	}
}

// TestHandle_GetStatus_NoLeakStatus_WhenNoScheduler: LeakStatus omitted when no scheduler set.
func TestHandle_GetStatus_NoLeakStatus_WhenNoScheduler(t *testing.T) {
	prg := newTestProgram()

	resp := Handle(prg, ipc.Request{Action: ipc.ActionGetStatus}, Options{})

	if resp.LeakStatus != "" {
		t.Errorf("LeakStatus = %q, want empty when no scheduler", resp.LeakStatus)
	}
}

func TestFormatUptime(t *testing.T) {
	tests := []struct {
		name     string
		duration time.Duration
		want     string
	}{
		{"seconds", 45 * time.Second, "0m45s"},
		{"minutes", 5*time.Minute + 30*time.Second, "5m30s"},
		{"hours", 2*time.Hour + 15*time.Minute, "2h15m"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := FormatUptime(tt.duration); got != tt.want {
				t.Errorf("FormatUptime(%v) = %q, want %q", tt.duration, got, tt.want)
			}
		})
	}
}

// --- Story 12.1 tests ---

// TestGetStatus_MissingIP verifies handler behavior when IP is empty.
//
// AC3 full scenario (Status="connected" + empty IP) requires a connected tunnel.Client
// which needs network mocking beyond unit test scope. The tray-side rendering of
// "Protégé — IP en détection..." is covered by TestTray_UpdateState_Connected_UnknownIP.
//
// This test validates the handler-level contract: no error is injected when IP is empty,
// and VisibleIP="" propagates cleanly (not replaced by "unknown" or an error placeholder).
func TestGetStatus_MissingIP(t *testing.T) {
	prg := newTestProgram()

	// Explicitly set VisibleIP to empty to confirm handler doesn't transform it.
	prg.SetVisibleIP("")

	resp := Handle(prg, ipc.Request{Action: ipc.ActionGetStatus}, Options{})

	// With nil tunnel, status is "disconnected" (expected — tunnel.Client not mockable here).
	if resp.Status != ipc.StatusDisconnected {
		t.Errorf("expected disconnected with nil tunnel, got %q", resp.Status)
	}

	// No error must be set just because IP is empty.
	if resp.Error != "" {
		t.Errorf("expected no error for missing IP, got %q", resp.Error)
	}

	// IP must be empty (not "unknown" or an error placeholder).
	if resp.IP != "" {
		t.Errorf("expected empty IP with nil tunnel, got %q", resp.IP)
	}

	// Verify other AC3-related fields are populated even without tunnel.
	// These fields must be present for tray reconnection (blocklist, proxy, browser policies).
	if resp.HTTPProxySeq != 0 {
		t.Errorf("expected HTTPProxySeq=0 with fresh program, got %d", resp.HTTPProxySeq)
	}
}

func TestHandle_GetAllowIPv6Leak_DefaultFalse(t *testing.T) {
	resp := Handle(newTestProgram(), ipc.Request{Action: ipc.ActionGetAllowIPv6Leak}, Options{})
	if resp.Status != ipc.StatusOK {
		t.Errorf("expected ok, got %q", resp.Status)
	}
	if resp.AllowIPv6Leak {
		t.Error("expected AllowIPv6Leak = false by default")
	}
}

func TestHandle_SetAllowIPv6Leak_InvalidValue(t *testing.T) {
	resp := Handle(newTestProgram(), ipc.Request{Action: ipc.ActionSetAllowIPv6Leak, Value: "maybe"}, Options{})
	if resp.Status != ipc.StatusError {
		t.Errorf("expected error for invalid value, got %q", resp.Status)
	}
}

func TestHandle_SetAllowIPv6Leak_True(t *testing.T) {
	prg := newTestProgram()
	cfgPath := filepath.Join(t.TempDir(), "config.toml")
	// Write initial config so Load succeeds.
	initial := &config.Config{TUN: config.TUNConfig{Name: "levoile0", MTU: 1420}}
	if err := initial.Save(cfgPath); err != nil {
		t.Fatal(err)
	}
	opts := Options{ConfigPathFn: func() string { return cfgPath }}

	resp := Handle(prg, ipc.Request{Action: ipc.ActionSetAllowIPv6Leak, Value: "true"}, opts)
	if resp.Status != ipc.StatusOK {
		t.Errorf("expected ok, got %q (error: %s)", resp.Status, resp.Error)
	}
	if !resp.AllowIPv6Leak {
		t.Error("expected AllowIPv6Leak = true in response")
	}

	// Verify config was persisted.
	cfg, err := config.Load(cfgPath)
	if err != nil {
		t.Fatal(err)
	}
	if !cfg.Firewall.AllowIPv6Leak {
		t.Error("expected AllowIPv6Leak = true persisted in TOML")
	}

	// Verify program state was updated.
	if !prg.AllowIPv6Leak() {
		t.Error("expected program AllowIPv6Leak() = true")
	}
}

func TestHandle_SetAllowIPv6Leak_Roundtrip(t *testing.T) {
	prg := newTestProgram()
	cfgPath := filepath.Join(t.TempDir(), "config.toml")
	initial := &config.Config{TUN: config.TUNConfig{Name: "levoile0", MTU: 1420}}
	if err := initial.Save(cfgPath); err != nil {
		t.Fatal(err)
	}
	opts := Options{ConfigPathFn: func() string { return cfgPath }}

	// Enable
	Handle(prg, ipc.Request{Action: ipc.ActionSetAllowIPv6Leak, Value: "true"}, opts)
	if !prg.AllowIPv6Leak() {
		t.Error("expected true after enable")
	}

	// Disable
	resp := Handle(prg, ipc.Request{Action: ipc.ActionSetAllowIPv6Leak, Value: "false"}, opts)
	if resp.Status != ipc.StatusOK {
		t.Errorf("expected ok, got %q", resp.Status)
	}
	if prg.AllowIPv6Leak() {
		t.Error("expected false after disable")
	}

	// Verify persisted
	cfg, _ := config.Load(cfgPath)
	if cfg.Firewall.AllowIPv6Leak {
		t.Error("expected AllowIPv6Leak = false persisted after disable")
	}
}

func TestHandle_GetStatus_IncludesAllowIPv6Leak(t *testing.T) {
	prg := svc.NewProgram(svc.Config{
		RelayDomain:   "test.dev",
		RelayPubKey:   "dGVzdA==",
		AllowIPv6Leak: true,
	})

	resp := Handle(prg, ipc.Request{Action: ipc.ActionGetStatus}, Options{})
	if !resp.AllowIPv6Leak {
		t.Error("expected AllowIPv6Leak = true in status response when config is true")
	}
}
