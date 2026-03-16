package desktop

import (
	"context"
	"fmt"
	"testing"

	"github.com/velia-the-veil/le_voile/internal/ipc"
)

// mockIPCClient is a test double for IPCClient.
type mockIPCClient struct {
	resp ipc.Response
	err  error

	connectCalled bool
	connectCount  int
	closeCalled   bool
	closeCount    int
}

func (m *mockIPCClient) Connect() error {
	m.connectCalled = true
	m.connectCount++
	return nil
}

func (m *mockIPCClient) Close() error {
	m.closeCalled = true
	m.closeCount++
	return nil
}

func (m *mockIPCClient) SendContext(_ context.Context, _ ipc.Request) (ipc.Response, error) {
	return m.resp, m.err
}

func TestGetStatus_Connected(t *testing.T) {
	mock := &mockIPCClient{
		resp: ipc.Response{
			Status: ipc.StatusConnected,
			IP:     "185.220.101.42",
			Uptime: "1h23m",
		},
	}
	app := NewApp(mock, "relay-iceland.levoile.dev")
	app.Startup(context.Background())

	sr := app.GetStatus()

	if sr.Status != ipc.StatusConnected {
		t.Errorf("status = %q, want %q", sr.Status, ipc.StatusConnected)
	}
	if sr.IP != "185.220.101.42" {
		t.Errorf("ip = %q, want %q", sr.IP, "185.220.101.42")
	}
	if sr.Message != "Connecté — Islande" {
		t.Errorf("message = %q, want %q", sr.Message, "Connecté — Islande")
	}
	if sr.Country != "Islande" {
		t.Errorf("country = %q, want %q", sr.Country, "Islande")
	}
	if sr.Uptime != "1h23m" {
		t.Errorf("uptime = %q, want %q", sr.Uptime, "1h23m")
	}
}

func TestGetStatus_Connecting(t *testing.T) {
	mock := &mockIPCClient{
		resp: ipc.Response{Status: ipc.StatusConnecting},
	}
	app := NewApp(mock, "relay-iceland.levoile.dev")
	app.Startup(context.Background())

	sr := app.GetStatus()

	if sr.Status != ipc.StatusConnecting {
		t.Errorf("status = %q, want %q", sr.Status, ipc.StatusConnecting)
	}
	if sr.Message != "Reconnexion en cours..." {
		t.Errorf("message = %q, want %q", sr.Message, "Reconnexion en cours...")
	}
}

func TestGetStatus_Disconnected(t *testing.T) {
	mock := &mockIPCClient{
		resp: ipc.Response{Status: ipc.StatusDisconnected},
	}
	app := NewApp(mock, "relay-iceland.levoile.dev")
	app.Startup(context.Background())

	sr := app.GetStatus()

	if sr.Status != ipc.StatusDisconnected {
		t.Errorf("status = %q, want %q", sr.Status, ipc.StatusDisconnected)
	}
	if sr.Message != "Déconnecté" {
		t.Errorf("message = %q, want %q", sr.Message, "Déconnecté")
	}
}

func TestGetStatus_IPCError(t *testing.T) {
	mock := &mockIPCClient{
		err: fmt.Errorf("ipc: client: not connected"),
	}
	app := NewApp(mock, "relay-iceland.levoile.dev")
	app.Startup(context.Background())
	initialConnects := mock.connectCount

	sr := app.GetStatus()

	if sr.Status != ipc.StatusDisconnected {
		t.Errorf("status = %q, want %q", sr.Status, ipc.StatusDisconnected)
	}
	if sr.Message != "Déconnecté" {
		t.Errorf("message = %q, want %q", sr.Message, "Déconnecté")
	}
	// Verify reconnect was attempted (Close + Connect after error)
	if mock.closeCount < 1 {
		t.Error("expected Close() call for reconnect after IPC error")
	}
	if mock.connectCount <= initialConnects {
		t.Error("expected Connect() call for reconnect after IPC error")
	}
}

func TestRelayCountryMapping(t *testing.T) {
	tests := []struct {
		domain  string
		want    string
	}{
		{"relay-iceland.levoile.dev", "Islande"},
		{"relay-finland.levoile.dev", "Finlande"},
		{"relay-germany.example.com", "Allemagne"},
		{"relay-france.levoile.dev", "France"},
		{"relay-usa.levoile.dev", "États-Unis"},
		{"custom-relay.example.com", "custom-relay.example.com"},
		{"", ""},
	}

	for _, tt := range tests {
		t.Run(tt.domain, func(t *testing.T) {
			got := countryFromDomain(tt.domain)
			if got != tt.want {
				t.Errorf("countryFromDomain(%q) = %q, want %q", tt.domain, got, tt.want)
			}
		})
	}
}

func TestStartupConnectsIPC(t *testing.T) {
	mock := &mockIPCClient{}
	app := NewApp(mock, "")

	app.Startup(context.Background())

	if !mock.connectCalled {
		t.Error("Startup did not call ipcClient.Connect()")
	}
}

func TestShutdownClosesIPC(t *testing.T) {
	mock := &mockIPCClient{}
	app := NewApp(mock, "")

	app.Shutdown(context.Background())

	if !mock.closeCalled {
		t.Error("Shutdown did not call ipcClient.Close()")
	}
}

func TestGetStatus_ConnectedNoCountry(t *testing.T) {
	mock := &mockIPCClient{
		resp: ipc.Response{
			Status: ipc.StatusConnected,
			IP:     "1.2.3.4",
		},
	}
	app := NewApp(mock, "")
	app.Startup(context.Background())

	sr := app.GetStatus()

	if sr.Message != "Connecté" {
		t.Errorf("message = %q, want %q", sr.Message, "Connecté")
	}
}

func TestGetStatus_DisconnectedNoRelayID(t *testing.T) {
	mock := &mockIPCClient{
		resp: ipc.Response{Status: ipc.StatusDisconnected},
	}
	app := NewApp(mock, "relay-iceland.levoile.dev")
	app.Startup(context.Background())

	sr := app.GetStatus()

	if sr.RelayID != "" {
		t.Errorf("RelayID = %q, want empty when disconnected", sr.RelayID)
	}
}

func TestGetStatus_ConnectedHasRelayID(t *testing.T) {
	mock := &mockIPCClient{
		resp: ipc.Response{Status: ipc.StatusConnected, IP: "1.2.3.4"},
	}
	app := NewApp(mock, "relay-iceland.levoile.dev")
	app.Startup(context.Background())

	sr := app.GetStatus()

	if sr.RelayID != "relay-iceland.levoile.dev" {
		t.Errorf("RelayID = %q, want %q when connected", sr.RelayID, "relay-iceland.levoile.dev")
	}
}

func TestGetStatus_Error(t *testing.T) {
	mock := &mockIPCClient{
		resp: ipc.Response{
			Status: ipc.StatusError,
			Error:  "tunnel_timeout",
		},
	}
	app := NewApp(mock, "relay-iceland.levoile.dev")
	app.Startup(context.Background())

	sr := app.GetStatus()

	if sr.Status != ipc.StatusError {
		t.Errorf("status = %q, want %q", sr.Status, ipc.StatusError)
	}
	if sr.Message != "Erreur — tunnel_timeout" {
		t.Errorf("message = %q, want %q", sr.Message, "Erreur — tunnel_timeout")
	}
}
