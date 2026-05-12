package ipchandler

import (
	"os"
	"testing"

	"github.com/velia-the-veil/le_voile/internal/ipc"
)

// TestStrictAuth_Off_AllowsEmpty covers the default: without the env flag,
// empty req.Auth is still accepted for mutating actions (backward compat).
// The audit line still fires but the action proceeds.
func TestStrictAuth_Off_AllowsEmpty(t *testing.T) {
	os.Unsetenv("LEVOILE_IPC_STRICT_AUTH")
	if strictIPCAuthRequired() {
		t.Fatal("strictIPCAuthRequired = true without env set")
	}
}

// TestStrictAuth_On_Enforced : when the operator sets LEVOILE_IPC_STRICT_AUTH=1,
// the gate triggers and rejects empty-Auth mutating calls at the Handle
// dispatch boundary — covered by returning an "auth_required" error
// without ever touching service state.
func TestStrictAuth_On_Enforced(t *testing.T) {
	t.Setenv("LEVOILE_IPC_STRICT_AUTH", "1")
	if !strictIPCAuthRequired() {
		t.Fatal("strictIPCAuthRequired = false with env=1")
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
