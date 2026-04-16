//go:build windows

package firewall

import (
	"context"
	"errors"
	"net"
	"testing"
	"unsafe"
)

// --- Struct layout verification (catch alignment bugs early) ---

func TestStructSizes(t *testing.T) {
	// Verify key WFP struct sizes match expected amd64 layouts.
	tests := []struct {
		name     string
		got      uintptr
		wantMin  uintptr // minimum size (exact depends on alignment)
	}{
		{"fwpmDisplayData0", unsafe.Sizeof(fwpmDisplayData0{}), 16},
		{"fwpByteBlob", unsafe.Sizeof(fwpByteBlob{}), 16},
		{"fwpValue0", unsafe.Sizeof(fwpValue0{}), 16},
		{"fwpmFilterCondition0", unsafe.Sizeof(fwpmFilterCondition0{}), 40},
		{"fwpmAction0", unsafe.Sizeof(fwpmAction0{}), 20},
		{"fwpmProvider0", unsafe.Sizeof(fwpmProvider0{}), 56},
		{"fwpmSublayer0", unsafe.Sizeof(fwpmSublayer0{}), 64},
		{"fwpmFilter0", unsafe.Sizeof(fwpmFilter0{}), 200},
		{"fwpmFilterEnumTemplate0", unsafe.Sizeof(fwpmFilterEnumTemplate0{}), 72},
	}
	for _, tt := range tests {
		if tt.got < tt.wantMin {
			t.Errorf("%s: size %d < expected minimum %d", tt.name, tt.got, tt.wantMin)
		}
	}
}

// --- Unit tests (no WFP engine required) ---

func TestNew_ReturnsWFPFirewall(t *testing.T) {
	fw := New(nil, Options{})
	if fw == nil {
		t.Fatal("New returned nil")
	}
	if _, ok := fw.(*wfpFirewall); !ok {
		t.Fatalf("expected *wfpFirewall, got %T", fw)
	}
}

func TestNew_WithOptions(t *testing.T) {
	fw := New(nil, Options{AllowIPv6Leak: true})
	wfp := fw.(*wfpFirewall)
	if !wfp.opts.AllowIPv6Leak {
		t.Error("AllowIPv6Leak not propagated")
	}
}

func TestAlteredCh_NonNil(t *testing.T) {
	fw := New(nil, Options{})
	ch := fw.AlteredCh()
	if ch == nil {
		t.Error("AlteredCh() returned nil on Windows — expected non-nil channel")
	}
}

func TestCheckPrerequisites_WFPAvailable(t *testing.T) {
	// fwpuclnt.dll should be available on any Windows 7+ system.
	err := checkPrerequisites()
	// May fail with ErrNotElevated when not running as admin — that's fine.
	if errors.Is(err, ErrWFPUnavailable) {
		t.Fatal("fwpuclnt.dll should be available on Windows 7+")
	}
}

func TestDeactivate_IdempotentWhenNotActive(t *testing.T) {
	// TC-2.7.5: Deactivate should succeed even if never activated.
	fw := New(nil, Options{})
	ctx := context.Background()

	// First call — nothing to deactivate.
	err := fw.Deactivate(ctx)
	if err != nil {
		// May fail with elevation error — skip in that case.
		if errors.Is(err, ErrNotElevated) {
			t.Skip("requires elevation")
		}
		t.Fatalf("first Deactivate: %v", err)
	}

	// Second call — should be idempotent.
	err = fw.Deactivate(ctx)
	if err != nil {
		t.Fatalf("second Deactivate: %v", err)
	}
}

func TestIsActive_FalseWhenNotActivated(t *testing.T) {
	fw := New(nil, Options{})
	ctx := context.Background()

	active, err := fw.IsActive(ctx)
	if err != nil {
		if errors.Is(err, ErrNotElevated) {
			t.Skip("requires elevation")
		}
		t.Fatalf("IsActive: %v", err)
	}
	if active {
		// Clean up unexpected orphans.
		_, _ = fw.CleanupOrphans(ctx)
		t.Fatal("IsActive=true without prior Activate — possible orphan filters")
	}
}

func TestCleanupOrphans_NoopWhenClean(t *testing.T) {
	fw := New(nil, Options{})
	ctx := context.Background()

	n, err := fw.CleanupOrphans(ctx)
	if err != nil {
		if errors.Is(err, ErrNotElevated) {
			t.Skip("requires elevation")
		}
		t.Fatalf("CleanupOrphans: %v", err)
	}
	if n != 0 {
		t.Errorf("expected 0 orphans, got %d", n)
	}
}

func TestInterfaceLUID_InvalidName(t *testing.T) {
	_, err := interfaceLUID("nonexistent_interface_xyz")
	if err == nil {
		t.Fatal("expected error for non-existent interface")
	}
}

func TestGUIDConstants_Stable(t *testing.T) {
	// Verify provider/sublayer GUIDs are non-zero (catch accidental zeroing).
	if leVoileProviderKey.Data1 == 0 {
		t.Error("provider GUID Data1 is zero")
	}
	if leVoileSublayerKey.Data1 == 0 {
		t.Error("sublayer GUID Data1 is zero")
	}
	// Verify they differ.
	if leVoileProviderKey == leVoileSublayerKey {
		t.Error("provider and sublayer GUIDs must differ")
	}
}

// --- Integration tests (require elevation) ---
// These are guarded by the "integration" build tag. Run with:
//   go test -tags integration ./internal/firewall/ -run TestIntegrationWFP -v
// Must run as LocalSystem or Administrator.

func TestIntegrationWFP_ActivateDeactivate(t *testing.T) {
	if !isElevated() {
		t.Skip("requires admin/LocalSystem elevation")
	}

	ctx := context.Background()
	log := &testLoggerWin{}
	fw := New(log, Options{})

	// Ensure clean state.
	_, _ = fw.CleanupOrphans(ctx)

	// Create a dummy loopback-like interface name. The test uses "Loopback Pseudo-Interface 1"
	// which always exists on Windows. However, Activate expects a TUN interface
	// that may not exist in CI. Instead, test the full lifecycle only if levoile0 exists.
	tunName := "levoile0"
	if _, err := interfaceLUID(tunName); err != nil {
		t.Skipf("TUN interface %s not available: %v", tunName, err)
	}

	relayIP := net.ParseIP("198.51.100.42")

	// TC-2.7.1: Activate → IsActive() == true
	if err := fw.Activate(ctx, ActivateParams{Mode: ModeFull, RelayIP: relayIP, TunName: tunName}); err != nil {
		t.Fatalf("Activate: %v", err)
	}
	defer fw.Deactivate(ctx)

	active, err := fw.IsActive(ctx)
	if err != nil {
		t.Fatalf("IsActive: %v", err)
	}
	if !active {
		t.Error("expected IsActive=true after Activate")
	}

	// TC-2.7.5: Deactivate idempotent
	if err := fw.Deactivate(ctx); err != nil {
		t.Fatalf("Deactivate: %v", err)
	}
	if err := fw.Deactivate(ctx); err != nil {
		t.Fatalf("second Deactivate: %v", err)
	}

	active, err = fw.IsActive(ctx)
	if err != nil {
		t.Fatalf("IsActive after Deactivate: %v", err)
	}
	if active {
		t.Error("expected IsActive=false after Deactivate")
	}
}

func TestIntegrationWFP_CrashRecovery(t *testing.T) {
	if !isElevated() {
		t.Skip("requires admin/LocalSystem elevation")
	}

	ctx := context.Background()
	fw := New(nil, Options{})

	// Ensure clean state.
	_, _ = fw.CleanupOrphans(ctx)

	tunName := "levoile0"
	if _, err := interfaceLUID(tunName); err != nil {
		t.Skipf("TUN interface %s not available: %v", tunName, err)
	}

	// Activate and "crash" (don't Deactivate).
	if err := fw.Activate(ctx, ActivateParams{Mode: ModeFull, RelayIP: net.ParseIP("198.51.100.42"), TunName: tunName}); err != nil {
		t.Fatalf("Activate: %v", err)
	}

	// TC-2.7.4: Simulate crash — create new instance, cleanup orphans.
	fw2 := New(nil, Options{})
	n, err := fw2.CleanupOrphans(ctx)
	if err != nil {
		t.Fatalf("CleanupOrphans: %v", err)
	}
	if n == 0 {
		t.Error("expected orphan filters after simulated crash")
	}

	// Verify clean state.
	active, err := fw2.IsActive(ctx)
	if err != nil {
		t.Fatalf("IsActive after cleanup: %v", err)
	}
	if active {
		t.Error("expected IsActive=false after CleanupOrphans")
	}
}

// --- Helpers ---

func isElevated() bool {
	err := checkPrerequisites()
	return err == nil
}

type testLoggerWin struct {
	infos  []string
	warns  []string
	errs   []string
}

func (l *testLoggerWin) Infof(format string, args ...any)  { l.infos = append(l.infos, format) }
func (l *testLoggerWin) Warnf(format string, args ...any)  { l.warns = append(l.warns, format) }
func (l *testLoggerWin) Errorf(format string, args ...any) { l.errs = append(l.errs, format) }
func (l *testLoggerWin) Debugf(format string, args ...any) {}
