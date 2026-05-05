//go:build windows

package ipchandler

import (
	"testing"

	"github.com/velia-the-veil/le_voile/windows/internal/ipc"
)

// TestStrictAuth_Default rejects empty-Auth mutating requests when the test
// bypass is off. This mirrors the production posture: production binaries
// observe testBypassAuthGate=false unconditionally because the variable is
// package-internal (audit fix R-T1, 2026-05-04).
func TestStrictAuth_Default(t *testing.T) {
	prev := testBypassAuthGate
	testBypassAuthGate = false
	t.Cleanup(func() { testBypassAuthGate = prev })

	resp := Handle(newTestProgram(), ipc.Request{Action: ipc.ActionConnect}, Options{})
	if resp.Status != ipc.StatusError || resp.Error != "auth_required" {
		t.Fatalf("strict mode: empty-Auth Connect should be auth_required, got status=%v err=%q",
			resp.Status, resp.Error)
	}
}

// TestStrictAuth_BypassFlag covers the test-only escape hatch that the rest
// of this package's tests rely on. Production binaries never reach this
// state because testBypassAuthGate is package-internal. GetStatus is a
// read-only action so the gate is irrelevant for it; the discriminator is
// that an empty-Auth Connect (mutating) is accepted instead of being
// rejected with "auth_required".
func TestStrictAuth_BypassFlag(t *testing.T) {
	prev := testBypassAuthGate
	testBypassAuthGate = true
	t.Cleanup(func() { testBypassAuthGate = prev })

	resp := Handle(newTestProgram(), ipc.Request{Action: ipc.ActionConnect}, Options{})
	if resp.Error == "auth_required" {
		t.Fatalf("bypass mode: empty-Auth Connect should not be auth_required, got status=%v err=%q",
			resp.Status, resp.Error)
	}
}

// TestIsMutatingAction covers the list used by the strict-auth gate. If a
// new mutating action is added without being listed here, it escapes the
// gate — breaking this test is the expected "updated the action set,
// please reconsider auth requirements" signal.
func TestIsMutatingAction(t *testing.T) {
	mustMutate := []string{
		ipc.ActionConnect,
		ipc.ActionDisconnect,
		ipc.ActionQuit,
		ipc.ActionUIDisconnect,
		ipc.ActionSetAutoStart,
		ipc.ActionSetBlocklist,
		ipc.ActionSetHTTPProxy,
		ipc.ActionSelectCountry,
		ipc.ActionRetryCaptive,
		ipc.ActionSetAllowIPv6Leak,
		ipc.ActionSetKillSwitchMode,
		ipc.ActionTriggerRecovery,
	}
	for _, a := range mustMutate {
		if !isMutatingAction(a) {
			t.Errorf("isMutatingAction(%q) = false, want true", a)
		}
	}

	readOnly := []string{
		ipc.ActionGetStatus,
		ipc.ActionLeakCheck,
		ipc.ActionCheckUpdate,
		ipc.ActionUpdateStatus,
		ipc.ActionGetRegistry,
		ipc.ActionGetAllowIPv6Leak,
		ipc.ActionGetUISupervision,
		ipc.ActionGetKillSwitchMode,
	}
	for _, a := range readOnly {
		if isMutatingAction(a) {
			t.Errorf("isMutatingAction(%q) = true, want false (read-only)", a)
		}
	}
}
