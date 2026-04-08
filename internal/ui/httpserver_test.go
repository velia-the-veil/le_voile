package ui

import (
	"context"
	"encoding/json"
	"fmt"
	"io/fs"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/velia-the-veil/le_voile/internal/ipc"
)

// mockIPCClient implements IPCClient for testing.
type mockIPCClient struct {
	resp ipc.Response
	err  error
}

func (m *mockIPCClient) Connect() error { return nil }
func (m *mockIPCClient) Close() error   { return nil }
func (m *mockIPCClient) SendContext(_ context.Context, _ ipc.Request) (ipc.Response, error) {
	return m.resp, m.err
}

func testFS() fs.FS {
	return fstest.MapFS{
		"index.html": &fstest.MapFile{Data: []byte("<html>test</html>")},
	}
}

func TestGetStatus_Connected(t *testing.T) {
	mock := &mockIPCClient{
		resp: ipc.Response{
			Status:      ipc.StatusConnected,
			IP:          "185.220.1.1",
			Country:     "Islande",
			CountryFlag: "🇮🇸",
			RelayID:     "is-01",
			RelayLatency: "42ms",
			Uptime:      "1h30m",
		},
	}
	srv := NewHTTPServer(NewSafeIPCClient(mock), testFS())

	req := httptest.NewRequest(http.MethodGet, "/api/status", nil)
	w := httptest.NewRecorder()
	srv.mux.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}

	var resp APIStatusResponse
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}

	if resp.Status != "connected" {
		t.Errorf("status = %q, want connected", resp.Status)
	}
	if resp.IP != "185.220.1.1" {
		t.Errorf("ip = %q, want 185.220.1.1", resp.IP)
	}
	if resp.Message != "Connecté — Islande" {
		t.Errorf("message = %q, want 'Connecté — Islande'", resp.Message)
	}
	if resp.Country != "Islande" {
		t.Errorf("country = %q, want Islande", resp.Country)
	}
	if resp.CountryFlag != "🇮🇸" {
		t.Errorf("country_flag = %q, want 🇮🇸", resp.CountryFlag)
	}
	if resp.RelayID != "is-01" {
		t.Errorf("relay_id = %q, want is-01", resp.RelayID)
	}
}

func TestGetStatus_Disconnected(t *testing.T) {
	mock := &mockIPCClient{
		resp: ipc.Response{Status: ipc.StatusDisconnected},
	}
	srv := NewHTTPServer(NewSafeIPCClient(mock), testFS())

	req := httptest.NewRequest(http.MethodGet, "/api/status", nil)
	w := httptest.NewRecorder()
	srv.mux.ServeHTTP(w, req)

	var resp APIStatusResponse
	json.NewDecoder(w.Body).Decode(&resp)

	if resp.Status != "disconnected" {
		t.Errorf("status = %q, want disconnected", resp.Status)
	}
	if resp.Message != "Déconnecté" {
		t.Errorf("message = %q, want 'Déconnecté'", resp.Message)
	}
}

func TestGetStatus_IPCError(t *testing.T) {
	mock := &mockIPCClient{
		err: fmt.Errorf("pipe broken"),
	}
	srv := NewHTTPServer(NewSafeIPCClient(mock), testFS())

	req := httptest.NewRequest(http.MethodGet, "/api/status", nil)
	w := httptest.NewRecorder()
	srv.mux.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200 even on IPC error, got %d", w.Code)
	}

	var resp APIStatusResponse
	json.NewDecoder(w.Body).Decode(&resp)

	if resp.Status != "disconnected" {
		t.Errorf("status = %q, want disconnected on IPC error", resp.Status)
	}
	if resp.Message != "Déconnecté" {
		t.Errorf("message = %q, want 'Déconnecté'", resp.Message)
	}
}

func TestGetStatus_Connecting(t *testing.T) {
	mock := &mockIPCClient{
		resp: ipc.Response{Status: ipc.StatusConnecting},
	}
	srv := NewHTTPServer(NewSafeIPCClient(mock), testFS())

	req := httptest.NewRequest(http.MethodGet, "/api/status", nil)
	w := httptest.NewRecorder()
	srv.mux.ServeHTTP(w, req)

	var resp APIStatusResponse
	json.NewDecoder(w.Body).Decode(&resp)

	if resp.Status != "connecting" {
		t.Errorf("status = %q, want connecting", resp.Status)
	}
	if resp.Message != "Reconnexion en cours..." {
		t.Errorf("message = %q, want 'Reconnexion en cours...'", resp.Message)
	}
}

func TestServeAssets(t *testing.T) {
	mock := &mockIPCClient{}
	srv := NewHTTPServer(NewSafeIPCClient(mock), testFS())

	// Request "/" which serves index.html via FileServer.
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	w := httptest.NewRecorder()
	srv.mux.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	if !strings.Contains(w.Body.String(), "<html>test</html>") {
		t.Error("expected index.html content")
	}
}

func TestConnect(t *testing.T) {
	mock := &mockIPCClient{
		resp: ipc.Response{Status: ipc.StatusConnected},
	}
	srv := NewHTTPServer(NewSafeIPCClient(mock), testFS())

	req := httptest.NewRequest(http.MethodPost, "/api/connect", nil)
	w := httptest.NewRecorder()
	srv.mux.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}

	var resp map[string]string
	json.NewDecoder(w.Body).Decode(&resp)
	if resp["status"] != "connected" {
		t.Errorf("status = %q, want connected", resp["status"])
	}
}

func TestDisconnect(t *testing.T) {
	mock := &mockIPCClient{
		resp: ipc.Response{Status: ipc.StatusDisconnected},
	}
	srv := NewHTTPServer(NewSafeIPCClient(mock), testFS())

	req := httptest.NewRequest(http.MethodPost, "/api/disconnect", nil)
	w := httptest.NewRecorder()
	srv.mux.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}

	var resp map[string]string
	json.NewDecoder(w.Body).Decode(&resp)
	if resp["status"] != "disconnected" {
		t.Errorf("status = %q, want disconnected", resp["status"])
	}
}

func TestConnect_IPCError_ReturnsDisconnected(t *testing.T) {
	mock := &mockIPCClient{
		err: fmt.Errorf("pipe broken"),
	}
	srv := NewHTTPServer(NewSafeIPCClient(mock), testFS())

	req := httptest.NewRequest(http.MethodPost, "/api/connect", nil)
	w := httptest.NewRecorder()
	srv.mux.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}

	var resp map[string]string
	json.NewDecoder(w.Body).Decode(&resp)
	if resp["status"] != "disconnected" {
		t.Errorf("status = %q, want disconnected", resp["status"])
	}
}

func TestDisconnect_IPCError_ReturnsDisconnected(t *testing.T) {
	mock := &mockIPCClient{
		err: fmt.Errorf("pipe broken"),
	}
	srv := NewHTTPServer(NewSafeIPCClient(mock), testFS())

	req := httptest.NewRequest(http.MethodPost, "/api/disconnect", nil)
	w := httptest.NewRecorder()
	srv.mux.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}

	var resp map[string]string
	json.NewDecoder(w.Body).Decode(&resp)
	if resp["status"] != "disconnected" {
		t.Errorf("status = %q, want disconnected", resp["status"])
	}
}

func TestConnect_ErrorFieldIncluded(t *testing.T) {
	mock := &mockIPCClient{
		resp: ipc.Response{Status: ipc.StatusError, Error: "no relay available"},
	}
	srv := NewHTTPServer(NewSafeIPCClient(mock), testFS())

	req := httptest.NewRequest(http.MethodPost, "/api/connect", nil)
	w := httptest.NewRecorder()
	srv.mux.ServeHTTP(w, req)

	var resp map[string]string
	json.NewDecoder(w.Body).Decode(&resp)
	if resp["error"] != "no relay available" {
		t.Errorf("error = %q, want 'no relay available'", resp["error"])
	}
}

func TestMethodNotAllowed(t *testing.T) {
	mock := &mockIPCClient{}
	srv := NewHTTPServer(NewSafeIPCClient(mock), testFS())

	tests := []struct {
		method, path string
	}{
		{http.MethodPost, "/api/status"},
		{http.MethodGet, "/api/connect"},
		{http.MethodGet, "/api/disconnect"},
		{http.MethodPost, "/api/registry"},
		{http.MethodGet, "/api/country"},
	}

	for _, tt := range tests {
		req := httptest.NewRequest(tt.method, tt.path, nil)
		w := httptest.NewRecorder()
		srv.mux.ServeHTTP(w, req)
		if w.Code != http.StatusMethodNotAllowed {
			t.Errorf("%s %s: expected 405, got %d", tt.method, tt.path, w.Code)
		}
	}
}

func TestHTTPServer_StartAndAddr(t *testing.T) {
	mock := &mockIPCClient{
		resp: ipc.Response{Status: ipc.StatusDisconnected},
	}
	safe := NewSafeIPCClient(mock)
	srv := NewHTTPServer(safe, testFS())

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	errCh := make(chan error, 1)
	go func() {
		errCh <- srv.Start(ctx)
	}()

	// Wait for server to be ready.
	<-srv.Ready()
	addr := srv.Addr()
	if addr == "" {
		t.Fatal("Addr() returned empty string")
	}

	// Hit the status endpoint via real HTTP.
	resp, err := http.Get("http://" + addr + "/api/status")
	if err != nil {
		t.Fatalf("GET /api/status: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("expected 200, got %d", resp.StatusCode)
	}

	// Shutdown.
	cancel()
	if err := <-errCh; err != nil {
		t.Errorf("Start returned error: %v", err)
	}
}

func TestRegistryEndpoint(t *testing.T) {
	mock := &mockIPCClient{
		resp: ipc.Response{
			RegistryCountries: []ipc.RegistryCountry{
				{Code: "is", Name: "Islande", Flag: "🇮🇸", RelayCount: 2, Active: true},
				{Code: "de", Name: "Allemagne", Flag: "🇩🇪", RelayCount: 3, Active: false},
				{Code: "fi", Name: "Finlande", Flag: "🇫🇮", RelayCount: 1, Active: false},
				{Code: "us", Name: "États-Unis", Flag: "🇺🇸", RelayCount: 2, Active: false},
			},
		},
	}
	srv := NewHTTPServer(NewSafeIPCClient(mock), testFS())

	req := httptest.NewRequest(http.MethodGet, "/api/registry", nil)
	w := httptest.NewRecorder()
	srv.mux.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}

	var resp struct {
		Countries []ipc.RegistryCountry `json:"countries"`
	}
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(resp.Countries) != 4 {
		t.Fatalf("expected 4 countries, got %d", len(resp.Countries))
	}
	if resp.Countries[0].Code != "is" || !resp.Countries[0].Active {
		t.Errorf("first country: got %+v, want is/active", resp.Countries[0])
	}
	if resp.Countries[1].Name != "Allemagne" {
		t.Errorf("second country name = %q, want Allemagne", resp.Countries[1].Name)
	}
}

func TestRegistryEndpoint_IPCError_ReturnsEmptyArray(t *testing.T) {
	mock := &mockIPCClient{
		err: fmt.Errorf("pipe broken"),
	}
	srv := NewHTTPServer(NewSafeIPCClient(mock), testFS())

	req := httptest.NewRequest(http.MethodGet, "/api/registry", nil)
	w := httptest.NewRecorder()
	srv.mux.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}

	// Must return {"countries":[]} not {"countries":null}
	body := w.Body.String()
	if !strings.Contains(body, `"countries":[]`) {
		t.Errorf("expected empty array, got: %s", body)
	}
}

func TestCountryEndpoint_ValidCode(t *testing.T) {
	mock := &mockIPCClient{
		resp: ipc.Response{Status: ipc.StatusOK},
	}
	srv := NewHTTPServer(NewSafeIPCClient(mock), testFS())

	body := strings.NewReader(`{"code":"de"}`)
	req := httptest.NewRequest(http.MethodPost, "/api/country", body)
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	srv.mux.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}

	var resp map[string]string
	json.NewDecoder(w.Body).Decode(&resp)
	if resp["status"] != "ok" {
		t.Errorf("status = %q, want ok", resp["status"])
	}
}

func TestCountryEndpoint_InvalidMethod(t *testing.T) {
	mock := &mockIPCClient{}
	srv := NewHTTPServer(NewSafeIPCClient(mock), testFS())

	req := httptest.NewRequest(http.MethodGet, "/api/country", nil)
	w := httptest.NewRecorder()
	srv.mux.ServeHTTP(w, req)

	if w.Code != http.StatusMethodNotAllowed {
		t.Errorf("expected 405, got %d", w.Code)
	}
}

func TestCountryEndpoint_EmptyCode(t *testing.T) {
	mock := &mockIPCClient{}
	srv := NewHTTPServer(NewSafeIPCClient(mock), testFS())

	body := strings.NewReader(`{"code":""}`)
	req := httptest.NewRequest(http.MethodPost, "/api/country", body)
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	srv.mux.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d", w.Code)
	}
}

func TestCountryEndpoint_InvalidJSON(t *testing.T) {
	mock := &mockIPCClient{}
	srv := NewHTTPServer(NewSafeIPCClient(mock), testFS())

	body := strings.NewReader(`not json`)
	req := httptest.NewRequest(http.MethodPost, "/api/country", body)
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	srv.mux.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d", w.Code)
	}
}

func TestStatusMessage(t *testing.T) {
	tests := []struct {
		status, country, want string
	}{
		{"connected", "Islande", "Connecté — Islande"},
		{"connected", "", "Connecté"},
		{"connecting", "", "Reconnexion en cours..."},
		{"disconnected", "", "Déconnecté"},
		{"error", "", "Déconnecté"},
	}
	for _, tt := range tests {
		got := statusMessage(tt.status, tt.country)
		if got != tt.want {
			t.Errorf("statusMessage(%q, %q) = %q, want %q", tt.status, tt.country, got, tt.want)
		}
	}
}
