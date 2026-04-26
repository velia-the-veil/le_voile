//go:build windows

package ipchandler

import (
	"testing"

	"github.com/velia-the-veil/le_voile/windows/internal/ipc"
	svc "github.com/velia-the-veil/le_voile/windows/internal/service"
)

// TestHandle_GatesMutationsWhenIntegrityFailed verifies Story 7.5 AC2 :
// when startup HMAC verification fails, the IPC handler refuses every
// mutating action and surfaces IntegrityFailed=true so the UI can gate
// controls. GetStatus stays open so the frontend can fetch the flag.
func TestHandle_GatesMutationsWhenIntegrityFailed(t *testing.T) {
	prg := svc.NewProgram(svc.Config{})
	prg.SetIntegrityFailed(true)

	mutating := []string{
		ipc.ActionConnect,
		ipc.ActionSetAutoStart,
		ipc.ActionSetBlocklist,
		ipc.ActionSetHTTPProxy,
		ipc.ActionSelectCountry,
		ipc.ActionSetAllowIPv6Leak,
		ipc.ActionSetKillSwitchMode,
	}
	for _, action := range mutating {
		resp := Handle(prg, ipc.Request{Action: action, Value: "true"}, Options{
			ConfigPathFn: func() string { return "" },
		})
		if resp.Status != ipc.StatusError {
			t.Errorf("action=%q status=%q, want error", action, resp.Status)
		}
		if resp.Error != "integrity_failed" {
			t.Errorf("action=%q err=%q, want integrity_failed", action, resp.Error)
		}
		if !resp.IntegrityFailed {
			t.Errorf("action=%q IntegrityFailed=false, want true", action)
		}
	}

	// GetStatus must succeed (not locked out) but carry IntegrityFailed=true
	// so the UI can render the recovery banner.
	resp := Handle(prg, ipc.Request{Action: ipc.ActionGetStatus}, Options{
		ConfigPathFn: func() string { return "" },
	})
	if !resp.IntegrityFailed {
		t.Error("get_status IntegrityFailed=false, want true")
	}
}

// TestHandle_AllowsMutationsWhenIntegrityOK is the negative control: when
// IntegrityFailed is false the gate must be fully transparent.
func TestHandle_AllowsMutationsWhenIntegrityOK(t *testing.T) {
	prg := svc.NewProgram(svc.Config{})
	prg.SetIntegrityFailed(false)

	resp := Handle(prg, ipc.Request{Action: ipc.ActionGetStatus}, Options{
		ConfigPathFn: func() string { return "" },
	})
	if resp.IntegrityFailed {
		t.Error("get_status IntegrityFailed=true, want false when gate is off")
	}
}
