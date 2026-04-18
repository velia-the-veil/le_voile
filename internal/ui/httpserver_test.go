package ui

import (
	"context"
	"encoding/json"
	"fmt"
	"io/fs"
	"net/http"
	"net/http/httptest"
	"runtime"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/velia-the-veil/le_voile/internal/ipc"
)

// mockIPCClient implements IPCClient for testing. It records the last Request
// sent so tests can assert that handlers dispatch the intended IPC action —
// this guards against contract drift between the HTTP layer and ipchandler
// (e.g. reading a field the service doesn't populate for the chosen action).
type mockIPCClient struct {
	resp    ipc.Response
	err     error
	lastReq ipc.Request
}

func (m *mockIPCClient) Connect() error { return nil }
func (m *mockIPCClient) Close() error   { return nil }
func (m *mockIPCClient) SendContext(_ context.Context, req ipc.Request) (ipc.Response, error) {
	m.lastReq = req
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
			Status:       ipc.StatusConnected,
			IP:           "185.220.1.1",
			Country:      "Islande",
			CountryFlag:  "🇮🇸",
			RelayID:      "is-01",
			RelayLatency: "42ms",
			Uptime:       "1h30m",
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

// TestGetStatus_ServiceUnreachable_EmitsHint locks the Story 5.6 AC1/AC2
// contract: when the IPC transport fails, /api/status MUST set
// service_reachable=false and include a non-empty service_start_hint so the
// frontend can render the "Service Le Voile non démarré" fallback screen with
// the OS-specific shell command. Regression guard against the Story 5.4 review
// fix that silently absorbed IPC errors into a disconnected status.
func TestGetStatus_ServiceUnreachable_EmitsHint(t *testing.T) {
	mock := &mockIPCClient{err: fmt.Errorf("pipe broken")}
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
	if resp.ServiceReachable {
		t.Error("service_reachable = true, want false on IPC transport failure")
	}
	if resp.ServiceStartHint == nil {
		t.Fatal("service_start_hint = nil, want populated hint on unreachable service")
	}
	if resp.ServiceStartHint.HumanMessage == "" {
		t.Error("service_start_hint.human_message empty — frontend would render blank fallback")
	}
	// OS-specific sanity: the hint must match the OS this test process runs
	// on (CurrentServiceStartHint uses runtime.GOOS indirectly).
	if runtime.GOOS == "windows" && resp.ServiceStartHint.Command != "sc start levoile-service" {
		t.Errorf("windows command = %q, want 'sc start levoile-service'", resp.ServiceStartHint.Command)
	}
	if runtime.GOOS == "linux" && resp.ServiceStartHint.Command != "sudo systemctl start levoile.service" {
		t.Errorf("linux command = %q, want 'sudo systemctl start levoile.service'", resp.ServiceStartHint.Command)
	}
}

// TestGetStatus_ServiceReachable_NoHint guards the symmetric case: when the
// IPC call succeeds (tunnel connected OR disconnected, service alive), the
// hint MUST be omitted so the frontend does NOT render the fallback screen.
// Prevents a class of bugs where "disconnected tunnel" and "service down"
// would be conflated in the UI.
func TestGetStatus_ServiceReachable_NoHint(t *testing.T) {
	mock := &mockIPCClient{
		resp: ipc.Response{Status: ipc.StatusDisconnected},
	}
	srv := NewHTTPServer(NewSafeIPCClient(mock), testFS())

	req := httptest.NewRequest(http.MethodGet, "/api/status", nil)
	w := httptest.NewRecorder()
	srv.mux.ServeHTTP(w, req)

	body := w.Body.String()
	var resp APIStatusResponse
	json.NewDecoder(strings.NewReader(body)).Decode(&resp)

	if !resp.ServiceReachable {
		t.Error("service_reachable = false, want true when IPC succeeded (tunnel merely disconnected)")
	}
	if resp.ServiceStartHint != nil {
		t.Errorf("service_start_hint should be omitted when service is reachable, got %+v", resp.ServiceStartHint)
	}
	// Wire-level guard: omitempty means the key must not appear at all.
	if strings.Contains(body, `"service_start_hint"`) {
		t.Errorf("encoded JSON must omit service_start_hint when nil, got: %s", body)
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
	// Story 5.4 review M5 fix — IPC transport failure surfaces as a
	// machine-readable "error" field so the frontend can differentiate a true
	// disconnect from "service unreachable".
	if resp["error"] != "service_unreachable" {
		t.Errorf("error = %q, want service_unreachable", resp["error"])
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
	if resp["error"] != "service_unreachable" {
		t.Errorf("error = %q, want service_unreachable", resp["error"])
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

// TestDisconnect_ErrorFieldIncluded locks the disconnect error-propagation
// contract: when the IPC handler returns StatusError with an Error message,
// the frontend must receive that message in the JSON "error" field so
// toggleConnect() can surface it to the user (Story 5.4 AC2, learning 10-3 #M4).
func TestDisconnect_ErrorFieldIncluded(t *testing.T) {
	mock := &mockIPCClient{
		resp: ipc.Response{Status: ipc.StatusError, Error: "tunnel teardown failed"},
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
	if resp["error"] != "tunnel teardown failed" {
		t.Errorf("error = %q, want 'tunnel teardown failed'", resp["error"])
	}
}

// TestDisconnect_DispatchesActionDisconnect locks the IPC action contract:
// POST /api/disconnect MUST dispatch ipc.ActionDisconnect, not ActionConnect
// or any other action. Guards against copy-paste regressions in the handler.
func TestDisconnect_DispatchesActionDisconnect(t *testing.T) {
	mock := &mockIPCClient{
		resp: ipc.Response{Status: ipc.StatusDisconnected},
	}
	srv := NewHTTPServer(NewSafeIPCClient(mock), testFS())

	req := httptest.NewRequest(http.MethodPost, "/api/disconnect", nil)
	w := httptest.NewRecorder()
	srv.mux.ServeHTTP(w, req)

	if mock.lastReq.Action != ipc.ActionDisconnect {
		t.Errorf("dispatched action %q, want %q", mock.lastReq.Action, ipc.ActionDisconnect)
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
		{http.MethodPost, "/api/leak-status"},
		{http.MethodPost, "/api/update-status"},
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

func TestQuitEndpointRemoved(t *testing.T) {
	// /api/quit was intentionally removed (security: unauthenticated loopback
	// kill-switch of the service). Confirm the route does not resolve to any
	// registered handler — Go's ServeMux falls back to serving from the root
	// FileServer, which returns 404 for /api/quit (no such asset).
	mock := &mockIPCClient{}
	srv := NewHTTPServer(NewSafeIPCClient(mock), testFS())

	req := httptest.NewRequest(http.MethodPost, "/api/quit", nil)
	w := httptest.NewRecorder()
	srv.mux.ServeHTTP(w, req)

	if w.Code == http.StatusOK {
		t.Errorf("expected /api/quit to be removed (non-200), got 200")
	}
}

func TestLeakStatus_OK(t *testing.T) {
	mock := &mockIPCClient{
		resp: ipc.Response{
			LeakStatus:     ipc.StatusLeakOK,
			LeakLastCheck:  "2026-04-17T13:00:00Z",
			LeakExpectedIP: "198.51.100.7",
		},
	}
	srv := NewHTTPServer(NewSafeIPCClient(mock), testFS())

	req := httptest.NewRequest(http.MethodGet, "/api/leak-status", nil)
	w := httptest.NewRecorder()
	srv.mux.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	// Regression guard against H1/H2: the endpoint MUST read cached state via
	// ActionGetStatus (filled from the scheduler), not trigger a live 20 s
	// ActionLeakCheck on every poll.
	if mock.lastReq.Action != ipc.ActionGetStatus {
		t.Errorf("leak-status dispatched action %q, want %q (cached read)", mock.lastReq.Action, ipc.ActionGetStatus)
	}
	var resp APILeakStatusResponse
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp.Status != ipc.StatusLeakOK {
		t.Errorf("status = %q, want %q", resp.Status, ipc.StatusLeakOK)
	}
	if resp.LastCheck != "2026-04-17T13:00:00Z" {
		t.Errorf("last_check = %q, want timestamp", resp.LastCheck)
	}
	if resp.ExpectedIP != "198.51.100.7" {
		t.Errorf("expected_ip = %q, want %q", resp.ExpectedIP, "198.51.100.7")
	}
	if resp.Reason != "" {
		t.Errorf("reason = %q, want empty on ok", resp.Reason)
	}
}

// TestLeakStatus_LeakDetected (Story 6.2 AC6) — the handler forwards the
// new fields (expected_ip, reason) from the IPC Response to the JSON
// payload unchanged so the frontend can render a precise alert.
func TestLeakStatus_LeakDetected(t *testing.T) {
	mock := &mockIPCClient{
		resp: ipc.Response{
			LeakStatus:     ipc.StatusLeakDetected,
			LeakExpectedIP: "198.51.100.7",
			LeakReason:     "stun_ip_differs_from_relay",
		},
	}
	srv := NewHTTPServer(NewSafeIPCClient(mock), testFS())

	req := httptest.NewRequest(http.MethodGet, "/api/leak-status", nil)
	w := httptest.NewRecorder()
	srv.mux.ServeHTTP(w, req)

	var resp APILeakStatusResponse
	json.NewDecoder(w.Body).Decode(&resp)
	if resp.Status != ipc.StatusLeakDetected {
		t.Errorf("status = %q, want %q", resp.Status, ipc.StatusLeakDetected)
	}
	if resp.ExpectedIP != "198.51.100.7" {
		t.Errorf("expected_ip = %q, want %q", resp.ExpectedIP, "198.51.100.7")
	}
	if resp.Reason != "stun_ip_differs_from_relay" {
		t.Errorf("reason = %q, want %q", resp.Reason, "stun_ip_differs_from_relay")
	}
}

func TestLeakStatus_IPCError(t *testing.T) {
	mock := &mockIPCClient{err: fmt.Errorf("pipe broken")}
	srv := NewHTTPServer(NewSafeIPCClient(mock), testFS())

	req := httptest.NewRequest(http.MethodGet, "/api/leak-status", nil)
	w := httptest.NewRecorder()
	srv.mux.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200 even on IPC error, got %d", w.Code)
	}
	// Empty status is acceptable fallback — frontend treats it as pending/unknown.
}

func TestUpdateStatus_Ready(t *testing.T) {
	mock := &mockIPCClient{
		resp: ipc.Response{
			UpdateStatus:     ipc.StatusUpdateReady,
			UpdateVersion:    "1.2.0",
			InstalledVersion: "1.1.0",
		},
	}
	srv := NewHTTPServer(NewSafeIPCClient(mock), testFS())

	req := httptest.NewRequest(http.MethodGet, "/api/update-status", nil)
	w := httptest.NewRecorder()
	srv.mux.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	var resp APIUpdateStatusResponse
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp.Status != "update_ready" {
		t.Errorf("status = %q, want update_ready", resp.Status)
	}
	if resp.Version != "1.2.0" {
		t.Errorf("version = %q, want 1.2.0", resp.Version)
	}
	if resp.InstalledVersion != "1.1.0" {
		t.Errorf("installed_version = %q, want 1.1.0", resp.InstalledVersion)
	}
}

func TestUpdateStatus_UpToDate(t *testing.T) {
	mock := &mockIPCClient{
		resp: ipc.Response{UpdateStatus: ipc.StatusUpToDate, InstalledVersion: "1.2.0"},
	}
	srv := NewHTTPServer(NewSafeIPCClient(mock), testFS())

	req := httptest.NewRequest(http.MethodGet, "/api/update-status", nil)
	w := httptest.NewRecorder()
	srv.mux.ServeHTTP(w, req)

	var resp APIUpdateStatusResponse
	json.NewDecoder(w.Body).Decode(&resp)
	if resp.Status != "up_to_date" {
		t.Errorf("status = %q, want up_to_date", resp.Status)
	}
}

func TestUpdateStatus_Rollback(t *testing.T) {
	mock := &mockIPCClient{
		resp: ipc.Response{
			UpdateStatus:    ipc.StatusRollback,
			RollbackVersion: "1.1.0",
			RollbackReason:  "integrity check failed",
		},
	}
	srv := NewHTTPServer(NewSafeIPCClient(mock), testFS())

	req := httptest.NewRequest(http.MethodGet, "/api/update-status", nil)
	w := httptest.NewRecorder()
	srv.mux.ServeHTTP(w, req)

	var resp APIUpdateStatusResponse
	json.NewDecoder(w.Body).Decode(&resp)
	if resp.Status != "rollback" {
		t.Errorf("status = %q, want rollback", resp.Status)
	}
	if resp.RollbackVersion != "1.1.0" {
		t.Errorf("rollback_version = %q, want 1.1.0", resp.RollbackVersion)
	}
	if resp.RollbackReason != "integrity check failed" {
		t.Errorf("rollback_reason = %q, want 'integrity check failed'", resp.RollbackReason)
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

// TestRegistryEndpoint_JSONContract locks the /api/registry JSON field names
// consumed by the frontend sidebar (Story 5.3). The frontend reads c.flag and
// c.relay_count directly; renaming these tags breaks the sidebar silently.
// Decoding into a generic map asserts on the wire-level key names, so the
// test also catches Go tag renames that would otherwise slip past a typed
// decode.
func TestRegistryEndpoint_JSONContract(t *testing.T) {
	mock := &mockIPCClient{
		resp: ipc.Response{
			RegistryCountries: []ipc.RegistryCountry{
				{Code: "de", Name: "Allemagne", Flag: "🇩🇪", RelayCount: 2, Active: true},
				{Code: "gb", Name: "Royaume-Uni", Flag: "🇬🇧", RelayCount: 1, Active: false},
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

	var raw struct {
		Countries []map[string]interface{} `json:"countries"`
	}
	if err := json.NewDecoder(w.Body).Decode(&raw); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(raw.Countries) != 2 {
		t.Fatalf("countries: got %d, want 2", len(raw.Countries))
	}

	// Lock every frontend-visible key on the first entry.
	first := raw.Countries[0]
	for _, key := range []string{"code", "name", "flag", "relay_count", "active"} {
		if _, ok := first[key]; !ok {
			t.Errorf("missing JSON key %q in first entry; got keys: %v", key, keysOf(first))
		}
	}
	if first["code"] != "de" || first["name"] != "Allemagne" || first["flag"] != "🇩🇪" {
		t.Errorf("first entry: got %+v, want de/Allemagne/🇩🇪", first)
	}
	// JSON numbers decode to float64 by default.
	if rc, ok := first["relay_count"].(float64); !ok || int(rc) != 2 {
		t.Errorf("first.relay_count: got %v (%T), want 2", first["relay_count"], first["relay_count"])
	}
	if active, ok := first["active"].(bool); !ok || !active {
		t.Errorf("first.active: got %v, want true", first["active"])
	}
}

func keysOf(m map[string]interface{}) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
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

// TestStatusCountryFlagAndVisibleIP locks the /api/status contract required by
// Story 5.2: the JSON response must carry country_flag, country, relay_id (as
// produced by the discoverer, e.g. "relay-de-001") and ip (visible exit IP) so
// the frontend panneau Statut can render "🇩🇪 ALLEMAGNE / de-001 · 85ms /
// IP dévoilée : 203.0.113.7".
func TestStatusCountryFlagAndVisibleIP(t *testing.T) {
	mock := &mockIPCClient{
		resp: ipc.Response{
			Status:       ipc.StatusConnected,
			IP:           "203.0.113.7",
			RealIP:       "82.64.10.1",
			Country:      "Allemagne",
			CountryFlag:  "🇩🇪",
			RelayID:      "relay-de-001",
			RelayLatency: "85ms",
		},
	}
	srv := NewHTTPServer(NewSafeIPCClient(mock), testFS())

	req := httptest.NewRequest(http.MethodGet, "/api/status", nil)
	w := httptest.NewRecorder()
	srv.mux.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}

	// Capture the body ONCE — decoding consumes it, so keep the string for the
	// snake_case key regression check below.
	body := w.Body.String()
	var resp APIStatusResponse
	if err := json.NewDecoder(strings.NewReader(body)).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}

	// AC1 — country_flag + country both populated.
	if resp.Country != "Allemagne" {
		t.Errorf("country = %q, want Allemagne", resp.Country)
	}
	if resp.CountryFlag != "🇩🇪" {
		t.Errorf("country_flag = %q, want 🇩🇪", resp.CountryFlag)
	}

	// AC2 — relay_id carried through intact (frontend shortens "relay-" prefix).
	if resp.RelayID != "relay-de-001" {
		t.Errorf("relay_id = %q, want relay-de-001 (frontend strips prefix)", resp.RelayID)
	}
	if resp.RelayLatency != "85ms" {
		t.Errorf("relay_latency = %q, want 85ms", resp.RelayLatency)
	}

	// AC3 — visible IP exposed as `ip`; real IP exposed as `real_ip`.
	if resp.IP != "203.0.113.7" {
		t.Errorf("ip = %q, want 203.0.113.7", resp.IP)
	}
	if resp.RealIP != "82.64.10.1" {
		t.Errorf("real_ip = %q, want 82.64.10.1", resp.RealIP)
	}

	// Regression: encoded JSON must contain the snake_case keys the frontend
	// reads via fetch('/api/status').
	for _, key := range []string{`"country"`, `"country_flag"`, `"relay_id"`, `"relay_latency"`, `"ip"`, `"real_ip"`} {
		if !strings.Contains(body, key) {
			t.Errorf("response JSON missing %s key: %s", key, body)
		}
	}
}

// TestStatus_Connected_UnknownCountry locks the contract for the M4 edge case:
// when the registry yields a relay whose ID does not match any entry in
// CountryMetaMap, ipchandler leaves Country + CountryFlag empty. /api/status
// must pass the empty strings through faithfully so the frontend can fall back
// on the short relay ID. Regression guard against any future "helpful" filler
// applied in the HTTP layer.
func TestStatus_Connected_UnknownCountry(t *testing.T) {
	mock := &mockIPCClient{
		resp: ipc.Response{
			Status:      ipc.StatusConnected,
			IP:          "203.0.113.7",
			Country:     "",
			CountryFlag: "",
			RelayID:     "relay-xx-001",
		},
	}
	srv := NewHTTPServer(NewSafeIPCClient(mock), testFS())

	req := httptest.NewRequest(http.MethodGet, "/api/status", nil)
	w := httptest.NewRecorder()
	srv.mux.ServeHTTP(w, req)

	var resp APIStatusResponse
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp.Country != "" {
		t.Errorf("country = %q, want empty (HTTP layer must not invent a country)", resp.Country)
	}
	if resp.CountryFlag != "" {
		t.Errorf("country_flag = %q, want empty", resp.CountryFlag)
	}
	if resp.RelayID != "relay-xx-001" {
		t.Errorf("relay_id = %q, want relay-xx-001 (frontend fallback needs it)", resp.RelayID)
	}
	if resp.Status != "connected" {
		t.Errorf("status = %q, want connected", resp.Status)
	}
}

// TestStatus_Connected_NoVisibleIP locks the contract for the M5 race window:
// right after Connect, DetectVisibleIP runs in a goroutine and VisibleIP() may
// still be "" for 1-2 polling cycles. The HTTP layer must pass the empty IP
// through so the frontend can display its "détection en cours…" placeholder
// instead of a blank row.
func TestStatus_Connected_NoVisibleIP(t *testing.T) {
	mock := &mockIPCClient{
		resp: ipc.Response{
			Status:      ipc.StatusConnected,
			IP:          "",
			RealIP:      "82.64.10.1",
			Country:     "Allemagne",
			CountryFlag: "🇩🇪",
			RelayID:     "relay-de-001",
		},
	}
	srv := NewHTTPServer(NewSafeIPCClient(mock), testFS())

	req := httptest.NewRequest(http.MethodGet, "/api/status", nil)
	w := httptest.NewRecorder()
	srv.mux.ServeHTTP(w, req)

	var resp APIStatusResponse
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp.Status != "connected" {
		t.Errorf("status = %q, want connected", resp.Status)
	}
	if resp.IP != "" {
		t.Errorf("ip = %q, want empty (frontend triggers placeholder on empty)", resp.IP)
	}
	// Country must still flow through — the visible IP race does not affect
	// country metadata, which is computed synchronously from the registry.
	if resp.Country != "Allemagne" {
		t.Errorf("country = %q, want Allemagne", resp.Country)
	}
}

// TestStatus_ProductionRelayShape_E2E runs the real HTTPServer over net.Listen
// against a mock IPC populated with the actual shape produced by the
// registry + ipchandler for de-001.levoile.dev (3-digit suffix, as observed in
// https://relay.levoile.dev/.well-known/relay-registry.json on 2026-04-17).
// This is the full-stack smoke for Story 5.2 that the agent can run without
// a live tunnel — it validates the HTTP→JSON contract end-to-end.
func TestStatus_ProductionRelayShape_E2E(t *testing.T) {
	mock := &mockIPCClient{
		resp: ipc.Response{
			Status:       ipc.StatusConnected,
			IP:           "217.160.59.54", // de-001.levoile.dev resolved address
			RealIP:       "90.66.218.27",
			Country:      "Allemagne",
			CountryFlag:  "🇩🇪",
			RelayID:      "relay-de-001", // production 3-digit suffix
			RelayLatency: "42ms",
		},
	}
	srv := NewHTTPServer(NewSafeIPCClient(mock), testFS())

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	errCh := make(chan error, 1)
	go func() { errCh <- srv.Start(ctx) }()
	<-srv.Ready()

	resp, err := http.Get("http://" + srv.Addr() + "/api/status")
	if err != nil {
		t.Fatalf("GET /api/status: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}

	var api APIStatusResponse
	if err := json.NewDecoder(resp.Body).Decode(&api); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if api.Status != "connected" {
		t.Errorf("status = %q, want connected", api.Status)
	}
	if api.Country != "Allemagne" || api.CountryFlag != "🇩🇪" {
		t.Errorf("country/flag = %q/%q, want Allemagne/🇩🇪", api.Country, api.CountryFlag)
	}
	if api.RelayID != "relay-de-001" {
		t.Errorf("relay_id = %q, want relay-de-001 (frontend strips 'relay-' → 'de-001')", api.RelayID)
	}
	if api.IP != "217.160.59.54" {
		t.Errorf("ip = %q, want 217.160.59.54 (de-001 public IP)", api.IP)
	}
	if api.Message != "Connecté — Allemagne" {
		t.Errorf("message = %q, want 'Connecté — Allemagne'", api.Message)
	}

	cancel()
	if err := <-errCh; err != nil {
		t.Errorf("Start returned error: %v", err)
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

// --- /api/ui-prefs (Story 5.5 review H3 fix) -------------------------------

// newTestHTTPServerWithPrefs builds a handler whose prefs store is redirected
// to a temp file so tests never touch the real user config dir.
func newTestHTTPServerWithPrefs(t *testing.T) *HTTPServer {
	t.Helper()
	s := NewHTTPServer(NewSafeIPCClient(&mockIPCClient{}), testFS())
	s.prefs.path = t.TempDir() + "/ui-prefs.json"
	return s
}

func TestUIPrefs_GetReturnsDefaultsOnFirstRun(t *testing.T) {
	s := newTestHTTPServerWithPrefs(t)
	req := httptest.NewRequest(http.MethodGet, "/api/ui-prefs", nil)
	rec := httptest.NewRecorder()

	s.handleUIPrefs(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	var got UIPrefs
	if err := json.NewDecoder(rec.Body).Decode(&got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if !got.QuitPromptEnabled {
		t.Error("default QuitPromptEnabled should be true")
	}
}

func TestUIPrefs_PostPersistsAndGetRetrieves(t *testing.T) {
	s := newTestHTTPServerWithPrefs(t)

	// POST disables the quit prompt.
	post := httptest.NewRequest(http.MethodPost, "/api/ui-prefs",
		strings.NewReader(`{"quit_prompt_enabled":false}`))
	post.Header.Set("Content-Type", "application/json")
	recPost := httptest.NewRecorder()
	s.handleUIPrefs(recPost, post)
	if recPost.Code != http.StatusOK {
		t.Fatalf("POST status = %d, want 200", recPost.Code)
	}

	// GET must now return the persisted value.
	get := httptest.NewRequest(http.MethodGet, "/api/ui-prefs", nil)
	recGet := httptest.NewRecorder()
	s.handleUIPrefs(recGet, get)
	var got UIPrefs
	if err := json.NewDecoder(recGet.Body).Decode(&got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if got.QuitPromptEnabled {
		t.Error("QuitPromptEnabled should be false after POST")
	}
}

func TestUIPrefs_PostBadJSON(t *testing.T) {
	s := newTestHTTPServerWithPrefs(t)
	req := httptest.NewRequest(http.MethodPost, "/api/ui-prefs",
		strings.NewReader("{not json"))
	rec := httptest.NewRecorder()
	s.handleUIPrefs(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400", rec.Code)
	}
}

func TestUIPrefs_MethodNotAllowed(t *testing.T) {
	s := newTestHTTPServerWithPrefs(t)
	req := httptest.NewRequest(http.MethodDelete, "/api/ui-prefs", nil)
	rec := httptest.NewRecorder()
	s.handleUIPrefs(rec, req)
	if rec.Code != http.StatusMethodNotAllowed {
		t.Errorf("status = %d, want 405", rec.Code)
	}
}

// --- Story 5.9: Mode dégradé endpoints ---

// AC3 — /api/status surfaces killswitch_mode every time (default normal when
// the IPC layer left it empty due to service unreachable).
func TestGetStatus_KillSwitchMode_DefaultsToNormalWhenEmpty(t *testing.T) {
	mock := &mockIPCClient{resp: ipc.Response{Status: ipc.StatusDisconnected}}
	srv := NewHTTPServer(NewSafeIPCClient(mock), testFS())

	req := httptest.NewRequest(http.MethodGet, "/api/status", nil)
	w := httptest.NewRecorder()
	srv.mux.ServeHTTP(w, req)

	var resp APIStatusResponse
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp.KillSwitchMode != ipc.KillSwitchModeNormal {
		t.Errorf("killswitch_mode = %q, want normal (default safe rendering)", resp.KillSwitchMode)
	}
}

func TestGetStatus_KillSwitchMode_SurfacesDegraded(t *testing.T) {
	mock := &mockIPCClient{resp: ipc.Response{
		Status:         ipc.StatusConnected,
		KillSwitchMode: ipc.KillSwitchModeDegraded,
	}}
	srv := NewHTTPServer(NewSafeIPCClient(mock), testFS())

	req := httptest.NewRequest(http.MethodGet, "/api/status", nil)
	w := httptest.NewRecorder()
	srv.mux.ServeHTTP(w, req)

	var resp APIStatusResponse
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp.KillSwitchMode != ipc.KillSwitchModeDegraded {
		t.Errorf("killswitch_mode = %q, want degraded", resp.KillSwitchMode)
	}
}

// AC2 — POST /api/settings/killswitch dispatches the IPC action with the
// mode value and returns the killswitch_mode echoed by the service.
func TestSetKillSwitch_PostDegraded(t *testing.T) {
	mock := &mockIPCClient{resp: ipc.Response{
		Status:         ipc.StatusOK,
		KillSwitchMode: ipc.KillSwitchModeDegraded,
	}}
	srv := NewHTTPServer(NewSafeIPCClient(mock), testFS())

	body := strings.NewReader(`{"mode":"degraded"}`)
	req := httptest.NewRequest(http.MethodPost, "/api/settings/killswitch", body)
	req.Header.Set("X-CSRF-Token", srv.csrfToken)
	w := httptest.NewRecorder()
	srv.mux.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", w.Code)
	}
	if mock.lastReq.Action != ipc.ActionSetKillSwitchMode {
		t.Errorf("ipc action = %q, want %q", mock.lastReq.Action, ipc.ActionSetKillSwitchMode)
	}
	if mock.lastReq.Value != ipc.KillSwitchModeDegraded {
		t.Errorf("ipc value = %q, want degraded", mock.lastReq.Value)
	}
	if mock.lastReq.Auth != "" {
		t.Errorf("UI request must leave Auth empty, got %q", mock.lastReq.Auth)
	}

	var resp map[string]string
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp["killswitch_mode"] != ipc.KillSwitchModeDegraded {
		t.Errorf("response killswitch_mode = %q, want degraded", resp["killswitch_mode"])
	}
}

// AC2 — invalid mode value returns 400 without dispatching IPC.
func TestSetKillSwitch_BadMode(t *testing.T) {
	mock := &mockIPCClient{}
	srv := NewHTTPServer(NewSafeIPCClient(mock), testFS())

	req := httptest.NewRequest(http.MethodPost, "/api/settings/killswitch",
		strings.NewReader(`{"mode":"bogus"}`))
	req.Header.Set("X-CSRF-Token", srv.csrfToken)
	w := httptest.NewRecorder()
	srv.mux.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400", w.Code)
	}
	if mock.lastReq.Action != "" {
		t.Errorf("IPC must not be dispatched on invalid mode (got %q)", mock.lastReq.Action)
	}
}

// Story 5.9 M2 fix — POST /api/settings/killswitch without the CSRF token
// returns 403 and never reaches the IPC layer.
func TestSetKillSwitch_RejectsMissingCSRF(t *testing.T) {
	mock := &mockIPCClient{}
	srv := NewHTTPServer(NewSafeIPCClient(mock), testFS())

	req := httptest.NewRequest(http.MethodPost, "/api/settings/killswitch",
		strings.NewReader(`{"mode":"degraded"}`))
	w := httptest.NewRecorder()
	srv.mux.ServeHTTP(w, req)

	if w.Code != http.StatusForbidden {
		t.Errorf("status = %d, want 403 (missing CSRF)", w.Code)
	}
	if mock.lastReq.Action != "" {
		t.Errorf("IPC must not be dispatched without CSRF, got %q", mock.lastReq.Action)
	}
}

// Story 5.9 M2 fix — wrong CSRF token returns 403.
func TestSetKillSwitch_RejectsWrongCSRF(t *testing.T) {
	mock := &mockIPCClient{}
	srv := NewHTTPServer(NewSafeIPCClient(mock), testFS())

	req := httptest.NewRequest(http.MethodPost, "/api/settings/killswitch",
		strings.NewReader(`{"mode":"degraded"}`))
	req.Header.Set("X-CSRF-Token", "definitely-not-the-real-token")
	w := httptest.NewRecorder()
	srv.mux.ServeHTTP(w, req)

	if w.Code != http.StatusForbidden {
		t.Errorf("status = %d, want 403", w.Code)
	}
}

// Story 5.9 M2 fix — /api/csrf-token returns the per-process token.
func TestCSRFToken_Endpoint(t *testing.T) {
	mock := &mockIPCClient{}
	srv := NewHTTPServer(NewSafeIPCClient(mock), testFS())

	req := httptest.NewRequest(http.MethodGet, "/api/csrf-token", nil)
	w := httptest.NewRecorder()
	srv.mux.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", w.Code)
	}
	var resp map[string]string
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp["token"] != srv.csrfToken {
		t.Errorf("returned token = %q, want server token", resp["token"])
	}
	if len(resp["token"]) < 32 {
		t.Errorf("token too short: %d chars", len(resp["token"]))
	}
}

// /api/settings includes killswitch_mode for the Settings panel.
func TestGetSettings_IncludesKillSwitchMode(t *testing.T) {
	mock := &mockIPCClient{resp: ipc.Response{
		Status:         ipc.StatusConnected,
		KillSwitchMode: ipc.KillSwitchModeDegraded,
	}}
	srv := NewHTTPServer(NewSafeIPCClient(mock), testFS())

	req := httptest.NewRequest(http.MethodGet, "/api/settings", nil)
	w := httptest.NewRecorder()
	srv.mux.ServeHTTP(w, req)

	var resp map[string]any
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp["killswitch_mode"] != ipc.KillSwitchModeDegraded {
		t.Errorf("settings killswitch_mode = %v, want degraded", resp["killswitch_mode"])
	}
}
