package ui

import (
	"context"
	"sync"
	"testing"

	"github.com/velia-the-veil/le_voile/internal/ipc"
)

// mockSystrayAPI records systray calls for testing.
type mockSystrayAPI struct {
	mu      sync.Mutex
	icon    []byte
	tooltip string
	title   string
}

func (m *mockSystrayAPI) SetIcon(iconBytes []byte) {
	m.mu.Lock()
	m.icon = iconBytes
	m.mu.Unlock()
}
func (m *mockSystrayAPI) SetTooltip(tooltip string) {
	m.mu.Lock()
	m.tooltip = tooltip
	m.mu.Unlock()
}
func (m *mockSystrayAPI) SetTitle(title string) {
	m.mu.Lock()
	m.title = title
	m.mu.Unlock()
}

func (m *mockSystrayAPI) getTooltip() string {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.tooltip
}

func TestNewUI(t *testing.T) {
	client := &mockIPCClient{}
	u := New(client, Config{RelayDomain: "levoile.dev"})

	if u == nil {
		t.Fatal("New returned nil")
	}
	if u.client == nil {
		t.Error("client not set")
	}
	if u.config.RelayDomain != "levoile.dev" {
		t.Errorf("relay_domain = %q, want levoile.dev", u.config.RelayDomain)
	}
}

func TestUpdateTrayState_Connected(t *testing.T) {
	api := &mockSystrayAPI{}
	u := &UI{
		api:    api,
		client: NewSafeIPCClient(&mockIPCClient{}),
	}

	resp := ipc.Response{
		Status:  ipc.StatusConnected,
		IP:      "1.2.3.4",
		Country: "Islande",
	}
	u.updateTrayState(resp)

	tooltip := api.getTooltip()
	if tooltip != "Protégé — Islande — IP : 1.2.3.4" {
		t.Errorf("tooltip = %q", tooltip)
	}

	u.mu.Lock()
	connected := u.connected
	u.mu.Unlock()
	if !connected {
		t.Error("expected connected=true")
	}
}

func TestUpdateTrayState_Disconnected(t *testing.T) {
	api := &mockSystrayAPI{}
	u := &UI{
		api:    api,
		client: NewSafeIPCClient(&mockIPCClient{}),
	}

	resp := ipc.Response{Status: ipc.StatusDisconnected}
	u.updateTrayState(resp)

	tooltip := api.getTooltip()
	if tooltip != "Non protégé" {
		t.Errorf("tooltip = %q, want 'Non protégé'", tooltip)
	}

	u.mu.Lock()
	connected := u.connected
	u.mu.Unlock()
	if connected {
		t.Error("expected connected=false")
	}
}

func TestUpdateTrayState_NoDuplicateUpdates(t *testing.T) {
	api := &mockSystrayAPI{}
	u := &UI{
		api:    api,
		client: NewSafeIPCClient(&mockIPCClient{}),
	}

	resp := ipc.Response{Status: ipc.StatusDisconnected}
	u.updateTrayState(resp)
	tooltip1 := api.getTooltip()

	// Set tooltip to something different to detect if update runs again.
	api.SetTooltip("changed")
	u.updateTrayState(resp) // same state — should skip

	tooltip2 := api.getTooltip()
	if tooltip2 != "changed" {
		t.Errorf("expected no update on same state, but tooltip changed to %q", tooltip2)
	}
	_ = tooltip1
}

func TestHandleIPCError_SetsDisconnected(t *testing.T) {
	api := &mockSystrayAPI{}
	// Client that fails Connect() to avoid blocking in reconnectIPC.
	client := &mockIPCClientReconnect{connectErr: nil}
	u := &UI{
		api:    api,
		client: NewSafeIPCClient(client),
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // cancel immediately to prevent blocking in reconnectIPC

	u.handleIPCError(ctx)

	tooltip := api.getTooltip()
	if tooltip != "Service indisponible" {
		t.Errorf("tooltip = %q, want 'Service indisponible'", tooltip)
	}
}

func TestHandleToggle_ConnectWhenDisconnected(t *testing.T) {
	api := &mockSystrayAPI{}
	mock := &mockIPCClient{
		resp: ipc.Response{Status: ipc.StatusConnected, IP: "1.2.3.4"},
	}
	u := &UI{
		api:       api,
		client:    NewSafeIPCClient(mock),
		connected: false,
	}

	ctx := context.Background()
	u.handleToggle(ctx)

	tooltip := api.getTooltip()
	if tooltip != "Protégé — IP visible : 1.2.3.4" {
		t.Errorf("tooltip = %q, want 'Protégé — IP visible : 1.2.3.4'", tooltip)
	}
}

func TestHandleToggle_DisconnectWhenConnected(t *testing.T) {
	api := &mockSystrayAPI{}
	mock := &mockIPCClient{
		resp: ipc.Response{Status: ipc.StatusDisconnected},
	}
	u := &UI{
		api:       api,
		client:    NewSafeIPCClient(mock),
		connected: true,
	}

	ctx := context.Background()
	u.handleToggle(ctx)

	tooltip := api.getTooltip()
	if tooltip != "Non protégé" {
		t.Errorf("tooltip = %q, want 'Non protégé'", tooltip)
	}
}

func TestHandleToggle_IPCError(t *testing.T) {
	api := &mockSystrayAPI{}
	mock := &mockIPCClient{
		err: context.DeadlineExceeded,
	}
	u := &UI{
		api:    api,
		client: NewSafeIPCClient(mock),
	}

	ctx := context.Background()
	u.handleToggle(ctx)

	tooltip := api.getTooltip()
	if tooltip == "" {
		t.Error("expected error tooltip, got empty")
	}
}

// TestGetStatus_MissingIP verifies that get_status returns "connected"
// even when IP is empty (async detection not yet complete).
func TestGetStatus_MissingIP(t *testing.T) {
	api := &mockSystrayAPI{}
	u := &UI{
		api:    api,
		client: NewSafeIPCClient(&mockIPCClient{}),
	}

	resp := ipc.Response{
		Status:  ipc.StatusConnected,
		Country: "Islande",
		IP:      "", // empty — detection in progress
	}
	u.updateTrayState(resp)

	tooltip := api.getTooltip()
	if tooltip != "Protégé — Islande — IP en détection..." {
		t.Errorf("tooltip = %q, want 'Protégé — Islande — IP en détection...'", tooltip)
	}

	u.mu.Lock()
	connected := u.connected
	u.mu.Unlock()
	if !connected {
		t.Error("expected connected=true even with empty IP")
	}
}

// mockIPCClientReconnect implements IPCClient with configurable Connect behavior.
type mockIPCClientReconnect struct {
	mockIPCClient
	connectErr error
}

func (m *mockIPCClientReconnect) Connect() error { return m.connectErr }
