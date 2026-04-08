package ui

import (
	"context"
	"fmt"
	"io/fs"
	"sync"
	"sync/atomic"
	"time"

	"fyne.io/systray"
	"github.com/velia-the-veil/le_voile/internal/dns"
	"github.com/velia-the-veil/le_voile/internal/ipc"
)

const (
	pollInterval     = 2 * time.Second
	reconnectInitial = 1 * time.Second
	reconnectMax     = 10 * time.Second
	quitTimeout      = 10 * time.Second
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

type defaultSystrayAPI struct{}

func (defaultSystrayAPI) SetIcon(iconBytes []byte) { systray.SetIcon(iconBytes) }
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

	mu               sync.Mutex
	last             string // last known state to avoid redundant updates
	connected        bool
	shutdownDone     bool
	shutdownInProgress atomic.Bool

	sysProxy    *SysProxy
	webviewOpen atomic.Bool // prevents multiple windows

	menuOpen   *systray.MenuItem
	menuToggle *systray.MenuItem
	menuQuit   *systray.MenuItem
}

// New creates a UI instance with real systray API.
func New(client IPCClient, cfg Config) *UI {
	return &UI{
		api:      defaultSystrayAPI{},
		menuAPI:  defaultSystrayMenuAPI{},
		client:   NewSafeIPCClient(client),
		config:   cfg,
		sysProxy: NewSysProxy(cfg.RelayDomain),
	}
}

// newWithDeps creates a UI with injected dependencies (for testing).
func newWithDeps(api SystrayAPI, menuAPI SystrayMenuAPI, client IPCClient, cfg Config, sysProxy *SysProxy) *UI {
	return &UI{
		api:      api,
		menuAPI:  menuAPI,
		client:   NewSafeIPCClient(client),
		config:   cfg,
		sysProxy: sysProxy,
	}
}

// Run starts the systray event loop. Blocks and must be called from the main goroutine.
func (u *UI) Run() {
	systray.Run(u.onReady, u.onExit)
}

func (u *UI) onReady() {
	u.api.SetIcon(IconDefault)
	u.api.SetTooltip("Non protégé")
	u.api.SetTitle("Le Voile")

	// Menu items — French UI.
	u.menuOpen = u.menuAPI.AddMenuItem("Ouvrir la fenêtre", "")
	u.menuToggle = u.menuAPI.AddMenuItem("Activer Le Voile", "Activer/Désactiver la protection")
	u.menuAPI.AddSeparator()
	u.menuQuit = u.menuAPI.AddMenuItem("Quitter", "Quitter Le Voile")

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
}

func (u *UI) onExit() {
	u.shutdownServiceAndRestore()
	if u.cancel != nil {
		u.cancel()
	}
	u.client.Close()
}

func (u *UI) menuHandler(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			return
		case <-u.menuOpen.ClickedCh:
			u.handleOpenWebview()
		case <-u.menuToggle.ClickedCh:
			u.handleToggle(ctx)
		case <-u.menuQuit.ClickedCh:
			u.handleQuit()
		}
	}
}

func (u *UI) handleOpenWebview() {
	if u.webviewOpen.Load() {
		return // already open
	}
	addr := u.httpServer.Addr()
	if addr == "" {
		return
	}
	u.webviewOpen.Store(true)
	go func() {
		defer u.webviewOpen.Store(false)
		openWebview(addr)
	}()
}

func (u *UI) handleToggle(ctx context.Context) {
	u.mu.Lock()
	isConnected := u.connected
	u.mu.Unlock()

	action := ipc.ActionConnect
	if isConnected {
		action = ipc.ActionDisconnect
	}

	resp, err := u.client.SendContext(ctx, ipc.Request{Action: action})
	if err != nil {
		u.api.SetTooltip(fmt.Sprintf("Erreur : %s", err))
		return
	}

	// Force a full state refresh on next poll.
	u.mu.Lock()
	u.last = ""
	u.mu.Unlock()

	switch resp.Status {
	case ipc.StatusConnected:
		u.api.SetIcon(IconConnected)
		u.api.SetTooltip(fmt.Sprintf("Protégé — IP visible : %s", resp.IP))
		if u.menuToggle != nil {
			u.menuToggle.SetTitle("Désactiver Le Voile")
		}
	case ipc.StatusDisconnected:
		u.api.SetIcon(IconDisconnected)
		u.api.SetTooltip("Non protégé")
		if u.menuToggle != nil {
			u.menuToggle.SetTitle("Activer Le Voile")
		}
	case ipc.StatusError:
		u.api.SetTooltip(fmt.Sprintf("Erreur : %s", resp.Error))
	}
}

func (u *UI) handleQuit() {
	u.shutdownServiceAndRestore()
	u.menuAPI.Quit()
}

// shutdownServiceAndRestore stops the service, restores DNS and proxy.
// Idempotent — safe to call multiple times.
func (u *UI) shutdownServiceAndRestore() {
	u.mu.Lock()
	if u.shutdownDone {
		u.mu.Unlock()
		return
	}
	u.shutdownDone = true
	u.mu.Unlock()

	u.shutdownInProgress.Store(true)

	// Restore WinINET proxy.
	if u.sysProxy != nil {
		u.sysProxy.Restore()
	}

	// Tell the service to stop.
	quitCtx, cancel := context.WithTimeout(context.Background(), quitTimeout)
	defer cancel()
	u.client.SendContext(quitCtx, ipc.Request{Action: ipc.ActionQuit})

	// Safety net: restore DNS from persisted file in case the service
	// didn't shut down cleanly. No-op if the service already restored.
	dns.RecoverOrphanDNS(context.Background())
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
	delay := reconnectInitial
	for {
		if err := u.client.Connect(); err == nil {
			return
		}
		select {
		case <-ctx.Done():
			return
		case <-time.After(delay):
		}
		delay *= 2
		if delay > reconnectMax {
			delay = reconnectMax
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
	if u.menuToggle != nil {
		u.menuToggle.SetTitle("Activer Le Voile")
	}

	// Close and reconnect.
	u.client.Close()
	u.reconnectIPC(ctx)
}

func (u *UI) updateTrayState(resp ipc.Response) {
	// Build state key to detect changes.
	stateKey := fmt.Sprintf("%s|%s|%s|%s|%v", resp.Status, resp.IP, resp.Country, resp.RelayID, resp.HTTPProxyActive)
	u.mu.Lock()
	if u.last == stateKey {
		u.mu.Unlock()
		return
	}
	u.last = stateKey
	u.mu.Unlock()

	switch resp.Status {
	case ipc.StatusConnected:
		u.api.SetIcon(IconConnected)
		tooltip := "Protégé"
		if resp.Country != "" {
			tooltip = fmt.Sprintf("Protégé — %s", resp.Country)
		}
		if resp.IP != "" {
			tooltip = fmt.Sprintf("%s — IP : %s", tooltip, resp.IP)
		}
		u.api.SetTooltip(tooltip)
		if u.menuToggle != nil {
			u.menuToggle.SetTitle("Désactiver Le Voile")
		}
		u.mu.Lock()
		u.connected = true
		u.mu.Unlock()

		// Sync WinINET proxy.
		if resp.HTTPProxyActive && resp.HTTPProxyAddr != "" {
			u.syncSysProxy(true, resp.HTTPProxyAddr)
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
		if u.menuToggle != nil {
			u.menuToggle.SetTitle("Activer Le Voile")
		}
		u.mu.Lock()
		wasConnected := u.connected
		u.connected = false
		u.mu.Unlock()

		if wasConnected {
			u.syncSysProxy(false, "")
		}
	}
}

func (u *UI) syncSysProxy(active bool, addr string) {
	if u.sysProxy == nil {
		return
	}
	if active && addr != "" {
		u.sysProxy.Save()
		u.sysProxy.Set(addr)
	} else {
		u.sysProxy.Restore()
	}
}
