//go:build windows

package ipchandler

import (
	"testing"
	"time"

	"github.com/velia-the-veil/le_voile/windows/internal/ipc"
	svc "github.com/velia-the-veil/le_voile/windows/internal/service"
	"github.com/velia-the-veil/le_voile/internal/tunnel"
)

// Story 6.3 — Task 8. handleTriggerRecovery must reject requests that
// don't carry a valid machine-local ctl token. Since the 2026-04 strict
// default, an empty-Auth mutating request is rejected at the global gate
// with "auth_required" before reaching the action-specific "auth_failed"
// check — both outcomes are acceptable (the request never touches state).
func TestHandle_TriggerRecovery_NoAuth_Rejected(t *testing.T) {
	prg := svc.NewProgram(svc.Config{})
	prg.SetCtlToken([]byte("token-32-bytes-secret-aaaaaaaaaa"))

	resp := Handle(prg, ipc.Request{Action: ipc.ActionTriggerRecovery}, Options{})
	if resp.Status != ipc.StatusError {
		t.Fatalf("Status = %q; want error", resp.Status)
	}
	if resp.Error != "auth_required" && resp.Error != "auth_failed" {
		t.Errorf("Error = %q; want auth_required or auth_failed", resp.Error)
	}
}

func TestHandle_TriggerRecovery_WrongAuth_Rejected(t *testing.T) {
	prg := svc.NewProgram(svc.Config{})
	prg.SetCtlToken([]byte("token-32-bytes-secret-aaaaaaaaaa"))

	resp := Handle(prg, ipc.Request{
		Action: ipc.ActionTriggerRecovery,
		Auth:   "wrong-token",
	}, Options{})
	if resp.Status != ipc.StatusError || resp.Error != "auth_failed" {
		t.Errorf("wrong token accepted: %q/%q", resp.Status, resp.Error)
	}
}

// TestHandle_TriggerRecovery_NoTunnel_Rejected: authentication succeeds
// but there is no tunnel to recover (Program.TunnelClient == nil).
func TestHandle_TriggerRecovery_NoTunnel_Rejected(t *testing.T) {
	prg := svc.NewProgram(svc.Config{})
	prg.SetCtlToken([]byte("token-32-bytes-secret-aaaaaaaaaa"))

	resp := Handle(prg, ipc.Request{
		Action: ipc.ActionTriggerRecovery,
		Auth:   "token-32-bytes-secret-aaaaaaaaaa",
	}, Options{})
	if resp.Error != "tunnel_not_connected" {
		t.Errorf("Error = %q; want tunnel_not_connected", resp.Error)
	}
}

// TestHandle_TriggerRecovery_TunnelDisconnected_Rejected: tunnel exists
// but is not in StateConnected. Recovery would be meaningless.
func TestHandle_TriggerRecovery_TunnelDisconnected_Rejected(t *testing.T) {
	prg := svc.NewProgram(svc.Config{})
	prg.SetCtlToken([]byte("token-32-bytes-secret-aaaaaaaaaa"))
	const zeroKey32 = "AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA="
	client, err := tunnel.NewClient("127.0.0.1:1", zeroKey32, tunnel.WithInsecure(true))
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}
	client.State().Set(tunnel.StateDisconnected)
	prg.ForTestSetTunnelClient(client)

	resp := Handle(prg, ipc.Request{
		Action: ipc.ActionTriggerRecovery,
		Auth:   "token-32-bytes-secret-aaaaaaaaaa",
	}, Options{})
	if resp.Error != "tunnel_not_connected" {
		t.Errorf("Error = %q; want tunnel_not_connected when state!=Connected", resp.Error)
	}
}

// TestHandle_TriggerRecovery_Success_FireAndForget validates the happy
// path: tunnel connected, token matches, handler replies ok
// immediately. RecoverFromAnomaly runs in a goroutine and we don't wait
// for it — the IPC reply must not block.
func TestHandle_TriggerRecovery_Success_FireAndForget(t *testing.T) {
	prg := svc.NewProgram(svc.Config{})
	prg.SetCtlToken([]byte("token-32-bytes-secret-aaaaaaaaaa"))
	const zeroKey32 = "AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA="
	client, err := tunnel.NewClient("127.0.0.1:1", zeroKey32, tunnel.WithInsecure(true))
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}
	client.State().Set(tunnel.StateConnected)
	prg.ForTestSetTunnelClient(client)

	// Bound the call so a deadlock in the handler is caught quickly.
	done := make(chan ipc.Response, 1)
	go func() {
		done <- Handle(prg, ipc.Request{
			Action: ipc.ActionTriggerRecovery,
			Auth:   "token-32-bytes-secret-aaaaaaaaaa",
		}, Options{})
	}()

	select {
	case resp := <-done:
		if resp.Status != ipc.StatusOK {
			t.Errorf("Status = %q; want ok. Error: %q", resp.Status, resp.Error)
		}
		if resp.AnomalyActive {
			t.Errorf("AnomalyActive should be false on fresh trigger, got true")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("handleTriggerRecovery blocked for 2s — should be fire-and-forget")
	}
}

// Review-fix M2 regression guard. When a recovery is already in
// progress, handleTriggerRecovery must not blindly return StatusOK as
// if it launched a fresh sequence. It MUST surface AnomalyActive=true
// so the operator sees the trigger was effectively a no-op.
func TestHandle_TriggerRecovery_AlreadyInProgress_ReflectsInResponse(t *testing.T) {
	prg := svc.NewProgram(svc.Config{})
	prg.SetCtlToken([]byte("token-32-bytes-secret-aaaaaaaaaa"))
	const zeroKey32 = "AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA="
	client, err := tunnel.NewClient("127.0.0.1:1", zeroKey32, tunnel.WithInsecure(true))
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}
	client.State().Set(tunnel.StateConnected)
	prg.ForTestSetTunnelClient(client)

	// Simulate an in-flight recovery by seeding AnomalyActive directly.
	prg.ForTestSetAnomaly(true, "leak_detected")
	defer prg.ForTestSetAnomaly(false, "")

	resp := Handle(prg, ipc.Request{
		Action: ipc.ActionTriggerRecovery,
		Auth:   "token-32-bytes-secret-aaaaaaaaaa",
	}, Options{})
	if resp.Status != ipc.StatusOK {
		t.Fatalf("Status = %q; want ok", resp.Status)
	}
	if !resp.AnomalyActive {
		t.Error("AnomalyActive = false; want true when a recovery is already in progress")
	}
	if resp.AnomalyReason != "leak_detected" {
		t.Errorf("AnomalyReason = %q; want leak_detected", resp.AnomalyReason)
	}
}
