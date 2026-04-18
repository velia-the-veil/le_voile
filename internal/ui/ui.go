package ui

import (
	"context"
	"fmt"
	"io/fs"
	"runtime"
	"sync"
	"sync/atomic"
	"time"

	"fyne.io/systray"
	"github.com/velia-the-veil/le_voile/internal/ipc"
)

const (
	pollInterval = 2 * time.Second
	// ipcRetryInterval is the fixed cadence at which the UI re-attempts to
	// reach the service when IPC is down (Story 5.6 AC3 / FR13c). Do NOT
	// re-introduce exponential backoff — the 5 s fixed interval is part of
	// the user-facing contract and matches the discreet spinner shown on
	// the "Service non démarré" fallback screen.
	ipcRetryInterval = 5 * time.Second
	// uiDisconnectTimeout bounds how long the shutdown sequence waits for
	// the service to acknowledge the UI-quit notification. The service
	// response is purely informational (no lifecycle work), so a short
	// timeout is fine — if the service is unreachable we exit anyway.
	uiDisconnectTimeout = 2 * time.Second
)

// IPCClient abstracts the IPC client for testability.
type IPCClient interface {
	Connect() error
	Close() error
	SendContext(ctx context.Context, req ipc.Request) (ipc.Response, error)
}

// SystrayAPI abstracts systray calls for testability.
type SystrayAPI interface {
	SetIcon(iconBytes []byte)
	SetTooltip(tooltip string)
	SetTitle(title string)
}

// SystrayMenuAPI abstracts systray menu operations for testability.
type SystrayMenuAPI interface {
	AddMenuItem(title, tooltip string) *systray.MenuItem
	AddSeparator()
	Quit()
}

// updateMenuController is the minimal surface of a tray menu item used by
// updateTrayState to drive the "Mise à jour disponible" entry (Story 8.1 AC8).
// *systray.MenuItem satisfies it natively; tests inject a mock to assert
// Show/Hide/SetTitle/SetTooltip without touching the real systray loop.
type updateMenuController interface {
	SetTitle(string)
	SetTooltip(string)
	Show()
	Hide()
}

type defaultSystrayAPI struct{}

func (defaultSystrayAPI) SetIcon(iconBytes []byte)  { systray.SetIcon(iconBytes) }
func (defaultSystrayAPI) SetTooltip(tooltip string) { systray.SetTooltip(tooltip) }
func (defaultSystrayAPI) SetTitle(title string)     { systray.SetTitle(title) }

type defaultSystrayMenuAPI struct{}

func (defaultSystrayMenuAPI) AddMenuItem(title, tooltip string) *systray.MenuItem {
	return systray.AddMenuItem(title, tooltip)
}
func (defaultSystrayMenuAPI) AddSeparator() { systray.AddSeparator() }
func (defaultSystrayMenuAPI) Quit()         { systray.Quit() }

// Config holds UI configuration.
type Config struct {
	RelayDomain string
	FrontendFS  fs.FS // embedded frontend assets
}

// UI combines systray + webview + local HTTP server in a single process.
type UI struct {
	api     SystrayAPI
	menuAPI SystrayMenuAPI
	client  *SafeIPCClient
	config  Config

	httpServer *HTTPServer
	cancel     context.CancelFunc

	mu                 sync.Mutex
	last               string // last known state to avoid redundant updates
	connected          bool
	shutdownInProgress atomic.Bool
	shutdownOnce       sync.Once

	// webviewTerminate terminates the webview event loop (if open).
	// Protected by mu.
	webviewTerminate func()

	sysProxy       *SysProxy
	webviewOpen    atomic.Bool   // prevents multiple windows
	webviewHidden  atomic.Bool   // true while the webview is hidden to tray (SW_HIDE)
	sysProxyActive atomic.Bool   // tracks whether WinINET is currently set to Le Voile proxy
	showCh         chan struct{} // signals webview to show itself (buffered 1, replaced per open)
	hideCh         chan struct{} // signals webview to hide to tray (buffered 1, replaced per open)

	// quitFn is invoked by onWebviewClosed when ✕ requests app quit.
	// Defaults to u.handleQuit; overridable for testing.
	quitFn func()

	// onRetryWait is invoked right before reconnectIPC enters the 5 s wait.
	// Nil in production; tests set it to coordinate timing without relying on
	// Sleep (Story 5.6 review finding M2).
	onRetryWait func()

	menuOpen       *systray.MenuItem
	menuKillSwitch *systray.MenuItem
	menuQuit       *systray.MenuItem

	// Story 8.1 AC8 — "Mise à jour disponible" tray entry. Hidden by default
	// (Hide() called once in onReady), shown by updateTrayState when the IPC
	// poll surfaces UpdateStatus="update_ready". `lastShownUpdateVersion`
	// (under mu) prevents redundant SetTitle/Show calls when polling repeats
	// the same version. menuUpdateClicked stays nil in tests; the menuHandler
	// substitutes neverFiresChan, mirroring the menuKillSwitch pattern.
	menuUpdate        updateMenuController
	menuUpdateClicked chan struct{}
	lastShownUpdateVersion string
}

// New creates a UI instance with real systray API.
func New(client IPCClient, cfg Config) *UI {
	u := &UI{
		api:      defaultSystrayAPI{},
		menuAPI:  defaultSystrayMenuAPI{},
		client:   NewSafeIPCClient(client),
		config:   cfg,
		sysProxy: NewSysProxy(cfg.RelayDomain),
	}
	u.quitFn = u.handleQuit
	return u
}

// newWithDeps creates a UI with injected dependencies (for testing).
func newWithDeps(api SystrayAPI, menuAPI SystrayMenuAPI, client IPCClient, cfg Config, sysProxy *SysProxy) *UI {
	u := &UI{
		api:      api,
		menuAPI:  menuAPI,
		client:   NewSafeIPCClient(client),
		config:   cfg,
		sysProxy: sysProxy,
	}
	u.quitFn = u.handleQuit
	return u
}

// Run starts the systray event loop. Blocks and must be called from the main goroutine.
func (u *UI) Run() {
	systray.Run(u.onReady, u.onExit)
}

func (u *UI) onReady() {
	u.api.SetIcon(IconDefault)
	u.api.SetTooltip("Non protégé")
	u.api.SetTitle("Le Voile")

	// Left-click on the tray icon toggles the webview. We gate this to Windows
	// only: fyne.io/systray sets ItemIsMenu=false on Linux as soon as any
	// tapped callback is registered (systray_unix.go:368), which breaks the
	// natural "right-click opens menu" default on libayatana-appindicator.
	// On Linux the user interacts with the window through the context menu's
	// "Ouvrir la fenêtre" entry — see Story 5.5 AC4 + review M1.
	if runtime.GOOS == "windows" {
		systray.SetOnTapped(u.handleTrayToggle)
	}

	// Menu items — French UI.
	u.menuOpen = u.menuAPI.AddMenuItem("Ouvrir la fenêtre", "")
	// Story 8.1 AC8 — update notification entry. Created with a placeholder
	// title and immediately hidden; updateTrayState reveals it when an update
	// is staged. Inserted between "Ouvrir la fenêtre" and "Mode dégradé" so
	// the destructive kill-switch entry stays at the bottom of the action
	// group (visual hierarchy: passive notice → destructive action).
	updateMenuItem := u.menuAPI.AddMenuItem("Mise à jour disponible", "")
	updateMenuItem.Hide()
	u.menuUpdate = updateMenuItem
	u.menuUpdateClicked = updateMenuItem.ClickedCh
	// Story 5.9 — destructive entry: opens the webview and asks the frontend
	// to display the confirmation modal. The actual switch only happens after
	// the user clicks "Continuer" in that modal (no optimistic state).
	u.menuKillSwitch = u.menuAPI.AddMenuItem("Mode dégradé", "Désactiver temporairement la protection")
	u.menuAPI.AddSeparator()
	u.menuQuit = u.menuAPI.AddMenuItem("Quitter", "Quitter l'interface (la protection reste active)")

	// Recover orphaned WinINET proxy from a previous crash.
	if u.sysProxy != nil {
		u.sysProxy.RecoverOrphan()
	}

	ctx, cancel := context.WithCancel(context.Background())
	u.cancel = cancel

	// Start HTTP server for webview.
	u.httpServer = NewHTTPServer(u.client, u.config.FrontendFS)
	go u.httpServer.Start(ctx)

	go u.connectAndPoll(ctx)
	go u.menuHandler(ctx)

	// Open webview once HTTP server is ready (in goroutine to avoid blocking systray).
	go func() {
		u.httpServer.Addr() // blocks until listener is bound
		u.handleOpenWebview()
	}()
}

func (u *UI) onExit() {
	u.shutdown()
}

func (u *UI) menuHandler(ctx context.Context) {
	for {
		// menuKillSwitch / menuUpdate may be nil in tests that don't construct
		// them via the real onReady. Substitute the never-fires sentinel so
		// the select keeps all branches balanced in production and tests.
		killCh := neverFiresChan
		if u.menuKillSwitch != nil {
			killCh = u.menuKillSwitch.ClickedCh
		}
		updateCh := neverFiresChan
		if u.menuUpdateClicked != nil {
			updateCh = u.menuUpdateClicked
		}
		select {
		case <-ctx.Done():
			return
		case <-u.menuOpen.ClickedCh:
			u.handleOpenWebview()
		case <-killCh:
			u.handleKillSwitchMenu()
		case <-updateCh:
			u.handleUpdateMenu()
		case <-u.menuQuit.ClickedCh:
			u.handleQuit()
		}
	}
}

// handleUpdateMenu opens (or wakes) the webview so the user sees the
// "update_ready" banner immediately and broadcasts a one-shot UI event the
// frontend can consume to scroll/highlight the update affordance. Story 8.1
// AC8 — non-intrusive: no OS-level notification, no IPC side-effect.
func (u *UI) handleUpdateMenu() {
	if u.httpServer != nil {
		u.httpServer.TriggerUIEvent("update_available")
	}
	u.handleOpenWebview()
}

// neverFiresChan is a sentinel of the same type as systray.MenuItem.ClickedCh
// that NEVER receives a value (and is intentionally never closed). Used as a
// placeholder for nil menu items so the menuHandler select stays balanced
// without spuriously firing the kill-switch case in tests that don't
// construct that menu entry. Story 5.9 L1 — renamed from the misleading
// "closedChan" which suggested it might be closed.
var neverFiresChan = make(chan struct{})

// handleKillSwitchMenu opens (or shows) the webview and queues a one-shot
// "killswitch_modal" UI event for the frontend to consume on next poll.
// The actual mode switch happens only after the user confirms via the modal —
// no IPC is dispatched here. Story 5.9 AC1.
func (u *UI) handleKillSwitchMenu() {
	if u.httpServer != nil {
		u.httpServer.TriggerUIEvent("killswitch_modal")
	}
	u.handleOpenWebview()
}

// handleOpenWebview creates the webview once per app lifetime, or signals an
// existing (possibly hidden) window to show and come to the foreground. The
// window is NEVER recreated after destruction: closing via ✕ quits the whole
// app (see the goroutine's post-openWebview handleQuit). This avoids a
// WebView2 recreation bug that rendered the second window blank.
func (u *UI) handleOpenWebview() {
	if u.httpServer == nil {
		return
	}
	if !u.webviewOpen.CompareAndSwap(false, true) {
		// Already open — signal "show + foreground" (no-op if already shown).
		u.mu.Lock()
		ch := u.showCh
		u.mu.Unlock()
		if ch != nil {
			select {
			case ch <- struct{}{}:
			default:
			}
		}
		return
	}
	addr := u.httpServer.Addr()
	if addr == "" {
		u.webviewOpen.Store(false)
		return
	}
	showCh := make(chan struct{}, 1)
	hideCh := make(chan struct{}, 1)
	u.mu.Lock()
	u.showCh = showCh
	u.hideCh = hideCh
	u.mu.Unlock()
	u.webviewHidden.Store(false)
	go func() {
		defer u.webviewOpen.Store(false)
		defer u.webviewHidden.Store(false)
		// Release the channel references once the webview is gone so GC can
		// reclaim them and handleTrayToggle sees nil (noop guard).
		defer func() {
			u.mu.Lock()
			u.showCh = nil
			u.hideCh = nil
			u.mu.Unlock()
		}()
		quit := openWebview(addr,
			func(terminate func()) {
				u.mu.Lock()
				u.webviewTerminate = terminate
				u.mu.Unlock()
			},
			func() {
				u.mu.Lock()
				u.webviewTerminate = nil
				u.mu.Unlock()
			},
			showCh,
			hideCh,
			func(hidden bool) { u.webviewHidden.Store(hidden) },
		)
		u.onWebviewClosed(quit)
	}()
}

// onWebviewClosed is invoked once per webview lifecycle after openWebview
// returns. When ✕ was clicked (quit=true) and we're not already shutting down,
// drive a full app shutdown via quitFn (defaults to handleQuit; overridable
// for tests). Extracted for testability — see TestOnWebviewClosed_*.
func (u *UI) onWebviewClosed(quit bool) {
	if !quit {
		return
	}
	if u.shutdownInProgress.Load() {
		return
	}
	if u.quitFn != nil {
		u.quitFn()
	}
}

// handleTrayToggle is bound to systray.SetOnTapped (left-click). Two-state
// behavior only — the webview is never recreated after close, so when the
// webview is absent (app shutting down or not yet ready) the click is a no-op:
//
//	hidden  → show + bring to front (signal on showCh)
//	visible → hide to tray           (signal on hideCh)
//
// Sends are non-blocking so rapid clicks coalesce.
func (u *UI) handleTrayToggle() {
	if !u.webviewOpen.Load() {
		return
	}
	u.mu.Lock()
	var ch chan struct{}
	if u.webviewHidden.Load() {
		ch = u.showCh
	} else {
		ch = u.hideCh
	}
	u.mu.Unlock()
	if ch == nil {
		return
	}
	select {
	case ch <- struct{}{}:
	default:
	}
}

func (u *UI) handleQuit() {
	u.shutdown()
	u.menuAPI.Quit()
}

const httpShutdownTimeout = 3 * time.Second

// shutdown performs the UI-only shutdown sequence. Idempotent via sync.Once.
// Story 5.8: the UI process exits independently of the service. The service
// keeps the tunnel, firewall, routing and TUN alive under systemd/SCM control
// until someone explicitly stops it (`sc stop levoile-service` /
// `systemctl stop levoile.service`).
// Sequence: shutdownInProgress → destroy webview → restore proxy →
// shutdown HTTP server → ActionUIDisconnect notification → cancel ctx → close IPC.
func (u *UI) shutdown() {
	u.shutdownOnce.Do(u.doShutdown)
}

func (u *UI) doShutdown() {
	// 1. Block orphan recovery in handleIPCError.
	u.shutdownInProgress.Store(true)

	// 2. Terminate webview if open.
	u.mu.Lock()
	terminate := u.webviewTerminate
	u.webviewTerminate = nil
	u.mu.Unlock()
	if terminate != nil {
		terminate()
	}

	// 3. Restore WinINET proxy (UI owns this resource).
	// Try Restore first, then verify proxy is no longer ours.
	if u.sysProxy != nil {
		u.sysProxy.Restore()
		if u.sysProxy.IsOurProxyActive() {
			u.sysProxy.ForceDisable()
		}
	}

	// 4. Shutdown HTTP server (3s timeout).
	if u.httpServer != nil {
		shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), httpShutdownTimeout)
		u.httpServer.Shutdown(shutdownCtx)
		shutdownCancel()
	}

	// 5. Notify the service that this UI is going away. The service
	// acknowledges with StatusOK and does NOT stop the tunnel — Story 5.8
	// AC1/AC5. The short timeout is deliberate: we're exiting the UI
	// either way, the notification is best-effort.
	disconnectCtx, disconnectCancel := context.WithTimeout(context.Background(), uiDisconnectTimeout)
	u.client.SendContext(disconnectCtx, ipc.Request{Action: ipc.ActionUIDisconnect})
	disconnectCancel()

	// 6. Cancel polling context and close IPC connection.
	if u.cancel != nil {
		u.cancel()
	}
	u.client.Close()
}

func (u *UI) connectAndPoll(ctx context.Context) {
	u.reconnectIPC(ctx)

	ticker := time.NewTicker(pollInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			resp, err := u.client.SendContext(ctx, ipc.Request{Action: ipc.ActionGetStatus})
			if err != nil {
				u.handleIPCError(ctx)
				continue
			}
			u.updateTrayState(resp)
		}
	}
}

func (u *UI) reconnectIPC(ctx context.Context) {
	// Fixed 5 s cadence (Story 5.6 AC3). The first attempt runs immediately
	// so the nominal startup path (service already up) does not incur any
	// delay before the first /api/status succeeds.
	for {
		if err := u.client.Connect(); err == nil {
			return
		}
		if u.onRetryWait != nil {
			u.onRetryWait()
		}
		select {
		case <-ctx.Done():
			return
		case <-time.After(ipcRetryInterval):
		}
	}
}

func (u *UI) handleIPCError(ctx context.Context) {
	if u.shutdownInProgress.Load() {
		return
	}

	u.mu.Lock()
	u.last = ""
	u.connected = false
	u.mu.Unlock()

	u.api.SetIcon(IconDisconnected)
	u.api.SetTooltip("Service indisponible")

	// Close and reconnect.
	u.client.Close()
	u.reconnectIPC(ctx)
}

func (u *UI) updateTrayState(resp ipc.Response) {
	// Story 8.1 AC8 — drive the "Mise à jour disponible" entry from a
	// dedicated path so its visibility tracks UpdateStatus/Version directly
	// and never gets swallowed by the icon-debounce below.
	u.applyUpdateMenu(resp)

	// Build state key to detect changes — include killswitch mode so the
	// degraded-mode override doesn't get debounced away when the tunnel
	// state is unchanged (Story 5.9 AC3). Story 6.3: also include
	// anomaly_active so the orange icon flips back to normal the instant
	// recovery completes, even when no other field changed.
	stateKey := fmt.Sprintf("%s|%s|%s|%s|%v|%s|%v", resp.Status, resp.IP, resp.Country, resp.RelayID, resp.HTTPProxyActive, resp.KillSwitchMode, resp.AnomalyActive)
	u.mu.Lock()
	if u.last == stateKey {
		u.mu.Unlock()
		return
	}
	u.last = stateKey
	u.mu.Unlock()

	// Story 6.3 — anomaly recovery wins over every other state. The user
	// needs to see the orange glyph regardless of whether the tunnel is
	// still technically "connected" when the leak check or TUN watchdog
	// fires. The banner text below matches the webview copy so tray and
	// webview stay in lockstep. Auto-clears once resp.AnomalyActive flips
	// back to false at the end of RecoverFromAnomaly.
	if resp.AnomalyActive {
		u.api.SetIcon(IconAlert)
		u.api.SetTooltip("Anomalie détectée — reconnexion en cours")
		u.mu.Lock()
		u.connected = false
		u.mu.Unlock()
		return
	}

	// Story 5.9 — degraded mode wins over the tunnel-state-driven icon. The
	// tray must render red + the dedicated tooltip even when the tunnel is
	// "connected", to match the permanent banner shown in the webview.
	if resp.KillSwitchMode == ipc.KillSwitchModeDegraded {
		u.api.SetIcon(IconDisconnected)
		u.api.SetTooltip("Mode dégradé — protection désactivée")
		u.mu.Lock()
		u.connected = false
		u.mu.Unlock()
		return
	}

	switch resp.Status {
	case ipc.StatusConnected:
		u.api.SetIcon(IconConnected)
		tooltip := "Protégé"
		if resp.Country != "" {
			tooltip = fmt.Sprintf("Protégé — %s", resp.Country)
		}
		if resp.IP != "" {
			tooltip = fmt.Sprintf("%s — IP : %s", tooltip, resp.IP)
		} else {
			tooltip = fmt.Sprintf("%s — IP en détection...", tooltip)
		}
		u.api.SetTooltip(tooltip)
		u.mu.Lock()
		u.connected = true
		u.mu.Unlock()

		// Sync WinINET proxy with current HTTP proxy state.
		if resp.HTTPProxyActive && resp.HTTPProxyAddr != "" {
			u.syncSysProxy(true, resp.HTTPProxyAddr)
		} else if u.sysProxyActive.Load() {
			u.syncSysProxy(false, "")
		}

	case ipc.StatusConnecting:
		u.api.SetIcon(IconConnecting)
		u.api.SetTooltip("Connexion en cours...")
		u.mu.Lock()
		u.connected = false
		u.mu.Unlock()

	default: // disconnected, error
		u.api.SetIcon(IconDisconnected)
		u.api.SetTooltip("Non protégé")
		u.mu.Lock()
		wasConnected := u.connected
		u.connected = false
		u.mu.Unlock()

		if wasConnected {
			u.syncSysProxy(false, "")
		}
	}
}

// applyUpdateMenu mirrors the staged-update state surfaced by /api/get_status
// onto the tray's "Mise à jour disponible" entry. Idempotent on identical
// versions (gates on lastShownUpdateVersion) so repeated 2 s polls don't churn
// the menu. No-op when no menu controller is wired (tests / packaging
// scenarios). Story 8.1 AC8.
//
// Code review M4: single lock cycle — claims `lastShownUpdateVersion` upfront
// and updates it before releasing mu. Eliminates the previous lock/unlock/lock
// pattern that allowed two concurrent callers to both see prev != newVersion
// and both call SetTitle. In production updateTrayState runs from a single
// goroutine so this matters only as defensive design, but it's free.
func (u *UI) applyUpdateMenu(resp ipc.Response) {
	if u.menuUpdate == nil {
		return
	}
	hasUpdate := resp.UpdateStatus == ipc.StatusUpdateReady && resp.UpdateVersion != ""

	u.mu.Lock()
	prev := u.lastShownUpdateVersion
	switch {
	case hasUpdate && resp.UpdateVersion != prev:
		u.lastShownUpdateVersion = resp.UpdateVersion
	case !hasUpdate && prev != "":
		u.lastShownUpdateVersion = ""
	default:
		u.mu.Unlock()
		return
	}
	u.mu.Unlock()

	// Outside the lock: actual systray calls. Doing them under mu would risk
	// blocking the polling loop on a slow native menu operation.
	if hasUpdate {
		u.menuUpdate.SetTitle(fmt.Sprintf("Mise à jour disponible : v%s", resp.UpdateVersion))
		u.menuUpdate.SetTooltip("Téléchargée et vérifiée — sera appliquée au prochain redémarrage du service")
		u.menuUpdate.Show()
		return
	}
	u.menuUpdate.Hide()
}

func (u *UI) syncSysProxy(active bool, addr string) {
	if u.sysProxy == nil {
		return
	}
	if active && addr != "" {
		u.sysProxy.Save()
		u.sysProxy.Set(addr)
		u.sysProxyActive.Store(true)
	} else {
		u.sysProxy.Restore()
		u.sysProxyActive.Store(false)
	}
}
