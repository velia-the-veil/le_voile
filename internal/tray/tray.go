// Package tray implements the system tray UI and user interactions.
package tray

import (
	"context"
	"fmt"
	"sync"
	"time"

	"fyne.io/systray"
	"github.com/velia-the-veil/le_voile/internal/ipc"
)

const (
	defaultPollInterval = 2 * time.Second
	reconnectInitial    = 1 * time.Second
	reconnectMax        = 10 * time.Second
	quitTimeout         = 3 * time.Second
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

	cancel        context.CancelFunc
	mu            sync.Mutex
	last          string // last known state to avoid redundant updates
	connected     bool   // current connection state for menu logic
	restoreCancel      context.CancelFunc // cancels the previous restoreTooltipAfter goroutine
	notifiedVer        string             // last version passed to NotifyUpdateReady (dedup guard)
	notifiedRollbackVer string            // last version passed to NotifyRollback (dedup guard)
	leakAlertActive    bool // true while a leak alert tooltip is being shown (AC3)
	blocklistEnabled   bool // current blocklist state (mirrors service config)

	menuToggle      *systray.MenuItem
	menuLeakCheck   *systray.MenuItem
	menuUpdateReady *systray.MenuItem
	menuAutoStart   *systray.MenuItem
	menuBlocklist   *systray.MenuItem
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

// NewWithConfig creates a Tray with the auto_start and blocklist preferences loaded from config.
// If portableMode is true, the auto-start menu option is hidden.
func NewWithConfig(autoStart bool, portableMode bool, blocklistEnabled bool) *Tray {
	return &Tray{
		api:              defaultSystrayAPI{},
		menuAPI:          defaultSystrayMenuAPI{},
		client:           ipc.NewClient(),
		pollInterval:     defaultPollInterval,
		autoStart:        autoStart,
		portableMode:     portableMode,
		blocklistEnabled: blocklistEnabled,
	}
}

// newWithDeps creates a Tray with injected dependencies (for testing).
func newWithDeps(api SystrayAPI, menuAPI SystrayMenuAPI, client IPCClient, poll time.Duration, autoStart bool, blocklistEnabled bool) *Tray {
	return &Tray{
		api:              api,
		menuAPI:          menuAPI,
		client:           client,
		pollInterval:     poll,
		autoStart:        autoStart,
		blocklistEnabled: blocklistEnabled,
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
	t.api.SetIcon(IconDisconnected)
	t.api.SetTooltip("Non protégé")
	t.api.SetTitle("Le Voile")

	// Create menu items
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
	t.menuAPI.AddSeparator()

	t.menuQuit = t.menuAPI.AddMenuItem("Quitter", "Quitter Le Voile")

	ctx, cancel := context.WithCancel(context.Background())
	t.cancel = cancel

	go t.connectAndPoll(ctx)
	go t.menuHandler(ctx)
}

func (t *Tray) onExit() {
	if t.cancel != nil {
		t.cancel()
	}
	t.client.Close()
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
		case <-t.menuToggle.ClickedCh:
			t.handleToggle(ctx)
		case <-t.menuLeakCheck.ClickedCh:
			t.handleLeakCheck(ctx)
		case <-autoStartCh:
			t.handleAutoStartToggle(ctx)
		case <-t.menuBlocklist.ClickedCh:
			t.handleBlocklistToggle(ctx)
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

	t.mu.Lock()
	t.last = "" // force state refresh
	t.mu.Unlock()

	t.updateTrayState(resp)
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

func (t *Tray) handleQuit(ctx context.Context) {
	quitCtx, cancel := context.WithTimeout(ctx, quitTimeout)
	defer cancel()

	t.client.SendContext(quitCtx, ipc.Request{Action: ipc.ActionQuit})
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
			if resp.LeakStatus == ipc.StatusLeakFail && !t.leakAlertActive {
				t.leakAlertActive = true
				t.api.SetTooltip("⚠ Fuite détectée — vérification en cours")
				go t.restoreTooltipAfter(ctx, 30*time.Second)
			} else if resp.LeakStatus == ipc.StatusLeakPass && t.leakAlertActive {
				// Recovery: clear flag; restoreTooltipAfter already handles tooltip restore.
				t.leakAlertActive = false
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

func (t *Tray) handleIPCError(ctx context.Context, err error) {
	t.mu.Lock()
	t.last = ""
	t.connected = false
	t.mu.Unlock()

	// Reset leak alert flag so future leaks detected after reconnect are notified.
	// Without this reset, a leak alert active at the time of disconnect would
	// permanently suppress all future alerts (leakAlertActive stays true).
	t.leakAlertActive = false

	t.api.SetIcon(IconDisconnected)
	t.api.SetTooltip(fmt.Sprintf("Non protégé — %s", err))

	if t.menuToggle != nil {
		t.menuToggle.SetTitle("Activer Le Voile")
	}

	t.client.Close()
	t.reconnectIPC(ctx)
}

func (t *Tray) updateTrayState(resp ipc.Response) {
	t.mu.Lock()
	stateKey := resp.Status + "|" + resp.IP + "|" + resp.Error + "|" + fmt.Sprintf("%v", resp.BlocklistEnabled)
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

	switch resp.Status {
	case ipc.StatusConnected:
		t.api.SetIcon(IconConnected)
		ip := resp.IP
		if ip == "" {
			ip = "unknown"
		}
		t.api.SetTooltip(fmt.Sprintf("Protégé — IP visible : %s", ip))
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
