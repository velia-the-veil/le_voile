//go:build linux

package ui

import (
	"bytes"
	"testing"

	"github.com/velia-the-veil/le_voile/linux/internal/ipc"
)

// Story 6.3 — Task 5 AC4. The anomaly branch must win over every other
// state so the user sees the orange glyph the instant the recovery
// starts. The test seeds StatusConnected (the "everything is fine"
// default) and flips on AnomalyActive; the tray must render IconAlert
// with the anomaly tooltip.
func TestUpdateTrayState_AnomalyOverridesConnected(t *testing.T) {
	api := &mockSystrayAPI{}
	u := &UI{
		api:    api,
		client: NewSafeIPCClient(&mockIPCClient{}),
	}

	resp := ipc.Response{
		Status:        ipc.StatusConnected,
		IP:            "192.0.2.1",
		Country:       "France",
		AnomalyActive: true,
		AnomalyReason: "leak_detected",
	}
	u.updateTrayState(resp)

	if tooltip := api.getTooltip(); tooltip != "Anomalie détectée — reconnexion en cours" {
		t.Errorf("tooltip = %q; want 'Anomalie détectée — reconnexion en cours'", tooltip)
	}
	if api.icon == nil {
		t.Fatal("icon not set")
	}
	if !bytes.Equal(api.icon, IconAlert) {
		t.Errorf("icon != IconAlert — anomaly branch did not fire")
	}

	// Connected flag must be cleared so other UI code keeps the "not
	// protected" semantics consistent with the orange glyph.
	u.mu.Lock()
	defer u.mu.Unlock()
	if u.connected {
		t.Error("u.connected=true after anomaly; expected false")
	}
}

// TestUpdateTrayState_AnomalyOverridesDegraded is the strongest version
// of the override: degraded mode is itself already an override over
// connected (Story 5.9). Anomaly recovery must still win, because the
// user needs to know the service is actively trying to recover rather
// than being in a stable degraded mode.
func TestUpdateTrayState_AnomalyOverridesDegraded(t *testing.T) {
	api := &mockSystrayAPI{}
	u := &UI{
		api:    api,
		client: NewSafeIPCClient(&mockIPCClient{}),
	}

	resp := ipc.Response{
		Status:         ipc.StatusConnected,
		KillSwitchMode: ipc.KillSwitchModeDegraded,
		AnomalyActive:  true,
	}
	u.updateTrayState(resp)

	if !bytes.Equal(api.icon, IconAlert) {
		t.Errorf("icon != IconAlert; anomaly should beat degraded-mode override")
	}
	if tooltip := api.getTooltip(); tooltip != "Anomalie détectée — reconnexion en cours" {
		t.Errorf("tooltip = %q; expected anomaly tooltip", tooltip)
	}
}

// TestUpdateTrayState_AnomalyClearsBackToConnected guarantees the
// transition path — the stateKey includes AnomalyActive so a
// false→true→false cycle re-renders the original connected icon even
// when no other field changed.
func TestUpdateTrayState_AnomalyClearsBackToConnected(t *testing.T) {
	api := &mockSystrayAPI{}
	u := &UI{
		api:    api,
		client: NewSafeIPCClient(&mockIPCClient{}),
	}

	base := ipc.Response{
		Status:  ipc.StatusConnected,
		IP:      "198.51.100.10",
		Country: "Allemagne",
		RelayID: "relay-de-01",
	}

	// Initial connected state.
	u.updateTrayState(base)
	if !bytes.Equal(api.icon, IconConnected) {
		t.Fatal("initial state did not render IconConnected")
	}

	// Anomaly fires.
	r2 := base
	r2.AnomalyActive = true
	u.updateTrayState(r2)
	if !bytes.Equal(api.icon, IconAlert) {
		t.Fatal("anomaly state did not render IconAlert")
	}

	// Anomaly clears — same fields as initial.
	u.updateTrayState(base)
	if !bytes.Equal(api.icon, IconConnected) {
		t.Errorf("after recovery, icon != IconConnected; the stateKey missed the AnomalyActive flip back")
	}
}

// TestUpdateTrayState_AnomalyWithDisconnectedBackend covers the edge
// case where the tunnel is already down while the watchdog triggers a
// recovery attempt. The orange glyph must still win.
func TestUpdateTrayState_AnomalyWithDisconnectedBackend(t *testing.T) {
	api := &mockSystrayAPI{}
	u := &UI{
		api:    api,
		client: NewSafeIPCClient(&mockIPCClient{}),
	}

	resp := ipc.Response{
		Status:        ipc.StatusDisconnected,
		AnomalyActive: true,
		AnomalyReason: "tun_altered",
	}
	u.updateTrayState(resp)

	if !bytes.Equal(api.icon, IconAlert) {
		t.Errorf("icon != IconAlert for disconnected+anomaly")
	}
}
