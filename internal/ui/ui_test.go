package ui

import (
	"context"
	"sync"
	"testing"
	"time"

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

// --- Story 5.5 (revised): tray-toggle 2-state behavior ---------------------

// TestHandleTrayToggle_NoopWhenAbsent verifies that when the webview is not
// open (app not yet started, or after quit), handleTrayToggle is a pure no-op
// — no channel sends, no panic on nil channels. This mirrors the post-close
// state: the webview is never recreated, the tray click is simply ignored
// until the process exits.
func TestHandleTrayToggle_NoopWhenAbsent(t *testing.T) {
	u := &UI{} // no channels allocated, webviewOpen=false
	u.handleTrayToggle()
	// If we reach this line without panic, the test passes.
}

// TestHandleTrayToggle_ShowsWhenHidden verifies the "hidden → show" branch:
// when webviewOpen=true and webviewHidden=true, a signal is sent on showCh
// and nothing on hideCh.
func TestHandleTrayToggle_ShowsWhenHidden(t *testing.T) {
	u := &UI{
		showCh: make(chan struct{}, 1),
		hideCh: make(chan struct{}, 1),
	}
	u.webviewOpen.Store(true)
	u.webviewHidden.Store(true)

	u.handleTrayToggle()

	select {
	case <-u.showCh:
	default:
		t.Error("expected signal on showCh (hidden → show)")
	}
	select {
	case <-u.hideCh:
		t.Error("hideCh should be empty")
	default:
	}
}

// TestHandleTrayToggle_HidesWhenVisible verifies the "visible → hide" branch:
// when webviewOpen=true and webviewHidden=false, a signal is sent on hideCh
// and nothing on showCh.
func TestHandleTrayToggle_HidesWhenVisible(t *testing.T) {
	u := &UI{
		showCh: make(chan struct{}, 1),
		hideCh: make(chan struct{}, 1),
	}
	u.webviewOpen.Store(true)
	u.webviewHidden.Store(false)

	u.handleTrayToggle()

	select {
	case <-u.hideCh:
	default:
		t.Error("expected signal on hideCh (visible → hide)")
	}
	select {
	case <-u.showCh:
		t.Error("showCh should be empty")
	default:
	}
}

// TestOnWebviewClosed_NoopOnFalseQuit verifies the ✕=quit gate: when the
// webview exits without a quit request (e.g. process-level Terminate during
// shutdown), onWebviewClosed must NOT trigger the quit function.
func TestOnWebviewClosed_NoopOnFalseQuit(t *testing.T) {
	var calls int
	u := &UI{quitFn: func() { calls++ }}

	u.onWebviewClosed(false)

	if calls != 0 {
		t.Errorf("quitFn called %d times, want 0 (quit=false)", calls)
	}
}

// TestOnWebviewClosed_NoopWhenShutdownInProgress verifies the re-entry guard:
// if we're already inside a shutdown path (e.g. tray "Quitter" was clicked
// just before ✕), onWebviewClosed must not call quitFn again.
func TestOnWebviewClosed_NoopWhenShutdownInProgress(t *testing.T) {
	var calls int
	u := &UI{quitFn: func() { calls++ }}
	u.shutdownInProgress.Store(true)

	u.onWebviewClosed(true)

	if calls != 0 {
		t.Errorf("quitFn called %d times during shutdown, want 0", calls)
	}
}

// TestOnWebviewClosed_QuitsOnTrueQuit verifies the happy path: ✕ (quit=true)
// with no in-flight shutdown triggers exactly one quitFn invocation.
func TestOnWebviewClosed_QuitsOnTrueQuit(t *testing.T) {
	var calls int
	u := &UI{quitFn: func() { calls++ }}

	u.onWebviewClosed(true)

	if calls != 1 {
		t.Errorf("quitFn called %d times, want 1", calls)
	}
}

// TestHandleTrayToggle_DropsWhenChannelFull verifies rapid clicks coalesce:
// handleTrayToggle must never block when the target channel is already full.
func TestHandleTrayToggle_DropsWhenChannelFull(t *testing.T) {
	u := &UI{
		showCh: make(chan struct{}, 1),
		hideCh: make(chan struct{}, 1),
	}
	u.webviewOpen.Store(true)
	u.webviewHidden.Store(false)

	// Pre-fill hideCh so the next send must be dropped.
	u.hideCh <- struct{}{}

	done := make(chan struct{})
	go func() {
		u.handleTrayToggle()
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(200 * time.Millisecond):
		t.Fatal("handleTrayToggle blocked on full channel")
	}
}

// --- Story 5.6 (retry IPC cadence + shutdown responsiveness) --------------

// TestIPCRetryInterval_Is5s locks the FR13c contract: the IPC retry cadence
// MUST be a fixed 5 seconds. Any drift (exponential backoff reintroduced,
// unit mismatch) fails here loudly instead of silently worsening the
// fallback-screen UX.
func TestIPCRetryInterval_Is5s(t *testing.T) {
	if ipcRetryInterval != 5*time.Second {
		t.Errorf("ipcRetryInterval = %v, want 5s (Story 5.6 AC3 / FR13c)", ipcRetryInterval)
	}
}

// TestReconnectIPC_ReturnsOnSuccess verifies the happy path: once Connect()
// succeeds, reconnectIPC returns without ever waiting.
func TestReconnectIPC_ReturnsOnSuccess(t *testing.T) {
	client := &mockIPCClientReconnect{connectErr: nil}
	u := &UI{client: NewSafeIPCClient(client)}

	done := make(chan struct{})
	go func() {
		u.reconnectIPC(context.Background())
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(200 * time.Millisecond):
		t.Fatal("reconnectIPC did not return immediately on successful Connect()")
	}
}

// TestReconnectIPC_RespectsContextCancellation guards Story 5.6 AC6: when the
// user quits, the retry loop must abort promptly on ctx cancellation rather
// than blocking for the full 5 s interval. Validates that shutdown is never
// held up by an in-flight retry wait.
//
// Uses the onRetryWait hook instead of a timing-based Sleep (review finding
// M2) so the test is deterministic on slow CI hosts.
func TestReconnectIPC_RespectsContextCancellation(t *testing.T) {
	client := &mockIPCClientReconnect{connectErr: errConnectRefused}
	entered := make(chan struct{}, 1)
	u := &UI{
		client: NewSafeIPCClient(client),
		onRetryWait: func() {
			// Non-blocking: one signal is enough; subsequent entries (should
			// not happen in this test) are dropped to keep the loop flowing.
			select {
			case entered <- struct{}{}:
			default:
			}
		},
	}

	ctx, cancel := context.WithCancel(context.Background())

	done := make(chan struct{})
	go func() {
		u.reconnectIPC(ctx)
		close(done)
	}()

	// Wait until reconnectIPC has actually reached the 5 s wait, THEN cancel.
	// The signal proves the goroutine is parked on time.After; ctx.Done must
	// unblock it quickly.
	select {
	case <-entered:
	case <-time.After(500 * time.Millisecond):
		t.Fatal("reconnectIPC never entered the retry wait within 500ms")
	}
	cancel()

	select {
	case <-done:
	case <-time.After(500 * time.Millisecond):
		t.Fatal("reconnectIPC did not return within 500ms of ctx cancel — blocked on full retry interval")
	}
}

// errConnectRefused is a sentinel used by reconnect tests where the mock
// must keep failing so the loop enters its wait branch.
var errConnectRefused = &refusedErr{}

type refusedErr struct{}

func (*refusedErr) Error() string { return "connection refused" }

// --- Story 5.9: Mode dégradé tray override ---

// AC3 — when killswitch_mode=degraded, the tray icon stays red even if the
// tunnel reports connected, and the tooltip switches to the dedicated message.
func TestUpdateTrayState_DegradedOverride_OverridesConnected(t *testing.T) {
	api := &mockSystrayAPI{}
	u := &UI{api: api, client: NewSafeIPCClient(&mockIPCClient{})}

	resp := ipc.Response{
		Status:         ipc.StatusConnected,
		IP:             "1.2.3.4",
		Country:        "Allemagne",
		KillSwitchMode: ipc.KillSwitchModeDegraded,
	}
	u.updateTrayState(resp)

	if api.getTooltip() != "Mode dégradé — protection désactivée" {
		t.Errorf("tooltip = %q, want degraded override", api.getTooltip())
	}
	u.mu.Lock()
	connected := u.connected
	u.mu.Unlock()
	if connected {
		t.Error("connected must be false in degraded mode (tray must read non-protected)")
	}
}

// AC3 — leaving degraded mode while still connected restores the normal icon.
// Verifies the stateKey debounce includes KillSwitchMode.
func TestUpdateTrayState_LeaveDegraded_RestoresConnectedTooltip(t *testing.T) {
	api := &mockSystrayAPI{}
	u := &UI{api: api, client: NewSafeIPCClient(&mockIPCClient{})}

	// First poll: degraded.
	u.updateTrayState(ipc.Response{
		Status:         ipc.StatusConnected,
		IP:             "1.2.3.4",
		Country:        "Allemagne",
		KillSwitchMode: ipc.KillSwitchModeDegraded,
	})
	// Second poll: same tunnel state but mode lifted to normal — must update.
	u.updateTrayState(ipc.Response{
		Status:         ipc.StatusConnected,
		IP:             "1.2.3.4",
		Country:        "Allemagne",
		KillSwitchMode: ipc.KillSwitchModeNormal,
	})

	tooltip := api.getTooltip()
	if tooltip != "Protégé — Allemagne — IP : 1.2.3.4" {
		t.Errorf("tooltip after degraded→normal = %q, want connected tooltip", tooltip)
	}
}

// AC1 — TriggerUIEvent + /api/ui-event have read-and-clear semantics, used by
// handleKillSwitchMenu to ask the frontend to display the destructive modal.
func TestTriggerUIEvent_ReadAndClear(t *testing.T) {
	srv := NewHTTPServer(NewSafeIPCClient(&mockIPCClient{}), nil, "")
	srv.TriggerUIEvent("killswitch_modal")

	if got := srv.pendingUIEvent.take(); got != "killswitch_modal" {
		t.Errorf("first take = %q, want killswitch_modal", got)
	}
	if got := srv.pendingUIEvent.take(); got != "" {
		t.Errorf("second take = %q, want empty (read-and-clear)", got)
	}
}

// --- Story 8.1 (AC8): tray "Mise à jour disponible" notification -----------

// mockUpdateMenu records calls so tests can assert visibility + title.
type mockUpdateMenu struct {
	mu      sync.Mutex
	title   string
	tooltip string
	visible bool
	calls   int // total state-mutating calls (SetTitle/SetTooltip/Show/Hide)
}

func (m *mockUpdateMenu) SetTitle(t string) {
	m.mu.Lock()
	m.title = t
	m.calls++
	m.mu.Unlock()
}
func (m *mockUpdateMenu) SetTooltip(t string) {
	m.mu.Lock()
	m.tooltip = t
	m.calls++
	m.mu.Unlock()
}
func (m *mockUpdateMenu) Show() {
	m.mu.Lock()
	m.visible = true
	m.calls++
	m.mu.Unlock()
}
func (m *mockUpdateMenu) Hide() {
	m.mu.Lock()
	m.visible = false
	m.calls++
	m.mu.Unlock()
}
func (m *mockUpdateMenu) snapshot() (string, string, bool, int) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.title, m.tooltip, m.visible, m.calls
}

// AC8 #1 — first "update_ready" detection populates the menu and shows it.
func TestUpdateTrayState_UpdateAvailable_FirstDetection(t *testing.T) {
	api := &mockSystrayAPI{}
	menu := &mockUpdateMenu{}
	u := &UI{
		api:        api,
		client:     NewSafeIPCClient(&mockIPCClient{}),
		menuUpdate: menu,
	}

	resp := ipc.Response{
		Status:        ipc.StatusConnected,
		IP:            "1.2.3.4",
		Country:       "Islande",
		UpdateStatus:  ipc.StatusUpdateReady,
		UpdateVersion: "1.4.2",
	}
	u.updateTrayState(resp)

	title, tooltip, visible, _ := menu.snapshot()
	if want := "Mise à jour disponible : v1.4.2"; title != want {
		t.Errorf("menu title = %q, want %q", title, want)
	}
	if want := "Téléchargée et vérifiée — sera appliquée au prochain redémarrage du service"; tooltip != want {
		t.Errorf("menu tooltip = %q, want %q", tooltip, want)
	}
	if !visible {
		t.Error("expected menu visible=true after first detection")
	}
	u.mu.Lock()
	got := u.lastShownUpdateVersion
	u.mu.Unlock()
	if got != "1.4.2" {
		t.Errorf("lastShownUpdateVersion = %q, want 1.4.2", got)
	}
}

// AC8 #2 — repolling with the same version is idempotent: no further calls.
func TestUpdateTrayState_UpdateAvailable_NoDuplicateOnSameVersion(t *testing.T) {
	api := &mockSystrayAPI{}
	menu := &mockUpdateMenu{}
	u := &UI{
		api:        api,
		client:     NewSafeIPCClient(&mockIPCClient{}),
		menuUpdate: menu,
	}

	resp := ipc.Response{
		Status:        ipc.StatusConnected,
		IP:            "1.2.3.4",
		Country:       "Islande",
		UpdateStatus:  ipc.StatusUpdateReady,
		UpdateVersion: "1.4.2",
	}
	u.updateTrayState(resp)
	_, _, _, callsAfterFirst := menu.snapshot()

	// Second call — exact same response.
	u.updateTrayState(resp)
	_, _, visible, callsAfterSecond := menu.snapshot()

	if callsAfterSecond != callsAfterFirst {
		t.Errorf("expected no menu mutations on duplicate state, got %d → %d calls", callsAfterFirst, callsAfterSecond)
	}
	if !visible {
		t.Error("menu should remain visible across duplicate updates")
	}
}

// AC8 #3 — when UpdateStatus disappears (e.g. version installed) the menu hides.
func TestUpdateTrayState_UpdateAvailable_HidesWhenStatusClears(t *testing.T) {
	api := &mockSystrayAPI{}
	menu := &mockUpdateMenu{}
	u := &UI{
		api:        api,
		client:     NewSafeIPCClient(&mockIPCClient{}),
		menuUpdate: menu,
	}

	// First: update_ready.
	u.updateTrayState(ipc.Response{
		Status:        ipc.StatusConnected,
		Country:       "Islande",
		UpdateStatus:  ipc.StatusUpdateReady,
		UpdateVersion: "1.4.2",
	})
	if _, _, visible, _ := menu.snapshot(); !visible {
		t.Fatal("precondition: menu should be visible after first update_ready")
	}

	// Second: status cleared.
	u.updateTrayState(ipc.Response{
		Status:  ipc.StatusConnected,
		Country: "Islande",
	})
	_, _, visible, _ := menu.snapshot()
	if visible {
		t.Error("expected menu hidden after UpdateStatus clears")
	}
	u.mu.Lock()
	got := u.lastShownUpdateVersion
	u.mu.Unlock()
	if got != "" {
		t.Errorf("lastShownUpdateVersion = %q, want empty after clear", got)
	}
}

// AC8 #4 — a NEW version (e.g. after a rollback skip then later release) updates the menu.
func TestUpdateTrayState_UpdateAvailable_NewVersionRefreshesMenu(t *testing.T) {
	api := &mockSystrayAPI{}
	menu := &mockUpdateMenu{}
	u := &UI{
		api:        api,
		client:     NewSafeIPCClient(&mockIPCClient{}),
		menuUpdate: menu,
	}

	u.updateTrayState(ipc.Response{
		Status:        ipc.StatusConnected,
		UpdateStatus:  ipc.StatusUpdateReady,
		UpdateVersion: "1.4.2",
	})
	u.updateTrayState(ipc.Response{
		Status:        ipc.StatusConnected,
		UpdateStatus:  ipc.StatusUpdateReady,
		UpdateVersion: "1.4.3",
	})

	title, _, visible, _ := menu.snapshot()
	if want := "Mise à jour disponible : v1.4.3"; title != want {
		t.Errorf("menu title = %q, want %q", title, want)
	}
	if !visible {
		t.Error("expected menu visible after version bump")
	}
	u.mu.Lock()
	got := u.lastShownUpdateVersion
	u.mu.Unlock()
	if got != "1.4.3" {
		t.Errorf("lastShownUpdateVersion = %q, want 1.4.3", got)
	}
}

// Code review M5 — when an update was visible and the next poll surfaces a
// rollback (Story 8.2 boundary), the tray entry must hide. Story 8.1 owns
// the entry; rollback UX lives in the webview banner. This test pins the
// boundary so a future Story 8.2 change can't silently leak the tray menu
// into rollback territory.
func TestUpdateTrayState_UpdateAvailable_HidesOnRollbackTransition(t *testing.T) {
	api := &mockSystrayAPI{}
	menu := &mockUpdateMenu{}
	u := &UI{
		api:        api,
		client:     NewSafeIPCClient(&mockIPCClient{}),
		menuUpdate: menu,
	}

	// Step 1: update_ready surfaces normally.
	u.updateTrayState(ipc.Response{
		Status:        ipc.StatusConnected,
		UpdateStatus:  ipc.StatusUpdateReady,
		UpdateVersion: "1.4.2",
	})
	if _, _, visible, _ := menu.snapshot(); !visible {
		t.Fatal("precondition: menu visible after update_ready")
	}

	// Step 2: next poll reports rollback for that same version.
	u.updateTrayState(ipc.Response{
		Status:          ipc.StatusConnected,
		UpdateStatus:    ipc.StatusRollback,
		RollbackVersion: "1.4.2",
		RollbackReason:  "tunnel timeout post-restart",
	})
	_, _, visible, _ := menu.snapshot()
	if visible {
		t.Error("expected menu hidden once UpdateStatus transitions to rollback")
	}
	u.mu.Lock()
	got := u.lastShownUpdateVersion
	u.mu.Unlock()
	if got != "" {
		t.Errorf("lastShownUpdateVersion = %q, want empty after rollback transition", got)
	}
}

// AC8 — when no menu is wired (tests / packaging without update menu), state
// updates are still safe.
func TestUpdateTrayState_UpdateAvailable_NoMenuWiredIsNoop(t *testing.T) {
	api := &mockSystrayAPI{}
	u := &UI{
		api:    api,
		client: NewSafeIPCClient(&mockIPCClient{}),
	}
	// menuUpdate is nil — must not panic.
	u.updateTrayState(ipc.Response{
		Status:        ipc.StatusConnected,
		UpdateStatus:  ipc.StatusUpdateReady,
		UpdateVersion: "1.4.2",
	})
	// If we reach here without panic, the noop guard works.
}
