// Package tray implements the system tray UI and user interactions.
package tray

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"fyne.io/systray"
	"github.com/velia-the-veil/le_voile/internal/dns"
	"github.com/velia-the-veil/le_voile/internal/ipc"
)

const (
	defaultPollInterval = 2 * time.Second
	reconnectInitial    = 1 * time.Second
	reconnectMax        = 10 * time.Second
	quitTimeout         = 10 * time.Second
)

// SystrayAPI abstracts systray calls for testability.
type SystrayAPI interface {
	SetIcon(iconBytes []byte)
	SetTooltip(tooltip string)
	SetTitle(title string)
}

// SystrayMenuAPI abstracts systray menu operations for testability.
type SystrayMenuAPI interface {
	AddMenuItem(title, tooltip string) *systray.MenuItem
	AddMenuItemCheckbox(title, tooltip string, checked bool) *systray.MenuItem
	AddSeparator()
	Quit()
}

// defaultSystrayAPI delegates to the real systray package.
type defaultSystrayAPI struct{}

func (defaultSystrayAPI) SetIcon(iconBytes []byte) { systray.SetIcon(iconBytes) }
func (defaultSystrayAPI) SetTooltip(tooltip string) { systray.SetTooltip(tooltip) }
func (defaultSystrayAPI) SetTitle(title string)     { systray.SetTitle(title) }

// defaultSystrayMenuAPI delegates to the real systray package for menus.
type defaultSystrayMenuAPI struct{}

func (defaultSystrayMenuAPI) AddMenuItem(title, tooltip string) *systray.MenuItem {
	return systray.AddMenuItem(title, tooltip)
}
func (defaultSystrayMenuAPI) AddMenuItemCheckbox(title, tooltip string, checked bool) *systray.MenuItem {
	return systray.AddMenuItemCheckbox(title, tooltip, checked)
}
func (defaultSystrayMenuAPI) AddSeparator() { systray.AddSeparator() }
func (defaultSystrayMenuAPI) Quit()         { systray.Quit() }

// IPCClient abstracts the IPC client for testability.
type IPCClient interface {
	Connect() error
	Close() error
	SendContext(ctx context.Context, req ipc.Request) (ipc.Response, error)
}

// Tray manages the system tray icon, menu, and IPC polling.
type Tray struct {
	api     SystrayAPI
	menuAPI SystrayMenuAPI
	client  IPCClient

	pollInterval time.Duration
	autoStart    bool // initial auto_start value from config
	portableMode bool // true when running as portable binary (hides auto-start)

	cancel             context.CancelFunc
	mu                 sync.Mutex
	last               string // last known state to avoid redundant updates
	connected          bool   // current connection state for menu logic
	restoreCancel      context.CancelFunc // cancels the previous restoreTooltipAfter goroutine
	dnsRecoveryCancel  context.CancelFunc // cancels pending DNS orphan recovery
	shutdownInProgress atomic.Bool        // set before ActionQuit; prevents handleIPCError from launching orphan recovery
	shutdownDone       bool               // prevents double shutdown (guarded by mu)
	notifiedVer        string             // last version passed to NotifyUpdateReady (dedup guard)
	notifiedRollbackVer string            // last version passed to NotifyRollback (dedup guard)
	leakAlertActive          bool // true while a leak alert tooltip is being shown (AC3)
	notifiedBrowserPolicies  bool // dedup guard for browser policy tooltip
	blocklistEnabled         bool // current blocklist state (mirrors service config)
	httpProxyEnabled   bool // current HTTP proxy state
	httpProxySeq       uint64 // last seen sequence number
	sysProxy           *SysProxy
	relayDomain        string

	desktopCmd      *exec.Cmd // tracks the desktop subprocess
	desktopRunning  bool      // true while desktop subprocess is alive (guarded by mu)

	menuOpen        *systray.MenuItem
	menuToggle      *systray.MenuItem
	menuLeakCheck   *systray.MenuItem
	menuUpdateReady *systray.MenuItem
	menuAutoStart   *systray.MenuItem
	menuBlocklist   *systray.MenuItem
	menuHTTPProxy   *systray.MenuItem
	menuQuit        *systray.MenuItem
}

// New creates a Tray with the real systray API and IPC client.
func New() *Tray {
	return &Tray{
		api:          defaultSystrayAPI{},
		menuAPI:      defaultSystrayMenuAPI{},
		client:       ipc.NewClient(),
		pollInterval: defaultPollInterval,
		autoStart:    true,
	}
}

// NewWithConfig creates a Tray with the auto_start, blocklist, and HTTP proxy preferences loaded from config.
// If portableMode is true, the auto-start menu option is hidden.
// relayDomain is used by SysProxy for ProxyOverride (bypass the relay itself).
func NewWithConfig(autoStart bool, portableMode bool, blocklistEnabled bool, httpProxyEnabled bool, relayDomain string) *Tray {
	return &Tray{
		api:              defaultSystrayAPI{},
		menuAPI:          defaultSystrayMenuAPI{},
		client:           ipc.NewClient(),
		pollInterval:     defaultPollInterval,
		autoStart:        autoStart,
		portableMode:     portableMode,
		blocklistEnabled: blocklistEnabled,
		httpProxyEnabled: httpProxyEnabled,
		relayDomain:      relayDomain,
		sysProxy:         NewSysProxy(relayDomain),
	}
}

// newWithDeps creates a Tray with injected dependencies (for testing).
func newWithDeps(api SystrayAPI, menuAPI SystrayMenuAPI, client IPCClient, poll time.Duration, autoStart bool, blocklistEnabled bool, httpProxyEnabled bool, sysProxy *SysProxy) *Tray {
	return &Tray{
		api:              api,
		menuAPI:          menuAPI,
		client:           client,
		pollInterval:     poll,
		autoStart:        autoStart,
		blocklistEnabled: blocklistEnabled,
		httpProxyEnabled: httpProxyEnabled,
		sysProxy:         sysProxy,
	}
}

// Run starts the systray event loop. This blocks and must be called from the
// main goroutine. onExit is called when the systray quits.
func (t *Tray) Run() {
	systray.Run(t.onReady, t.onExit)
}

// Stop requests the systray to quit.
func (t *Tray) Stop() {
	systray.Quit()
}

func (t *Tray) onReady() {
	t.api.SetIcon(IconDefault)
	t.api.SetTooltip("Non protégé")
	t.api.SetTitle("Le Voile")

	// Create menu items
	t.menuOpen = t.menuAPI.AddMenuItem("Ouvrir la fenêtre", "")
	t.menuToggle = t.menuAPI.AddMenuItem("Activer Le Voile", "Activer/Désactiver la protection")
	t.menuLeakCheck = t.menuAPI.AddMenuItem("Vérifier fuite WebRTC", "Tester si votre IP réelle est exposée via WebRTC")
	t.menuLeakCheck.Hide() // visible only when connected
	t.menuUpdateReady = t.menuAPI.AddMenuItem("Mise à jour prête", "Une mise à jour sera appliquée au prochain démarrage")
	t.menuUpdateReady.Hide() // visible only when an update is staged
	t.menuAPI.AddSeparator()

	if !t.portableMode {
		autoStartTitle := "Démarrage auto : oui"
		if !t.autoStart {
			autoStartTitle = "Démarrage auto : non"
		}
		t.menuAutoStart = t.menuAPI.AddMenuItemCheckbox(autoStartTitle, "Démarrage automatique au boot", t.autoStart)
		t.menuAPI.AddSeparator()
	}

	blocklistTitle := "Blocklist DNS : désactivée"
	if t.blocklistEnabled {
		blocklistTitle = "Blocklist DNS : activée"
	}
	t.menuBlocklist = t.menuAPI.AddMenuItemCheckbox(blocklistTitle, "Filtrer publicités et trackers via StevenBlack/hosts", t.blocklistEnabled)

	httpProxyTitle := "Proxy HTTP : désactivé"
	if t.httpProxyEnabled {
		httpProxyTitle = "Proxy HTTP : activé"
	}
	t.menuHTTPProxy = t.menuAPI.AddMenuItemCheckbox(httpProxyTitle, "Tunneliser le trafic web via le relay (camouflage IP)", t.httpProxyEnabled)
	t.menuAPI.AddSeparator()

	t.menuQuit = t.menuAPI.AddMenuItem("Quitter", "Quitter Le Voile")

	// Recover WinINET proxy if a previous tray instance crashed.
	if t.sysProxy != nil {
		t.sysProxy.RecoverOrphan()
	}

	ctx, cancel := context.WithCancel(context.Background())
	t.cancel = cancel

	go t.connectAndPoll(ctx)
	go t.menuHandler(ctx)
}

func (t *Tray) onExit() {
	// Cleanup may already have run from handleQuit. Run again as safety net
	// in case the tray is closed externally (e.g., task manager, OS shutdown).
	t.shutdownServiceAndRestore()

	if t.cancel != nil {
		t.cancel()
	}
	t.client.Close()
}

// shutdownServiceAndRestore stops the service, restores DNS and proxy.
// Idempotent — safe to call multiple times.
//
// Sequence (AC1, AC7, AC8):
//  1. shutdownInProgress = true (prevents handleIPCError from launching orphan recovery)
//  2. Restore WinINET proxy (tray owns this resource)
//  3. Kill desktop process (timeout 3s — non-blocking for service shutdown)
//  4. ActionQuit via IPC (timeout 10s — tells the service to shut down)
//  5. Safety-net DNS recovery
func (t *Tray) shutdownServiceAndRestore() {
	t.mu.Lock()
	if t.shutdownDone {
		t.mu.Unlock()
		return
	}
	t.shutdownDone = true
	wasProxyActive := t.httpProxyEnabled
	t.mu.Unlock()

	// 1. Signal shutdown intent — handleIPCError will skip orphan recovery.
	t.shutdownInProgress.Store(true)

	// Cancel any pending DNS recovery timer.
	t.cancelDNSRecovery()

	// 2. Restore WinINET proxy (tray is the sole owner — AC5).
	if wasProxyActive {
		t.syncSysProxy(false, "")
	}

	// 3. Kill desktop process if launched by this tray (AC7).
	t.killDesktopProcess()

	// 4. Tell the service to stop. The IPC handler calls RequestStop()
	// which goes through SCM (p.svc.Stop()) for a clean exit.
	// If the service doesn't respond within quitTimeout, the tray
	// still exits — the service will be recovered on next start (1.4).
	quitCtx, cancel := context.WithTimeout(context.Background(), quitTimeout)
	defer cancel()
	t.client.SendContext(quitCtx, ipc.Request{Action: ipc.ActionQuit})

	// 5. Safety net: restore DNS from persisted file in case the service
	// didn't shut down cleanly. No-op if the service already restored.
	dns.RecoverOrphanDNS(context.Background())
}

// desktopKillTimeout is how long the tray waits for the desktop to exit
// before force-killing it.
const desktopKillTimeout = 3 * time.Second

// killDesktopProcess terminates the desktop process.
// First tries the subprocess launched by this tray. If not available
// (desktop launched by installer or externally), falls back to taskkill
// to ensure the desktop is always closed on tray quit.
func (t *Tray) killDesktopProcess() {
	t.mu.Lock()
	cmd := t.desktopCmd
	running := t.desktopRunning
	t.mu.Unlock()

	if running && cmd != nil && cmd.Process != nil {
		// Give the desktop a short grace period to exit on its own.
		done := make(chan struct{})
		go func() {
			cmd.Wait()
			close(done)
		}()

		select {
		case <-done:
			return // Desktop exited gracefully.
		case <-time.After(desktopKillTimeout):
			cmd.Process.Kill()
			return
		}
	}

	// Fallback: kill any desktop process by name (covers installer-launched instances).
	exec.Command("taskkill", "/F", "/IM", filepath.Base(desktopExePath())).Run()
}

func (t *Tray) menuHandler(ctx context.Context) {
	// A nil channel in a select case is never selected, which lets us
	// conditionally enable the auto-start case without duplicating the loop.
	var autoStartCh <-chan struct{}
	if t.menuAutoStart != nil {
		autoStartCh = t.menuAutoStart.ClickedCh
	}
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.menuOpen.ClickedCh:
			t.handleOpenDesktop()
		case <-t.menuToggle.ClickedCh:
			t.handleToggle(ctx)
		case <-t.menuLeakCheck.ClickedCh:
			t.handleLeakCheck(ctx)
		case <-autoStartCh:
			t.handleAutoStartToggle(ctx)
		case <-t.menuBlocklist.ClickedCh:
			t.handleBlocklistToggle(ctx)
		case <-t.menuHTTPProxy.ClickedCh:
			t.handleHTTPProxyToggle(ctx)
		case <-t.menuQuit.ClickedCh:
			t.handleQuit(ctx)
		}
	}
}

func (t *Tray) handleToggle(ctx context.Context) {
	t.mu.Lock()
	isConnected := t.connected
	t.mu.Unlock()

	var action string
	if isConnected {
		action = ipc.ActionDisconnect
	} else {
		action = ipc.ActionConnect
	}

	resp, err := t.client.SendContext(ctx, ipc.Request{Action: action})
	if err != nil {
		t.api.SetTooltip(fmt.Sprintf("Erreur : %s", err))
		return
	}

	// Force a full state refresh on next poll instead of calling updateTrayState
	// directly. The connect/disconnect response lacks fields like BlocklistEnabled,
	// which would cause updateTrayState to incorrectly flip the blocklist menu.
	t.mu.Lock()
	t.last = ""
	t.mu.Unlock()

	// Apply immediate visual feedback for connection status only.
	switch resp.Status {
	case ipc.StatusConnected:
		t.api.SetIcon(IconConnected)
		t.api.SetTooltip(fmt.Sprintf("Protégé — IP visible : %s", resp.IP))
		if t.menuToggle != nil {
			t.menuToggle.SetTitle("Désactiver Le Voile")
		}
	case ipc.StatusDisconnected:
		t.api.SetIcon(IconDisconnected)
		t.api.SetTooltip("Non protégé")
		if t.menuToggle != nil {
			t.menuToggle.SetTitle("Activer Le Voile")
		}
	case ipc.StatusError:
		t.api.SetTooltip(fmt.Sprintf("Erreur : %s", resp.Error))
	}
}

func (t *Tray) handleAutoStartToggle(ctx context.Context) {
	t.mu.Lock()
	wasAutoStart := t.autoStart
	t.mu.Unlock()

	newValue := "true"
	if wasAutoStart {
		newValue = "false"
	}

	resp, err := t.client.SendContext(ctx, ipc.Request{
		Action: ipc.ActionSetAutoStart,
		Value:  newValue,
	})
	if err != nil {
		t.api.SetTooltip(fmt.Sprintf("Erreur auto-start : %s", err))
		return
	}

	if resp.Status == ipc.StatusOK {
		t.mu.Lock()
		t.autoStart = !wasAutoStart
		t.mu.Unlock()

		if t.menuAutoStart != nil {
			if wasAutoStart {
				t.menuAutoStart.Uncheck()
				t.menuAutoStart.SetTitle("Démarrage auto : non")
			} else {
				t.menuAutoStart.Check()
				t.menuAutoStart.SetTitle("Démarrage auto : oui")
			}
		}
	} else if resp.Error != "" {
		t.api.SetTooltip(fmt.Sprintf("Erreur auto-start : %s", resp.Error))
	}
}

func (t *Tray) handleBlocklistToggle(ctx context.Context) {
	t.mu.Lock()
	wasEnabled := t.blocklistEnabled
	t.mu.Unlock()

	newValue := "true"
	if wasEnabled {
		newValue = "false"
	}

	resp, err := t.client.SendContext(ctx, ipc.Request{
		Action: ipc.ActionSetBlocklist,
		Value:  newValue,
	})
	if err != nil {
		t.api.SetTooltip(fmt.Sprintf("Erreur blocklist : %s", err))
		return
	}

	if resp.Status == ipc.StatusOK {
		t.mu.Lock()
		t.blocklistEnabled = !wasEnabled
		t.mu.Unlock()

		if t.menuBlocklist != nil {
			if wasEnabled {
				t.menuBlocklist.Uncheck()
				t.menuBlocklist.SetTitle("Blocklist DNS : désactivée")
			} else {
				t.menuBlocklist.Check()
				t.menuBlocklist.SetTitle("Blocklist DNS : activée")
			}
		}
	} else if resp.Error != "" {
		t.api.SetTooltip(fmt.Sprintf("Erreur blocklist : %s", resp.Error))
	}
}

func (t *Tray) handleHTTPProxyToggle(ctx context.Context) {
	t.mu.Lock()
	wasEnabled := t.httpProxyEnabled
	t.mu.Unlock()

	newValue := "true"
	if wasEnabled {
		newValue = "false"
	}

	resp, err := t.client.SendContext(ctx, ipc.Request{
		Action: ipc.ActionSetHTTPProxy,
		Value:  newValue,
	})
	if err != nil {
		t.api.SetTooltip(fmt.Sprintf("Erreur proxy HTTP : %s", err))
		return
	}

	if resp.Status == ipc.StatusOK {
		t.mu.Lock()
		t.httpProxyEnabled = !wasEnabled
		t.httpProxySeq = resp.HTTPProxySeq
		t.mu.Unlock()

		if t.menuHTTPProxy != nil {
			if wasEnabled {
				t.menuHTTPProxy.Uncheck()
				t.menuHTTPProxy.SetTitle("Proxy HTTP : désactivé")
			} else {
				t.menuHTTPProxy.Check()
				t.menuHTTPProxy.SetTitle("Proxy HTTP : activé")
			}
		}

		// Update WinINET proxy based on new state.
		t.syncSysProxy(!wasEnabled, resp.HTTPProxyAddr)
	} else if resp.Error != "" {
		t.api.SetTooltip(fmt.Sprintf("Erreur proxy HTTP : %s", resp.Error))
	}
}

// syncSysProxy configures or restores the WinINET system proxy.
func (t *Tray) syncSysProxy(active bool, addr string) {
	if t.sysProxy == nil {
		return
	}
	if active && addr != "" {
		t.sysProxy.Save()
		t.sysProxy.Set(addr)
	} else {
		t.sysProxy.Restore()
	}
}

func (t *Tray) handleLeakCheck(ctx context.Context) {
	t.api.SetTooltip("Test de fuite WebRTC en cours...")

	leakCtx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()

	resp, err := t.client.SendContext(leakCtx, ipc.Request{Action: ipc.ActionLeakCheck})
	if err != nil {
		t.api.SetTooltip(fmt.Sprintf("Erreur leak check : %s", err))
		go t.restoreTooltipAfter(ctx, 10*time.Second)
		return
	}

	switch resp.Status {
	case ipc.StatusLeakPass:
		t.api.SetTooltip(fmt.Sprintf("Aucune fuite détectée — IP STUN : %s", resp.IP))
	case ipc.StatusLeakFail:
		t.api.SetTooltip(fmt.Sprintf("FUITE DÉTECTÉE — IP réelle visible : %s", resp.IP))
	default:
		t.api.SetTooltip(fmt.Sprintf("Leak check : %s — %s", resp.Status, resp.Error))
	}

	go t.restoreTooltipAfter(ctx, 10*time.Second)
}

// restoreTooltipAfter restores the tooltip to the current connection status
// after the given duration. Calling this cancels any previously pending restore.
func (t *Tray) restoreTooltipAfter(ctx context.Context, d time.Duration) {
	t.mu.Lock()
	if t.restoreCancel != nil {
		t.restoreCancel()
	}
	restoreCtx, cancel := context.WithCancel(ctx)
	t.restoreCancel = cancel
	t.mu.Unlock()

	select {
	case <-restoreCtx.Done():
		return
	case <-time.After(d):
	}

	t.mu.Lock()
	t.last = "" // force a refresh on next poll
	t.mu.Unlock()
}

// NotifyUpdateReady shows that an update is staged and will be installed on next restart.
func (t *Tray) NotifyUpdateReady(version string) {
	t.mu.Lock()
	t.notifiedVer = version
	t.mu.Unlock()

	if t.menuUpdateReady != nil {
		t.menuUpdateReady.SetTitle(fmt.Sprintf("Mise à jour v%s prête", version))
		t.menuUpdateReady.Show()
	}
	t.api.SetTooltip(fmt.Sprintf("Mise à jour v%s prête — appliquée au prochain démarrage", version))
	go t.restoreTooltipAfter(context.Background(), 10*time.Second)
}

// ClearUpdateNotification hides the update menu item and resets its title.
func (t *Tray) ClearUpdateNotification() {
	if t.menuUpdateReady != nil {
		t.menuUpdateReady.SetTitle("Mise à jour prête")
		t.menuUpdateReady.Hide()
	}
}

// NotifyRollback shows that an update was rolled back. Deduplicated per version.
func (t *Tray) NotifyRollback(version string) {
	t.mu.Lock()
	if t.notifiedRollbackVer == version {
		t.mu.Unlock()
		return
	}
	t.notifiedRollbackVer = version
	t.mu.Unlock()

	if t.menuUpdateReady != nil {
		t.menuUpdateReady.Hide()
	}
	t.api.SetTooltip(fmt.Sprintf("Mise à jour v%s annulée — version précédente restaurée", version))
	go t.restoreTooltipAfter(context.Background(), 15*time.Second)
}

// handleOpenDesktop launches the desktop subprocess if not already running.
func (t *Tray) handleOpenDesktop() {
	t.mu.Lock()
	defer t.mu.Unlock()

	// If the desktop is already running, do nothing.
	if t.desktopRunning {
		return
	}

	exePath := desktopExePath()
	t.desktopCmd = exec.Command(exePath)
	if err := t.desktopCmd.Start(); err != nil {
		t.api.SetTooltip(fmt.Sprintf("Erreur ouverture fenêtre : %s", err))
		return
	}
	t.desktopRunning = true
	// Wait in background and reset the flag when the process exits.
	go func() {
		t.desktopCmd.Wait()
		t.mu.Lock()
		t.desktopRunning = false
		t.mu.Unlock()
	}()
}

// desktopExePath returns the path to the desktop binary, expected to be in
// the same directory as the tray binary with the name "levoile-desktop.exe".
func desktopExePath() string {
	self, err := os.Executable()
	if err != nil {
		return "levoile-desktop.exe"
	}
	return filepath.Join(filepath.Dir(self), "levoile-desktop.exe")
}

func (t *Tray) handleQuit(ctx context.Context) {
	// Stop service and restore DNS/proxy BEFORE closing the tray.
	t.shutdownServiceAndRestore()
	t.menuAPI.Quit()
}

func (t *Tray) connectAndPoll(ctx context.Context) {
	t.reconnectIPC(ctx)

	ticker := time.NewTicker(t.pollInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			resp, err := t.client.SendContext(ctx, ipc.Request{Action: ipc.ActionGetStatus})
			if err != nil {
				t.handleIPCError(ctx, err)
				continue
			}
			t.updateTrayState(resp)
			// Detect rollback from polling response (highest priority)
			if resp.UpdateStatus == ipc.StatusRollback && resp.RollbackVersion != "" {
				t.NotifyRollback(resp.RollbackVersion)
			} else if resp.UpdateVersion != "" && resp.UpdateStatus == ipc.StatusUpdateReady {
				// Detect pending update from polling response (deduplicate)
				t.mu.Lock()
				alreadyNotified := t.notifiedVer == resp.UpdateVersion
				t.mu.Unlock()
				if !alreadyNotified {
					t.NotifyUpdateReady(resp.UpdateVersion)
				}
			}
			// Detect leak from periodic auto-test (AC3).
			t.mu.Lock()
			wasLeakActive := t.leakAlertActive
			if resp.LeakStatus == ipc.StatusLeakFail && !t.leakAlertActive {
				t.leakAlertActive = true
			} else if resp.LeakStatus == ipc.StatusLeakPass && t.leakAlertActive {
				t.leakAlertActive = false
			}
			t.mu.Unlock()
			if resp.LeakStatus == ipc.StatusLeakFail && !wasLeakActive {
				t.api.SetTooltip("⚠ Fuite détectée — vérification en cours")
				go t.restoreTooltipAfter(ctx, 30*time.Second)
			}
		}
	}
}

func (t *Tray) reconnectIPC(ctx context.Context) {
	delay := reconnectInitial
	for {
		if err := t.client.Connect(); err == nil {
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

// dnsRecoveryDelay is how long the tray waits for the service to come back
// before restoring orphaned DNS. Gives SCM time to auto-restart the service.
const dnsRecoveryDelay = 10 * time.Second

func (t *Tray) handleIPCError(ctx context.Context, err error) {
	// AC8: If shutdown is in progress (intentional quit), do NOT launch
	// orphan recovery or attempt reconnection — the service is shutting down.
	if t.shutdownInProgress.Load() {
		return
	}

	t.mu.Lock()
	t.last = ""
	t.connected = false
	wasProxyActive := t.httpProxyEnabled
	t.httpProxyEnabled = false
	t.httpProxySeq = 0 // AC3: force WinINET sync on next successful get_status after reconnect
	// Reset leak alert flag so future leaks detected after reconnect are notified.
	t.leakAlertActive = false
	t.mu.Unlock()

	// Restore WinINET immediately if the proxy was active — service may have crashed.
	if wasProxyActive {
		t.syncSysProxy(false, "")
	}

	// Schedule DNS orphan recovery — if the service doesn't come back within
	// dnsRecoveryDelay, restore DNS from the persisted state file so the user
	// doesn't lose internet. Cancelled if reconnectIPC succeeds first.
	t.startDNSRecoveryTimer(ctx)

	t.api.SetIcon(IconDisconnected)
	t.api.SetTooltip(fmt.Sprintf("Non protégé — %s", err))

	if t.menuToggle != nil {
		t.menuToggle.SetTitle("Activer Le Voile")
	}

	t.client.Close()
	t.reconnectIPC(ctx)

	// Service came back — cancel pending DNS recovery if it hasn't fired yet.
	t.cancelDNSRecovery()
}

// startDNSRecoveryTimer starts a background goroutine that will restore DNS
// after dnsRecoveryDelay if not cancelled. Only one timer is active at a time.
func (t *Tray) startDNSRecoveryTimer(ctx context.Context) {
	t.mu.Lock()
	if t.dnsRecoveryCancel != nil {
		t.dnsRecoveryCancel()
	}
	recoveryCtx, cancel := context.WithCancel(ctx)
	t.dnsRecoveryCancel = cancel
	t.mu.Unlock()

	go func() {
		select {
		case <-recoveryCtx.Done():
			return // service came back or tray exiting
		case <-time.After(dnsRecoveryDelay):
		}

		dns.RecoverOrphanDNS(context.Background())
	}()
}

// cancelDNSRecovery cancels any pending DNS recovery timer.
func (t *Tray) cancelDNSRecovery() {
	t.mu.Lock()
	if t.dnsRecoveryCancel != nil {
		t.dnsRecoveryCancel()
		t.dnsRecoveryCancel = nil
	}
	t.mu.Unlock()
}

func (t *Tray) updateTrayState(resp ipc.Response) {
	t.mu.Lock()
	stateKey := resp.Status + "|" + resp.IP + "|" + resp.Error + "|" + fmt.Sprintf("%v|%d|%d", resp.BlocklistEnabled, resp.HTTPProxySeq, len(resp.BrowserPoliciesApplied))
	if t.last == stateKey {
		t.mu.Unlock()
		return
	}
	t.last = stateKey

	// Capture blocklist state change; menu update applied after unlock,
	// consistent with other menu updates that occur outside the lock.
	blocklistChanged := resp.BlocklistEnabled != t.blocklistEnabled
	if blocklistChanged {
		t.blocklistEnabled = resp.BlocklistEnabled
	}

	// Capture HTTP proxy state change via sequence number comparison.
	httpProxyChanged := resp.HTTPProxySeq != t.httpProxySeq
	if httpProxyChanged {
		t.httpProxySeq = resp.HTTPProxySeq
		t.httpProxyEnabled = resp.HTTPProxyActive
	}

	switch resp.Status {
	case ipc.StatusConnected:
		t.connected = true
	case ipc.StatusConnecting:
		// keep current connected state during transition
	default:
		t.connected = false
	}
	t.mu.Unlock()

	// Synchronise blocklist menu label outside the lock.
	if blocklistChanged && t.menuBlocklist != nil {
		if resp.BlocklistEnabled {
			t.menuBlocklist.Check()
			t.menuBlocklist.SetTitle("Blocklist DNS : activée")
		} else {
			t.menuBlocklist.Uncheck()
			t.menuBlocklist.SetTitle("Blocklist DNS : désactivée")
		}
	}

	// Synchronise HTTP proxy menu label and WinINET outside the lock.
	if httpProxyChanged {
		if t.menuHTTPProxy != nil {
			if resp.HTTPProxyActive {
				t.menuHTTPProxy.Check()
				t.menuHTTPProxy.SetTitle("Proxy HTTP : activé")
			} else {
				t.menuHTTPProxy.Uncheck()
				t.menuHTTPProxy.SetTitle("Proxy HTTP : désactivé")
			}
		}
		t.syncSysProxy(resp.HTTPProxyActive, resp.HTTPProxyAddr)
	}

	// Notify browser policies via tooltip (one-shot, deduplicated).
	if len(resp.BrowserPoliciesApplied) > 0 {
		t.mu.Lock()
		alreadyNotified := t.notifiedBrowserPolicies
		t.notifiedBrowserPolicies = true
		t.mu.Unlock()

		if !alreadyNotified {
			browsers := strings.Join(resp.BrowserPoliciesApplied, ", ")
			t.api.SetTooltip(fmt.Sprintf("WebRTC: %s — restart browsers", browsers))
			go t.restoreTooltipAfter(context.Background(), 15*time.Second)
		}
	}

	// Captive portal overrides normal status display.
	if resp.CaptivePortal {
		t.api.SetIcon(IconCaptive)
		t.api.SetTooltip("Portail Wi-Fi détecté — authentifiez-vous")
		if t.menuToggle != nil {
			t.menuToggle.SetTitle("Activer Le Voile")
		}
		return
	}

	switch resp.Status {
	case ipc.StatusConnected:
		t.api.SetIcon(IconConnected)
		if resp.IP == "" {
			t.api.SetTooltip("Protégé — IP en détection...")
		} else {
			t.api.SetTooltip(fmt.Sprintf("Protégé — IP visible : %s", resp.IP))
		}
		if t.menuToggle != nil {
			t.menuToggle.SetTitle("Désactiver Le Voile")
		}
		if t.menuLeakCheck != nil {
			t.menuLeakCheck.Show()
		}
	case ipc.StatusConnecting:
		t.api.SetIcon(IconConnecting)
		t.api.SetTooltip("Connexion en cours...")
	case ipc.StatusDisconnected:
		t.api.SetIcon(IconDisconnected)
		tooltip := "Non protégé"
		if resp.Error != "" {
			tooltip += " — " + resp.Error
		}
		t.api.SetTooltip(tooltip)
		if t.menuToggle != nil {
			t.menuToggle.SetTitle("Activer Le Voile")
		}
		if t.menuLeakCheck != nil {
			t.menuLeakCheck.Hide()
		}
	case ipc.StatusError:
		t.api.SetIcon(IconDisconnected)
		t.api.SetTooltip(fmt.Sprintf("Erreur : %s", resp.Error))
		if t.menuToggle != nil {
			t.menuToggle.SetTitle("Activer Le Voile")
		}
		if t.menuLeakCheck != nil {
			t.menuLeakCheck.Hide()
		}
	default:
		t.api.SetIcon(IconDisconnected)
		t.api.SetTooltip(fmt.Sprintf("État inconnu : %s", resp.Status))
		if t.menuToggle != nil {
			t.menuToggle.SetTitle("Activer Le Voile")
		}
		if t.menuLeakCheck != nil {
			t.menuLeakCheck.Hide()
		}
	}
}

