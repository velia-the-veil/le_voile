//go:build windows

package ipchandler

import (
	"testing"

	"github.com/velia-the-veil/le_voile/windows/internal/ipc"
)

// Story 6.3 — Task 4. TestHandle_GetStatus_AnomalyIdle asserts that the
// get_status response carries both AnomalyActive=false and
// AnomalyReason="" when no recovery is in flight. Used as the baseline
// the frontend polling loop expects between anomalies.
func TestHandle_GetStatus_AnomalyIdle(t *testing.T) {
	prg := newTestProgram()
	resp := Handle(prg, ipc.Request{Action: ipc.ActionGetStatus}, Options{})
	if resp.AnomalyActive {
		t.Errorf("AnomalyActive=true for idle program; want false")
	}
	if resp.AnomalyReason != "" {
		t.Errorf("AnomalyReason=%q for idle program; want empty", resp.AnomalyReason)
	}
}

// TestHandle_GetStatus_AnomalyActive drives the fields via the Program's
// test-only anomaly setters. Because Program.AnomalyActive is backed by
// an atomic.Bool and AnomalyReason by atomic.Pointer[string], we can
// reach into them directly through the ForTest seam exposed for this
// purpose.
func TestHandle_GetStatus_AnomalyActive(t *testing.T) {
	prg := newTestProgram()
	prg.ForTestSetAnomaly(true, "leak_detected")

	resp := Handle(prg, ipc.Request{Action: ipc.ActionGetStatus}, Options{})
	if !resp.AnomalyActive {
		t.Errorf("AnomalyActive=false; want true")
	}
	if resp.AnomalyReason != "leak_detected" {
		t.Errorf("AnomalyReason=%q; want leak_detected", resp.AnomalyReason)
	}
}

// TestHandle_GetStatus_AnomalyTunAltered covers the second recovery
// trigger so we know both reason codes round-trip through the IPC
// response intact.
func TestHandle_GetStatus_AnomalyTunAltered(t *testing.T) {
	prg := newTestProgram()
	prg.ForTestSetAnomaly(true, "tun_altered")

	resp := Handle(prg, ipc.Request{Action: ipc.ActionGetStatus}, Options{})
	if !resp.AnomalyActive || resp.AnomalyReason != "tun_altered" {
		t.Errorf("unexpected (active=%v reason=%q); want (true tun_altered)",
			resp.AnomalyActive, resp.AnomalyReason)
	}
}

// TestHandle_GetStatus_AnomalyClearedReflectsInResponse verifies the
// state transition path: set, then clear, and confirm the IPC response
// moves back to idle without residual reason.
func TestHandle_GetStatus_AnomalyClearedReflectsInResponse(t *testing.T) {
	prg := newTestProgram()
	prg.ForTestSetAnomaly(true, "manual")
	prg.ForTestSetAnomaly(false, "")

	resp := Handle(prg, ipc.Request{Action: ipc.ActionGetStatus}, Options{})
	if resp.AnomalyActive {
		t.Errorf("AnomalyActive stuck true after clear")
	}
	if resp.AnomalyReason != "" {
		t.Errorf("AnomalyReason=%q after clear; want empty", resp.AnomalyReason)
	}
}
