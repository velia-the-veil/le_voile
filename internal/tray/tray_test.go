package tray

import (
	"bytes"
	"context"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"fyne.io/systray"
	"github.com/velia-the-veil/le_voile/internal/ipc"
)

const testPollInterval = 50 * time.Millisecond

// mockSystrayAPI records SetIcon, SetTooltip, and SetTitle calls.
type mockSystrayAPI struct {
	mu      sync.Mutex
	icon    []byte
	tooltip string
	title   string
	calls   int
}

func (m *mockSystrayAPI) SetIcon(iconBytes []byte) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.icon = iconBytes
	m.calls++
}

func (m *mockSystrayAPI) SetTooltip(tooltip string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.tooltip = tooltip
	m.calls++
}

func (m *mockSystrayAPI) SetTitle(title string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.title = title
	m.calls++
}

func (m *mockSystrayAPI) getTooltip() string {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.tooltip
}

func (m *mockSystrayAPI) getIcon() []byte {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.icon
}

func (m *mockSystrayAPI) getCalls() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.calls
}

// mockSystrayMenuAPI records menu operations.
type mockSystrayMenuAPI struct {
	mu       sync.Mutex
	quitCh   chan struct{}
	quitCall bool
}

func newMockMenuAPI() *mockSystrayMenuAPI {
	return &mockSystrayMenuAPI{quitCh: make(chan struct{}, 1)}
}

func (m *mockSystrayMenuAPI) AddMenuItem(title, tooltip string) *systray.MenuItem {
	return nil // menu items are not used in unit tests
}

func (m *mockSystrayMenuAPI) AddMenuItemCheckbox(title, tooltip string, checked bool) *systray.MenuItem {
	return nil
}

func (m *mockSystrayMenuAPI) AddSeparator() {}

func (m *mockSystrayMenuAPI) Quit() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.quitCall = true
	select {
	case m.quitCh <- struct{}{}:
	default:
	}
}

func (m *mockSystrayMenuAPI) wasQuitCalled() bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.quitCall
}

// mockIPCClient simulates the IPC client.
type mockIPCClient struct {
	mu           sync.Mutex
	connected    bool
	connectErr   error
	response     ipc.Response
	sendErr      error
	connectCalls int
	lastRequest  ipc.Request
	requests     []ipc.Request
}

func (m *mockIPCClient) Connect() error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.connectCalls++
	if m.connectErr != nil {
		return m.connectErr
	}
	m.connected = true
	return nil
}

func (m *mockIPCClient) Close() error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.connected = false
	return nil
}

func (m *mockIPCClient) SendContext(_ context.Context, req ipc.Request) (ipc.Response, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.lastRequest = req
	m.requests = append(m.requests, req)
	if m.sendErr != nil {
		return ipc.Response{}, m.sendErr
	}
	return m.response, nil
}

func (m *mockIPCClient) setResponse(resp ipc.Response) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.response = resp
}

func (m *mockIPCClient) setSendErr(err error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.sendErr = err
}

func (m *mockIPCClient) getLastRequest() ipc.Request {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.lastRequest
}

func (m *mockIPCClient) getRequests() []ipc.Request {
	m.mu.Lock()
	defer m.mu.Unlock()
	cp := make([]ipc.Request, len(m.requests))
	copy(cp, m.requests)
	return cp
}

// --- updateTrayState tests ---

func TestTray_UpdateState_Connected(t *testing.T) {
	api := &mockSystrayAPI{}
	client := &mockIPCClient{}
	tr := newWithDeps(api, newMockMenuAPI(), client, testPollInterval, true, false, false, nil)

	tr.updateTrayState(ipc.Response{Status: ipc.StatusConnected, IP: "82.1.2.3"})

	if got := api.getIcon(); !bytes.Equal(got, IconConnected) {
		t.Error("expected connected icon")
	}
	if got := api.getTooltip(); got != "Protégé — IP visible : 82.1.2.3" {
		t.Errorf("unexpected tooltip: %q", got)
	}
}

func TestTray_UpdateState_Connected_UnknownIP(t *testing.T) {
	api := &mockSystrayAPI{}
	client := &mockIPCClient{}
	tr := newWithDeps(api, newMockMenuAPI(), client, testPollInterval, true, false, false, nil)

	tr.updateTrayState(ipc.Response{Status: ipc.StatusConnected, IP: ""})

	if got := api.getTooltip(); got != "Protégé — IP visible : unknown" {
		t.Errorf("unexpected tooltip: %q", got)
	}
}

func TestTray_UpdateState_Connecting(t *testing.T) {
	api := &mockSystrayAPI{}
	client := &mockIPCClient{}
	tr := newWithDeps(api, newMockMenuAPI(), client, testPollInterval, true, false, false, nil)

	tr.updateTrayState(ipc.Response{Status: ipc.StatusConnecting})

	if got := api.getIcon(); !bytes.Equal(got, IconConnecting) {
		t.Error("expected connecting icon")
	}
	if got := api.getTooltip(); got != "Connexion en cours..." {
		t.Errorf("unexpected tooltip: %q", got)
	}
}

func TestTray_UpdateState_Disconnected(t *testing.T) {
	api := &mockSystrayAPI{}
	client := &mockIPCClient{}
	tr := newWithDeps(api, newMockMenuAPI(), client, testPollInterval, true, false, false, nil)

	tr.updateTrayState(ipc.Response{Status: ipc.StatusDisconnected})

	if got := api.getIcon(); !bytes.Equal(got, IconDisconnected) {
		t.Error("expected disconnected icon")
	}
	if got := api.getTooltip(); got != "Non protégé" {
		t.Errorf("unexpected tooltip: %q", got)
	}
}

func TestTray_UpdateState_Disconnected_WithError(t *testing.T) {
	api := &mockSystrayAPI{}
	client := &mockIPCClient{}
	tr := newWithDeps(api, newMockMenuAPI(), client, testPollInterval, true, false, false, nil)

	tr.updateTrayState(ipc.Response{Status: ipc.StatusDisconnected, Error: "timeout"})

	if got := api.getTooltip(); got != "Non protégé — timeout" {
		t.Errorf("unexpected tooltip: %q", got)
	}
}

func TestTray_UpdateState_Error(t *testing.T) {
	api := &mockSystrayAPI{}
	client := &mockIPCClient{}
	tr := newWithDeps(api, newMockMenuAPI(), client, testPollInterval, true, false, false, nil)

	tr.updateTrayState(ipc.Response{Status: ipc.StatusError, Error: "pipe broken"})

	if got := api.getIcon(); !bytes.Equal(got, IconDisconnected) {
		t.Error("expected disconnected icon for error state")
	}
	if got := api.getTooltip(); got != "Erreur : pipe broken" {
		t.Errorf("unexpected tooltip: %q", got)
	}
}

func TestTray_UpdateState_UnknownStatus(t *testing.T) {
	api := &mockSystrayAPI{}
	client := &mockIPCClient{}
	tr := newWithDeps(api, newMockMenuAPI(), client, testPollInterval, true, false, false, nil)

	tr.updateTrayState(ipc.Response{Status: "weird_status"})

	if got := api.getIcon(); !bytes.Equal(got, IconDisconnected) {
		t.Error("expected disconnected icon for unknown status")
	}
	if got := api.getTooltip(); got != "État inconnu : weird_status" {
		t.Errorf("unexpected tooltip: %q", got)
	}
}

// --- Deduplication test ---

func TestTray_UpdateState_NoUpdateIfSameState(t *testing.T) {
	api := &mockSystrayAPI{}
	client := &mockIPCClient{}
	tr := newWithDeps(api, newMockMenuAPI(), client, testPollInterval, true, false, false, nil)

	resp := ipc.Response{Status: ipc.StatusConnected, IP: "1.2.3.4"}
	tr.updateTrayState(resp)
	callsAfterFirst := api.getCalls()

	tr.updateTrayState(resp)
	callsAfterSecond := api.getCalls()

	if callsAfterSecond != callsAfterFirst {
		t.Errorf("expected no additional API calls on same state, got %d after first and %d after second",
			callsAfterFirst, callsAfterSecond)
	}
}

func TestTray_UpdateState_UpdatesOnDifferentState(t *testing.T) {
	api := &mockSystrayAPI{}
	client := &mockIPCClient{}
	tr := newWithDeps(api, newMockMenuAPI(), client, testPollInterval, true, false, false, nil)

	tr.updateTrayState(ipc.Response{Status: ipc.StatusConnected, IP: "1.2.3.4"})
	callsAfterFirst := api.getCalls()

	tr.updateTrayState(ipc.Response{Status: ipc.StatusDisconnected})
	callsAfterSecond := api.getCalls()

	if callsAfterSecond <= callsAfterFirst {
		t.Error("expected API calls on state change")
	}
}

// --- Connected state tracking tests ---

func TestTray_UpdateState_ConnectedStateTracking(t *testing.T) {
	api := &mockSystrayAPI{}
	client := &mockIPCClient{}
	tr := newWithDeps(api, newMockMenuAPI(), client, testPollInterval, true, false, false, nil)

	tr.updateTrayState(ipc.Response{Status: ipc.StatusConnected, IP: "1.2.3.4"})

	tr.mu.Lock()
	connected := tr.connected
	tr.mu.Unlock()

	if !connected {
		t.Error("expected connected=true after StatusConnected")
	}

	tr.updateTrayState(ipc.Response{Status: ipc.StatusDisconnected})

	tr.mu.Lock()
	connected = tr.connected
	tr.mu.Unlock()

	if connected {
		t.Error("expected connected=false after StatusDisconnected")
	}
}

func TestTray_UpdateState_ConnectingKeepsState(t *testing.T) {
	api := &mockSystrayAPI{}
	client := &mockIPCClient{}
	tr := newWithDeps(api, newMockMenuAPI(), client, testPollInterval, true, false, false, nil)

	// Set initial connected state
	tr.updateTrayState(ipc.Response{Status: ipc.StatusConnected, IP: "1.2.3.4"})

	// Connecting should keep the current state
	tr.updateTrayState(ipc.Response{Status: ipc.StatusConnecting})

	tr.mu.Lock()
	connected := tr.connected
	tr.mu.Unlock()

	if !connected {
		t.Error("expected connected state to be preserved during connecting")
	}
}

// --- handleToggle tests ---

func TestTray_HandleToggle_Connected_SendsDisconnect(t *testing.T) {
	api := &mockSystrayAPI{}
	client := &mockIPCClient{
		response: ipc.Response{Status: ipc.StatusDisconnected},
	}
	tr := newWithDeps(api, newMockMenuAPI(), client, testPollInterval, true, false, false, nil)

	// Set state to connected
	tr.mu.Lock()
	tr.connected = true
	tr.mu.Unlock()

	ctx := context.Background()
	tr.handleToggle(ctx)

	req := client.getLastRequest()
	if req.Action != ipc.ActionDisconnect {
		t.Errorf("expected disconnect action, got %q", req.Action)
	}
}

func TestTray_HandleToggle_Disconnected_SendsConnect(t *testing.T) {
	api := &mockSystrayAPI{}
	client := &mockIPCClient{
		response: ipc.Response{Status: ipc.StatusConnected},
	}
	tr := newWithDeps(api, newMockMenuAPI(), client, testPollInterval, true, false, false, nil)

	// Set state to disconnected
	tr.mu.Lock()
	tr.connected = false
	tr.mu.Unlock()

	ctx := context.Background()
	tr.handleToggle(ctx)

	req := client.getLastRequest()
	if req.Action != ipc.ActionConnect {
		t.Errorf("expected connect action, got %q", req.Action)
	}
}

// --- handleQuit tests ---

func TestTray_HandleQuit_CallsSystrayQuit(t *testing.T) {
	api := &mockSystrayAPI{}
	menuAPI := newMockMenuAPI()
	client := &mockIPCClient{
		response: ipc.Response{Status: ipc.StatusDisconnected},
	}
	tr := newWithDeps(api, menuAPI, client, testPollInterval, true, false, false, nil)

	ctx := context.Background()
	tr.handleQuit(ctx)

	// handleQuit delegates to onExit (via systray.Quit) which sends ActionQuit.
	if !menuAPI.wasQuitCalled() {
		t.Error("expected systray.Quit() to be called")
	}
}

func TestTray_OnExit_RunsShutdown(t *testing.T) {
	api := &mockSystrayAPI{}
	client := &mockIPCClient{
		response: ipc.Response{Status: ipc.StatusDisconnected},
	}
	tr := newWithDeps(api, newMockMenuAPI(), client, testPollInterval, true, false, false, nil)
	_, cancel := context.WithCancel(context.Background())
	tr.cancel = cancel

	tr.onExit()

	tr.mu.Lock()
	done := tr.shutdownDone
	tr.mu.Unlock()
	if !done {
		t.Error("expected shutdownDone to be true after onExit")
	}
}

// --- Polling and reconnect tests ---

func TestTray_ConnectAndPoll_SuccessfulPolling(t *testing.T) {
	api := &mockSystrayAPI{}
	client := &mockIPCClient{
		response: ipc.Response{Status: ipc.StatusConnected, IP: "10.0.0.1"},
	}
	tr := newWithDeps(api, newMockMenuAPI(), client, testPollInterval, true, false, false, nil)

	ctx, cancel := context.WithCancel(context.Background())

	go tr.connectAndPoll(ctx)

	time.Sleep(150 * time.Millisecond)
	cancel()

	if got := api.getTooltip(); got != "Protégé — IP visible : 10.0.0.1" {
		t.Errorf("expected connected tooltip after polling, got: %q", got)
	}
}

func TestTray_ConnectAndPoll_ReconnectsOnError(t *testing.T) {
	api := &mockSystrayAPI{}
	client := &mockIPCClient{
		response: ipc.Response{Status: ipc.StatusConnected, IP: "10.0.0.1"},
	}
	tr := newWithDeps(api, newMockMenuAPI(), client, testPollInterval, true, false, false, nil)

	ctx, cancel := context.WithCancel(context.Background())

	go tr.connectAndPoll(ctx)

	// Wait for initial connect + poll
	time.Sleep(150 * time.Millisecond)

	// Simulate IPC error
	client.setSendErr(fmt.Errorf("ipc: connection reset"))

	// Wait for error handling + reconnect
	time.Sleep(200 * time.Millisecond)

	// Restore IPC
	client.setSendErr(nil)
	client.setResponse(ipc.Response{Status: ipc.StatusDisconnected})

	// Wait for recovery poll
	time.Sleep(200 * time.Millisecond)
	cancel()

	if got := api.getTooltip(); got != "Non protégé" {
		t.Errorf("expected disconnected tooltip after recovery, got: %q", got)
	}
}

// --- Icons embed test ---

func TestIconsEmbedded(t *testing.T) {
	if len(IconConnected) == 0 {
		t.Error("IconConnected is empty")
	}
	if len(IconConnecting) == 0 {
		t.Error("IconConnecting is empty")
	}
	if len(IconDisconnected) == 0 {
		t.Error("IconDisconnected is empty")
	}
}

// --- IPC handler tests (set_auto_start / quit actions) ---

func TestTray_HandleToggle_IPCError_NoStateChange(t *testing.T) {
	api := &mockSystrayAPI{}
	client := &mockIPCClient{
		sendErr: fmt.Errorf("ipc: broken pipe"),
	}
	tr := newWithDeps(api, newMockMenuAPI(), client, testPollInterval, true, false, false, nil)

	tr.mu.Lock()
	tr.connected = true
	tr.mu.Unlock()

	ctx := context.Background()
	tr.handleToggle(ctx)

	// State should remain unchanged
	tr.mu.Lock()
	connected := tr.connected
	tr.mu.Unlock()

	if !connected {
		t.Error("expected connected state to remain after IPC error")
	}
}

// --- handleAutoStartToggle tests ---

func TestTray_HandleAutoStartToggle_SendsSetAutoStart(t *testing.T) {
	api := &mockSystrayAPI{}
	client := &mockIPCClient{
		response: ipc.Response{Status: ipc.StatusOK},
	}
	tr := newWithDeps(api, newMockMenuAPI(), client, testPollInterval, true, false, false, nil)

	ctx := context.Background()
	tr.handleAutoStartToggle(ctx)

	req := client.getLastRequest()
	if req.Action != ipc.ActionSetAutoStart {
		t.Errorf("expected set_auto_start action, got %q", req.Action)
	}
	if req.Value != "false" {
		t.Errorf("expected value 'false' (was autoStart=true), got %q", req.Value)
	}

	tr.mu.Lock()
	autoStart := tr.autoStart
	tr.mu.Unlock()

	if autoStart {
		t.Error("expected autoStart=false after toggle from true")
	}
}

func TestTray_HandleAutoStartToggle_FromFalse_SendsTrue(t *testing.T) {
	api := &mockSystrayAPI{}
	client := &mockIPCClient{
		response: ipc.Response{Status: ipc.StatusOK},
	}
	tr := newWithDeps(api, newMockMenuAPI(), client, testPollInterval, false, false, false, nil)

	ctx := context.Background()
	tr.handleAutoStartToggle(ctx)

	req := client.getLastRequest()
	if req.Value != "true" {
		t.Errorf("expected value 'true' (was autoStart=false), got %q", req.Value)
	}

	tr.mu.Lock()
	autoStart := tr.autoStart
	tr.mu.Unlock()

	if !autoStart {
		t.Error("expected autoStart=true after toggle from false")
	}
}

func TestTray_HandleAutoStartToggle_IPCError_ShowsTooltip(t *testing.T) {
	api := &mockSystrayAPI{}
	client := &mockIPCClient{
		sendErr: fmt.Errorf("ipc: broken pipe"),
	}
	tr := newWithDeps(api, newMockMenuAPI(), client, testPollInterval, true, false, false, nil)

	ctx := context.Background()
	tr.handleAutoStartToggle(ctx)

	if got := api.getTooltip(); got != "Erreur auto-start : ipc: broken pipe" {
		t.Errorf("expected error tooltip, got %q", got)
	}

	tr.mu.Lock()
	autoStart := tr.autoStart
	tr.mu.Unlock()

	if !autoStart {
		t.Error("expected autoStart to remain true after IPC error")
	}
}

// --- handleToggle error feedback test ---

func TestTray_HandleToggle_IPCError_ShowsTooltip(t *testing.T) {
	api := &mockSystrayAPI{}
	client := &mockIPCClient{
		sendErr: fmt.Errorf("ipc: connection refused"),
	}
	tr := newWithDeps(api, newMockMenuAPI(), client, testPollInterval, true, false, false, nil)

	tr.mu.Lock()
	tr.connected = true
	tr.mu.Unlock()

	ctx := context.Background()
	tr.handleToggle(ctx)

	if got := api.getTooltip(); got != "Erreur : ipc: connection refused" {
		t.Errorf("expected error tooltip, got %q", got)
	}
}

// --- handleLeakCheck tests ---

func TestTray_HandleLeakCheck_Pass(t *testing.T) {
	api := &mockSystrayAPI{}
	client := &mockIPCClient{
		response: ipc.Response{Status: ipc.StatusLeakPass, IP: "198.51.100.1"},
	}
	tr := newWithDeps(api, newMockMenuAPI(), client, testPollInterval, true, false, false, nil)

	ctx := context.Background()
	tr.handleLeakCheck(ctx)

	req := client.getLastRequest()
	if req.Action != ipc.ActionLeakCheck {
		t.Errorf("expected leak_check action, got %q", req.Action)
	}

	if got := api.getTooltip(); got != "Aucune fuite détectée — IP STUN : 198.51.100.1" {
		t.Errorf("unexpected tooltip: %q", got)
	}
}

func TestTray_HandleLeakCheck_Fail(t *testing.T) {
	api := &mockSystrayAPI{}
	client := &mockIPCClient{
		response: ipc.Response{Status: ipc.StatusLeakFail, IP: "192.168.1.100"},
	}
	tr := newWithDeps(api, newMockMenuAPI(), client, testPollInterval, true, false, false, nil)

	ctx := context.Background()
	tr.handleLeakCheck(ctx)

	if got := api.getTooltip(); got != "FUITE DÉTECTÉE — IP réelle visible : 192.168.1.100" {
		t.Errorf("unexpected tooltip: %q", got)
	}
}

func TestTray_HandleLeakCheck_IPCError(t *testing.T) {
	api := &mockSystrayAPI{}
	client := &mockIPCClient{
		sendErr: fmt.Errorf("ipc: timeout"),
	}
	tr := newWithDeps(api, newMockMenuAPI(), client, testPollInterval, true, false, false, nil)

	ctx := context.Background()
	tr.handleLeakCheck(ctx)

	if got := api.getTooltip(); got != "Erreur leak check : ipc: timeout" {
		t.Errorf("unexpected tooltip: %q", got)
	}
}

// --- NotifyUpdateReady tests ---

func TestTray_NotifyUpdateReady_SetsTooltip(t *testing.T) {
	api := &mockSystrayAPI{}
	client := &mockIPCClient{}
	tr := newWithDeps(api, newMockMenuAPI(), client, testPollInterval, true, false, false, nil)

	tr.NotifyUpdateReady("2.1.0")

	expected := "Mise à jour v2.1.0 prête — appliquée au prochain démarrage"
	if got := api.getTooltip(); got != expected {
		t.Errorf("tooltip = %q, want %q", got, expected)
	}
}

func TestTray_NotifyUpdateReady_NilMenuItem(t *testing.T) {
	api := &mockSystrayAPI{}
	client := &mockIPCClient{}
	tr := newWithDeps(api, newMockMenuAPI(), client, testPollInterval, true, false, false, nil)

	// menuUpdateReady is nil (not created by mock) — should not panic
	tr.NotifyUpdateReady("3.0.0")

	expected := "Mise à jour v3.0.0 prête — appliquée au prochain démarrage"
	if got := api.getTooltip(); got != expected {
		t.Errorf("tooltip = %q, want %q", got, expected)
	}
}

func TestTray_NotifyRollback(t *testing.T) {
	api := &mockSystrayAPI{}
	client := &mockIPCClient{}
	tr := newWithDeps(api, newMockMenuAPI(), client, testPollInterval, true, false, false, nil)

	tr.NotifyRollback("2.1.0")

	expected := "Mise à jour v2.1.0 annulée — version précédente restaurée"
	if got := api.getTooltip(); got != expected {
		t.Errorf("tooltip = %q, want %q", got, expected)
	}
}

func TestTray_NotifyRollback_Deduplicated(t *testing.T) {
	api := &mockSystrayAPI{}
	client := &mockIPCClient{}
	tr := newWithDeps(api, newMockMenuAPI(), client, testPollInterval, true, false, false, nil)

	tr.NotifyRollback("2.1.0")
	callsAfterFirst := api.getCalls()

	// Second call with same version should be deduplicated
	tr.NotifyRollback("2.1.0")
	callsAfterSecond := api.getCalls()

	if callsAfterSecond != callsAfterFirst {
		t.Errorf("expected no additional calls for duplicate rollback notification, got %d after first and %d after second",
			callsAfterFirst, callsAfterSecond)
	}
}

func TestTray_NotifyRollback_DifferentVersions(t *testing.T) {
	api := &mockSystrayAPI{}
	client := &mockIPCClient{}
	tr := newWithDeps(api, newMockMenuAPI(), client, testPollInterval, true, false, false, nil)

	tr.NotifyRollback("2.1.0")
	callsAfterFirst := api.getCalls()

	// Different version should trigger notification
	tr.NotifyRollback("2.2.0")
	callsAfterSecond := api.getCalls()

	if callsAfterSecond <= callsAfterFirst {
		t.Error("expected additional calls for different rollback version")
	}

	expected := "Mise à jour v2.2.0 annulée — version précédente restaurée"
	if got := api.getTooltip(); got != expected {
		t.Errorf("tooltip = %q, want %q", got, expected)
	}
}

func TestTray_NotifyRollback_NilMenuItem(t *testing.T) {
	api := &mockSystrayAPI{}
	client := &mockIPCClient{}
	tr := newWithDeps(api, newMockMenuAPI(), client, testPollInterval, true, false, false, nil)

	// menuUpdateReady is nil — should not panic
	tr.NotifyRollback("3.0.0")
}

// --- Leak alert tests ---

// TestTray_LeakAlert_IPCError_ResetsFlag: leakAlertActive is reset in handleIPCError (H2 regression test).
func TestTray_LeakAlert_IPCError_ResetsFlag(t *testing.T) {
	api := &mockSystrayAPI{}
	client := &mockIPCClient{
		response: ipc.Response{Status: ipc.StatusConnected, IP: "1.2.3.4"},
	}
	tr := newWithDeps(api, newMockMenuAPI(), client, testPollInterval, true, false, false, nil)

	// Simulate an active leak alert (e.g., from a prior poll).
	tr.leakAlertActive = true

	tr.handleIPCError(context.Background(), fmt.Errorf("ipc: broken pipe"))

	if tr.leakAlertActive {
		t.Error("leakAlertActive should be false after handleIPCError — stale flag blocks future alerts")
	}
}

// TestTray_ConnectAndPoll_LeakAlert_Shown: polling with LeakStatus "fail" triggers alert tooltip (AC3).
func TestTray_ConnectAndPoll_LeakAlert_Shown(t *testing.T) {
	api := &mockSystrayAPI{}
	client := &mockIPCClient{
		response: ipc.Response{
			Status:     ipc.StatusConnected,
			IP:         "10.0.0.1",
			LeakStatus: ipc.StatusLeakFail,
		},
	}
	tr := newWithDeps(api, newMockMenuAPI(), client, testPollInterval, true, false, false, nil)

	ctx, cancel := context.WithCancel(context.Background())
	go tr.connectAndPoll(ctx)

	time.Sleep(150 * time.Millisecond)
	cancel()

	if got := api.getTooltip(); got != "⚠ Fuite détectée — vérification en cours" {
		t.Errorf("expected leak alert tooltip, got: %q", got)
	}
	if !tr.leakAlertActive {
		t.Error("expected leakAlertActive=true after leak detected")
	}
}

// TestTray_ConnectAndPoll_LeakRecovery_ResetsFlag: LeakStatus "pass" after active alert resets the flag.
func TestTray_ConnectAndPoll_LeakRecovery_ResetsFlag(t *testing.T) {
	api := &mockSystrayAPI{}
	client := &mockIPCClient{
		response: ipc.Response{
			Status:     ipc.StatusConnected,
			IP:         "10.0.0.1",
			LeakStatus: ipc.StatusLeakPass,
		},
	}
	tr := newWithDeps(api, newMockMenuAPI(), client, testPollInterval, true, false, false, nil)

	// Seed a pre-existing active alert.
	tr.leakAlertActive = true

	ctx, cancel := context.WithCancel(context.Background())
	go tr.connectAndPoll(ctx)

	time.Sleep(150 * time.Millisecond)
	cancel()

	if tr.leakAlertActive {
		t.Error("expected leakAlertActive=false after recovery (LeakStatus=pass)")
	}
}

// --- handleBlocklistToggle tests ---

func TestTray_HandleBlocklistToggle_FromEnabled_SendsFalse(t *testing.T) {
	api := &mockSystrayAPI{}
	client := &mockIPCClient{
		response: ipc.Response{Status: ipc.StatusOK},
	}
	tr := newWithDeps(api, newMockMenuAPI(), client, testPollInterval, true, true, false, nil)

	ctx := context.Background()
	tr.handleBlocklistToggle(ctx)

	req := client.getLastRequest()
	if req.Action != ipc.ActionSetBlocklist {
		t.Errorf("expected set_blocklist action, got %q", req.Action)
	}
	if req.Value != "false" {
		t.Errorf("expected value 'false' (was blocklistEnabled=true), got %q", req.Value)
	}

	tr.mu.Lock()
	enabled := tr.blocklistEnabled
	tr.mu.Unlock()

	if enabled {
		t.Error("expected blocklistEnabled=false after toggle from true")
	}
}

func TestTray_HandleBlocklistToggle_FromDisabled_SendsTrue(t *testing.T) {
	api := &mockSystrayAPI{}
	client := &mockIPCClient{
		response: ipc.Response{Status: ipc.StatusOK},
	}
	tr := newWithDeps(api, newMockMenuAPI(), client, testPollInterval, true, false, false, nil)

	ctx := context.Background()
	tr.handleBlocklistToggle(ctx)

	req := client.getLastRequest()
	if req.Action != ipc.ActionSetBlocklist {
		t.Errorf("expected set_blocklist action, got %q", req.Action)
	}
	if req.Value != "true" {
		t.Errorf("expected value 'true' (was blocklistEnabled=false), got %q", req.Value)
	}

	tr.mu.Lock()
	enabled := tr.blocklistEnabled
	tr.mu.Unlock()

	if !enabled {
		t.Error("expected blocklistEnabled=true after toggle from false")
	}
}

func TestTray_HandleBlocklistToggle_IPCError_ShowsTooltip(t *testing.T) {
	api := &mockSystrayAPI{}
	client := &mockIPCClient{
		sendErr: fmt.Errorf("ipc: broken pipe"),
	}
	tr := newWithDeps(api, newMockMenuAPI(), client, testPollInterval, true, true, false, nil)

	ctx := context.Background()
	tr.handleBlocklistToggle(ctx)

	if got := api.getTooltip(); got != "Erreur blocklist : ipc: broken pipe" {
		t.Errorf("expected error tooltip, got %q", got)
	}

	tr.mu.Lock()
	enabled := tr.blocklistEnabled
	tr.mu.Unlock()

	if !enabled {
		t.Error("expected blocklistEnabled to remain true after IPC error")
	}
}

// --- handleHTTPProxyToggle tests ---

func TestTray_HandleHTTPProxyToggle_FromDisabled_SendsTrue(t *testing.T) {
	api := &mockSystrayAPI{}
	client := &mockIPCClient{
		response: ipc.Response{
			Status:          ipc.StatusOK,
			HTTPProxyActive: true,
			HTTPProxyAddr:   "127.0.0.1:50113",
			HTTPProxySeq:    1,
		},
	}
	tr := newWithDeps(api, newMockMenuAPI(), client, testPollInterval, true, false, false, nil)

	ctx := context.Background()
	tr.handleHTTPProxyToggle(ctx)

	req := client.getLastRequest()
	if req.Action != ipc.ActionSetHTTPProxy {
		t.Errorf("expected set_http_proxy action, got %q", req.Action)
	}
	if req.Value != "true" {
		t.Errorf("expected value 'true' (was httpProxyEnabled=false), got %q", req.Value)
	}

	tr.mu.Lock()
	enabled := tr.httpProxyEnabled
	seq := tr.httpProxySeq
	tr.mu.Unlock()

	if !enabled {
		t.Error("expected httpProxyEnabled=true after toggle from false")
	}
	if seq != 1 {
		t.Errorf("expected httpProxySeq=1, got %d", seq)
	}
}

func TestTray_HandleHTTPProxyToggle_FromEnabled_SendsFalse(t *testing.T) {
	api := &mockSystrayAPI{}
	client := &mockIPCClient{
		response: ipc.Response{
			Status:          ipc.StatusOK,
			HTTPProxyActive: false,
			HTTPProxyAddr:   "",
			HTTPProxySeq:    2,
		},
	}
	tr := newWithDeps(api, newMockMenuAPI(), client, testPollInterval, true, false, true, nil)

	ctx := context.Background()
	tr.handleHTTPProxyToggle(ctx)

	req := client.getLastRequest()
	if req.Action != ipc.ActionSetHTTPProxy {
		t.Errorf("expected set_http_proxy action, got %q", req.Action)
	}
	if req.Value != "false" {
		t.Errorf("expected value 'false' (was httpProxyEnabled=true), got %q", req.Value)
	}

	tr.mu.Lock()
	enabled := tr.httpProxyEnabled
	tr.mu.Unlock()

	if enabled {
		t.Error("expected httpProxyEnabled=false after toggle from true")
	}
}

func TestTray_HandleHTTPProxyToggle_IPCError_ShowsTooltip(t *testing.T) {
	api := &mockSystrayAPI{}
	client := &mockIPCClient{
		sendErr: fmt.Errorf("ipc: broken pipe"),
	}
	tr := newWithDeps(api, newMockMenuAPI(), client, testPollInterval, true, false, true, nil)

	ctx := context.Background()
	tr.handleHTTPProxyToggle(ctx)

	if got := api.getTooltip(); got != "Erreur proxy HTTP : ipc: broken pipe" {
		t.Errorf("expected error tooltip, got %q", got)
	}

	tr.mu.Lock()
	enabled := tr.httpProxyEnabled
	tr.mu.Unlock()

	if !enabled {
		t.Error("expected httpProxyEnabled to remain true after IPC error")
	}
}

func TestTray_HandleHTTPProxyToggle_ServiceError_ShowsTooltip(t *testing.T) {
	api := &mockSystrayAPI{}
	client := &mockIPCClient{
		response: ipc.Response{Status: ipc.StatusError, Error: "port_in_use"},
	}
	tr := newWithDeps(api, newMockMenuAPI(), client, testPollInterval, true, false, false, nil)

	ctx := context.Background()
	tr.handleHTTPProxyToggle(ctx)

	if got := api.getTooltip(); got != "Erreur proxy HTTP : port_in_use" {
		t.Errorf("expected error tooltip, got %q", got)
	}

	tr.mu.Lock()
	enabled := tr.httpProxyEnabled
	tr.mu.Unlock()

	if enabled {
		t.Error("expected httpProxyEnabled to remain false after service error")
	}
}

func TestTray_ClearUpdateNotification_NilMenuItem(t *testing.T) {
	api := &mockSystrayAPI{}
	client := &mockIPCClient{}
	tr := newWithDeps(api, newMockMenuAPI(), client, testPollInterval, true, false, false, nil)

	// Should not panic with nil menuUpdateReady
	tr.ClearUpdateNotification()
}

func TestDesktopExePath(t *testing.T) {
	p := desktopExePath()
	if p == "" {
		t.Fatal("expected non-empty path")
	}
	// Must end with le-voile-desktop.exe
	if !strings.HasSuffix(p, "le-voile-desktop.exe") {
		t.Errorf("expected path ending in le-voile-desktop.exe, got %q", p)
	}
}
