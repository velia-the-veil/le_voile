//go:build windows

package ipchandler

import (
	"testing"

	"github.com/velia-the-veil/le_voile/windows/internal/ipc"
)

// TestHandle_GetUISupervision_NoWatchdog verifies the handler returns OK
// with a nil UISupervision payload when the program has no UI watchdog
// (Linux installs, or Windows installs with the feature disabled). This
// is the contract the UI relies on to distinguish "no data" from "watchdog
// idle" — Story 5.7 AC7.
func TestHandle_GetUISupervision_NoWatchdog(t *testing.T) {
	resp := Handle(newTestProgram(), ipc.Request{Action: ipc.ActionGetUISupervision}, Options{})
	if resp.Status != ipc.StatusOK {
		t.Errorf("expected ok, got %q (err=%q)", resp.Status, resp.Error)
	}
	if resp.UISupervision != nil {
		t.Errorf("expected nil UISupervision, got %+v", resp.UISupervision)
	}
}

// TestHandle_GetStatus_NoWatchdog_OmitsField confirms GetStatus does not
// surface a non-nil UISupervision when the watchdog is absent — keeps the
// JSON payload clean for Linux clients.
func TestHandle_GetStatus_NoWatchdog_OmitsField(t *testing.T) {
	resp := Handle(newTestProgram(), ipc.Request{Action: ipc.ActionGetStatus}, Options{})
	if resp.UISupervision != nil {
		t.Errorf("expected nil UISupervision on GetStatus, got %+v", resp.UISupervision)
	}
}
