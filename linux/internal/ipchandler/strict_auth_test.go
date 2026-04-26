//go:build linux

package ipchandler

import (
	"testing"

	"github.com/velia-the-veil/le_voile/linux/internal/ipc"
)

// TestLegacyAuth_Default_Strict covers the 2026-04 flip: by default empty
// req.Auth on a mutating action is rejected — legacyEmptyAuthAllowed must
// return false when LEVOILE_IPC_LEGACY_AUTH is unset. t.Setenv("", "")
// is not a thing, so we use the empty-string override which is still
// falsy under our ==1 check. The parent TestMain sets LEGACY=1 for the
// package; t.Setenv scopes this override to the current test only.
func TestLegacyAuth_Default_Strict(t *testing.T) {
	t.Setenv("LEVOILE_IPC_LEGACY_AUTH", "")
	if legacyEmptyAuthAllowed() {
		t.Fatal("legacyEmptyAuthAllowed = true when env is empty — default must be strict")
	}
}

// TestLegacyAuth_Opt_In : setting LEVOILE_IPC_LEGACY_AUTH=1 re-enables the
// pre-2026-04 contract where mutating actions with empty Auth still
// proceeded (for hosts stuck on an old UI build that cannot send a token).
func TestLegacyAuth_Opt_In(t *testing.T) {
	t.Setenv("LEVOILE_IPC_LEGACY_AUTH", "1")
	if !legacyEmptyAuthAllowed() {
		t.Fatal("legacyEmptyAuthAllowed = false with env=1")
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
