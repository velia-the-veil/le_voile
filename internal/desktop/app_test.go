package desktop

import (
	"context"
	"fmt"
	"testing"

	"github.com/BurntSushi/toml"
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

	lastAction string
	lastValue  string
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

func (m *mockIPCClient) SendContext(_ context.Context, req ipc.Request) (ipc.Response, error) {
	m.lastAction = req.Action
	m.lastValue = req.Value
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
	app := NewApp(mock, "relay-iceland.levoile.dev", "", false)
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
	app := NewApp(mock, "relay-iceland.levoile.dev", "", false)
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
	app := NewApp(mock, "relay-iceland.levoile.dev", "", false)
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
	app := NewApp(mock, "relay-iceland.levoile.dev", "", false)
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
		domain     string
		wantName   string
		wantFlag   string
	}{
		{"relay-iceland.levoile.dev", "Islande", "🇮🇸"},
		{"relay-finland.levoile.dev", "Finlande", "🇫🇮"},
		{"relay-germany.example.com", "Allemagne", "🇩🇪"},
		{"relay-france.levoile.dev", "France", "🇫🇷"},
		{"relay-usa.levoile.dev", "États-Unis", "🇺🇸"},
		{"is.levoile.dev", "Islande", "🇮🇸"},
		{"de.levoile.dev", "Allemagne", "🇩🇪"},
		{"custom-relay.example.com", "custom-relay.example.com", ""},
		{"", "", ""},
	}

	for _, tt := range tests {
		t.Run(tt.domain, func(t *testing.T) {
			gotName, gotFlag := countryFromDomain(tt.domain)
			if gotName != tt.wantName {
				t.Errorf("countryFromDomain(%q) name = %q, want %q", tt.domain, gotName, tt.wantName)
			}
			if gotFlag != tt.wantFlag {
				t.Errorf("countryFromDomain(%q) flag = %q, want %q", tt.domain, gotFlag, tt.wantFlag)
			}
		})
	}
}

func TestStartupConnectsIPC(t *testing.T) {
	mock := &mockIPCClient{}
	app := NewApp(mock, "", "", false)

	app.Startup(context.Background())

	if !mock.connectCalled {
		t.Error("Startup did not call ipcClient.Connect()")
	}
}

func TestShutdownClosesIPC(t *testing.T) {
	mock := &mockIPCClient{}
	app := NewApp(mock, "", "", false)

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
	app := NewApp(mock, "", "", false)
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
	app := NewApp(mock, "relay-iceland.levoile.dev", "", false)
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
	app := NewApp(mock, "relay-iceland.levoile.dev", "", false)
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
	app := NewApp(mock, "relay-iceland.levoile.dev", "", false)
	app.Startup(context.Background())

	sr := app.GetStatus()

	if sr.Status != ipc.StatusError {
		t.Errorf("status = %q, want %q", sr.Status, ipc.StatusError)
	}
	if sr.Message != "Erreur — tunnel_timeout" {
		t.Errorf("message = %q, want %q", sr.Message, "Erreur — tunnel_timeout")
	}
}

// --- New tests for Story 10.2 ---

func TestGetRegistry_ReturnsCountries(t *testing.T) {
	mock := &mockIPCClient{
		resp: ipc.Response{
			Status: ipc.StatusOK,
			RegistryCountries: []ipc.RegistryCountry{
				{Code: "is", Name: "Islande", Flag: "🇮🇸", RelayCount: 2, Active: true},
				{Code: "de", Name: "Allemagne", Flag: "🇩🇪", RelayCount: 1, Active: false},
				{Code: "fi", Name: "Finlande", Flag: "🇫🇮", RelayCount: 1, Active: false},
				{Code: "us", Name: "États-Unis", Flag: "🇺🇸", RelayCount: 3, Active: false},
			},
		},
	}
	app := NewApp(mock, "is.levoile.dev", "", false)
	app.Startup(context.Background())

	reg := app.GetRegistry()

	if len(reg.Countries) != 4 {
		t.Fatalf("countries count = %d, want 4", len(reg.Countries))
	}
	if mock.lastAction != ipc.ActionGetRegistry {
		t.Errorf("last action = %q, want %q", mock.lastAction, ipc.ActionGetRegistry)
	}
	// Verify first country
	if reg.Countries[0].Code != "is" {
		t.Errorf("first country code = %q, want %q", reg.Countries[0].Code, "is")
	}
	if reg.Countries[0].Active != true {
		t.Error("first country should be active")
	}
}

func TestSelectCountry_Success(t *testing.T) {
	mock := &mockIPCClient{
		resp: ipc.Response{Status: ipc.StatusConnecting},
	}
	app := NewApp(mock, "is.levoile.dev", "", false)
	app.Startup(context.Background())

	sr := app.SelectCountry("de")

	if sr.Status != ipc.StatusConnecting {
		t.Errorf("status = %q, want %q", sr.Status, ipc.StatusConnecting)
	}
	if mock.lastAction != ipc.ActionSelectCountry {
		t.Errorf("last action = %q, want %q", mock.lastAction, ipc.ActionSelectCountry)
	}
	if mock.lastValue != "de" {
		t.Errorf("last value = %q, want %q", mock.lastValue, "de")
	}
}

func TestSelectCountry_InvalidCountry(t *testing.T) {
	mock := &mockIPCClient{
		resp: ipc.Response{Status: ipc.StatusError, Error: "no_relays_for_country"},
	}
	app := NewApp(mock, "is.levoile.dev", "", false)
	app.Startup(context.Background())

	sr := app.SelectCountry("xx")

	if sr.Status != ipc.StatusError {
		t.Errorf("status = %q, want %q", sr.Status, ipc.StatusError)
	}
}

func TestGetStatus_ReturnsRealIP(t *testing.T) {
	mock := &mockIPCClient{
		resp: ipc.Response{
			Status: ipc.StatusConnected,
			IP:     "93.184.216.34",
			Uptime: "0m15s",
		},
	}
	app := NewApp(mock, "is.levoile.dev", "", false)
	app.Startup(context.Background())

	sr := app.GetStatus()

	if sr.IP != "93.184.216.34" {
		t.Errorf("ip = %q, want %q", sr.IP, "93.184.216.34")
	}
	if sr.Country != "Islande" {
		t.Errorf("country = %q, want %q", sr.Country, "Islande")
	}
	if sr.Flag != "🇮🇸" {
		t.Errorf("flag = %q, want %q", sr.Flag, "🇮🇸")
	}
}

func TestGetRegistry_IPCError(t *testing.T) {
	mock := &mockIPCClient{
		err: fmt.Errorf("ipc: client: not connected"),
	}
	app := NewApp(mock, "is.levoile.dev", "", false)
	app.Startup(context.Background())

	reg := app.GetRegistry()

	if len(reg.Countries) != 0 {
		t.Errorf("countries count = %d, want 0 on error", len(reg.Countries))
	}
}

// --- New tests for Story 10.3 ---

func TestConnect_Success(t *testing.T) {
	mock := &mockIPCClient{
		resp: ipc.Response{Status: ipc.StatusConnecting},
	}
	app := NewApp(mock, "is.levoile.dev", "", false)
	app.runtimeQuit = func(ctx context.Context) {}
	app.Startup(context.Background())

	sr := app.Connect()

	if sr.Status != ipc.StatusConnecting {
		t.Errorf("status = %q, want %q", sr.Status, ipc.StatusConnecting)
	}
	if sr.Message != "Reconnexion en cours..." {
		t.Errorf("message = %q, want %q", sr.Message, "Reconnexion en cours...")
	}
	if mock.lastAction != ipc.ActionConnect {
		t.Errorf("last action = %q, want %q", mock.lastAction, ipc.ActionConnect)
	}
}

func TestConnect_IPCError(t *testing.T) {
	mock := &mockIPCClient{
		err: fmt.Errorf("ipc: client: not connected"),
	}
	app := NewApp(mock, "is.levoile.dev", "", false)
	app.runtimeQuit = func(ctx context.Context) {}
	app.Startup(context.Background())
	initialConnects := mock.connectCount

	sr := app.Connect()

	if sr.Status != ipc.StatusDisconnected {
		t.Errorf("status = %q, want %q", sr.Status, ipc.StatusDisconnected)
	}
	if sr.Message != "Déconnecté" {
		t.Errorf("message = %q, want %q", sr.Message, "Déconnecté")
	}
	if mock.connectCount <= initialConnects {
		t.Error("expected reconnectIPC after IPC error")
	}
}

func TestDisconnect_Success(t *testing.T) {
	mock := &mockIPCClient{
		resp: ipc.Response{Status: ipc.StatusDisconnected},
	}
	app := NewApp(mock, "is.levoile.dev", "", false)
	app.runtimeQuit = func(ctx context.Context) {}
	app.Startup(context.Background())

	sr := app.Disconnect()

	if sr.Status != ipc.StatusDisconnected {
		t.Errorf("status = %q, want %q", sr.Status, ipc.StatusDisconnected)
	}
	if mock.lastAction != ipc.ActionDisconnect {
		t.Errorf("last action = %q, want %q", mock.lastAction, ipc.ActionDisconnect)
	}
}

func TestDisconnect_IPCError(t *testing.T) {
	mock := &mockIPCClient{
		err: fmt.Errorf("ipc: client: not connected"),
	}
	app := NewApp(mock, "is.levoile.dev", "", false)
	app.runtimeQuit = func(ctx context.Context) {}
	app.Startup(context.Background())

	sr := app.Disconnect()

	if sr.Status != ipc.StatusDisconnected {
		t.Errorf("status = %q, want %q", sr.Status, ipc.StatusDisconnected)
	}
	if sr.Message != "Déconnecté" {
		t.Errorf("message = %q, want %q", sr.Message, "Déconnecté")
	}
}

func TestQuit_SendsActionQuit(t *testing.T) {
	mock := &mockIPCClient{
		resp: ipc.Response{Status: ipc.StatusOK},
	}
	quitCalled := false
	app := NewApp(mock, "is.levoile.dev", "", false)
	app.runtimeQuit = func(ctx context.Context) { quitCalled = true }
	app.Startup(context.Background())

	app.Quit()

	if mock.lastAction != ipc.ActionQuit {
		t.Errorf("last action = %q, want %q", mock.lastAction, ipc.ActionQuit)
	}
	if !quitCalled {
		t.Error("expected runtimeQuit to be called")
	}
}

func TestGetSkipQuitModal(t *testing.T) {
	app := NewApp(&mockIPCClient{}, "", "", false)
	if app.GetSkipQuitModal() {
		t.Error("expected GetSkipQuitModal() = false by default")
	}

	app2 := NewApp(&mockIPCClient{}, "", "", true)
	if !app2.GetSkipQuitModal() {
		t.Error("expected GetSkipQuitModal() = true when initialized with true")
	}
}

func TestSetSkipQuitModal(t *testing.T) {
	app := NewApp(&mockIPCClient{}, "", "", false)
	app.SetSkipQuitModal(true)
	if !app.GetSkipQuitModal() {
		t.Error("expected GetSkipQuitModal() = true after SetSkipQuitModal(true)")
	}
	app.SetSkipQuitModal(false)
	if app.GetSkipQuitModal() {
		t.Error("expected GetSkipQuitModal() = false after SetSkipQuitModal(false)")
	}
}

func TestSetSkipQuitModal_PersistsToConfig(t *testing.T) {
	path := t.TempDir() + "/config.toml"
	app := NewApp(&mockIPCClient{}, "", path, false)
	app.runtimeQuit = func(ctx context.Context) {}
	app.Startup(context.Background())

	app.SetSkipQuitModal(true)

	// Verify it was persisted by loading the config file
	cfg, err := loadTestConfig(path)
	if err != nil {
		t.Fatalf("failed to load config: %v", err)
	}
	if !cfg.Client.SkipQuitModal {
		t.Error("expected SkipQuitModal = true in persisted config")
	}
}

// loadTestConfig is a local helper to avoid importing config in test signatures.
func loadTestConfig(path string) (*struct {
	Client struct {
		SkipQuitModal bool `toml:"skip_quit_modal"`
	} `toml:"client"`
}, error) {
	// Re-use the config package via the app's own import.
	// Since we need to read the toml file, use config.Load directly.
	cfg := &struct {
		Client struct {
			SkipQuitModal bool `toml:"skip_quit_modal"`
		} `toml:"client"`
	}{}
	_, err := toml.DecodeFile(path, cfg)
	return cfg, err
}
