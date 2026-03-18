// Package desktop implements the Wails v2 desktop window and Go bindings.
package desktop

import (
	"context"
	"fmt"
	"time"

	"github.com/velia-the-veil/le_voile/internal/config"
	"github.com/velia-the-veil/le_voile/internal/ipc"
	"github.com/velia-the-veil/le_voile/internal/registry"
	wailsRuntime "github.com/wailsapp/wails/v2/pkg/runtime"
)

// IPCClient abstracts the IPC client for testability.
type IPCClient interface {
	Connect() error
	Close() error
	SendContext(ctx context.Context, req ipc.Request) (ipc.Response, error)
}

// StatusResponse is the JSON payload returned to the Wails frontend.
type StatusResponse struct {
	Status   string `json:"status"`    // "connected", "connecting", "disconnected"
	IP       string `json:"ip"`        // visible IP or ""
	Country  string `json:"country"`   // relay country name or ""
	Flag     string `json:"flag"`      // relay country flag emoji or ""
	RelayID  string `json:"relay_id"`
	Latency  string `json:"latency"`   // relay latency or ""
	Uptime   string `json:"uptime"`    // formatted duration or ""
	Message  string `json:"message"`   // French non-technical message
}

// CountryInfo holds country info for the frontend registry response.
type CountryInfo struct {
	Code       string `json:"code"`
	Name       string `json:"name"`
	Flag       string `json:"flag"`
	RelayCount int    `json:"relay_count"`
	Active     bool   `json:"active"`
}

// RegistryResponse is the JSON payload for GetRegistry.
type RegistryResponse struct {
	Countries []CountryInfo `json:"countries"`
}

// App is the Wails application struct exposed to the frontend via bindings.
type App struct {
	ctx           context.Context
	ipcClient     IPCClient
	relayDomain   string
	skipQuitModal bool                          // cached from config at startup
	runtimeQuit   func(ctx context.Context)     // injected for testability; defaults to wailsRuntime.Quit
	runtimeHide   func(ctx context.Context)     // injected for testability; defaults to wailsRuntime.WindowHide
	configPath    string                        // path to config file for saving preferences
}

// NewApp creates a new App with the given IPC client, relay domain, and config path.
func NewApp(client IPCClient, relayDomain string, configPath string, skipQuitModal bool) *App {
	return &App{
		ipcClient:     client,
		relayDomain:   relayDomain,
		runtimeQuit:   wailsRuntime.Quit,
		runtimeHide:   wailsRuntime.WindowHide,
		configPath:    configPath,
		skipQuitModal: skipQuitModal,
	}
}

// GetSkipQuitModal returns the cached "Ne plus montrer" preference.
func (a *App) GetSkipQuitModal() bool {
	return a.skipQuitModal
}

// SetSkipQuitModal updates the preference in memory and persists it to config.
// If persistence fails, the in-memory value is reverted to avoid a mismatch
// between what the user sees this session and what loads on next startup.
func (a *App) SetSkipQuitModal(skip bool) {
	prev := a.skipQuitModal
	a.skipQuitModal = skip
	if a.configPath == "" {
		return
	}
	cfg, err := config.Load(a.configPath)
	if err != nil {
		a.skipQuitModal = prev
		return
	}
	cfg.Client.SkipQuitModal = skip
	if err := cfg.Save(a.configPath); err != nil {
		a.skipQuitModal = prev
	}
}

// Startup is the Wails OnStartup callback. It connects the IPC client.
// Connect may fail if the service is not yet running; GetStatus will
// retry on each poll cycle, so a failed initial connect is non-fatal.
func (a *App) Startup(ctx context.Context) {
	a.ctx = ctx
	if err := a.ipcClient.Connect(); err != nil {
		// Non-fatal: the 2s polling loop in GetStatus will reconnect.
		// Retry once after a short delay in case the service is still starting.
		go func() {
			select {
			case <-ctx.Done():
				return
			case <-time.After(500 * time.Millisecond):
				a.ipcClient.Connect()
			}
		}()
	}
}

// OnBeforeClose intercepts the window close (X button) and hides the window
// instead of destroying the process (AC2). The tray and service continue
// running. The window can be re-opened via left-click on the tray icon.
// Returns true to prevent the default close behavior.
func (a *App) OnBeforeClose(ctx context.Context) bool {
	a.runtimeHide(a.ctx)
	return true
}

// Shutdown is the Wails OnShutdown callback. It closes the IPC client.
func (a *App) Shutdown(ctx context.Context) {
	a.ipcClient.Close()
}

// GetStatus queries the service via IPC and returns the current status
// formatted for the frontend with French messages.
func (a *App) GetStatus() StatusResponse {
	ctx, cancel := context.WithTimeout(a.ctx, 5*time.Second)
	defer cancel()

	resp, err := a.ipcClient.SendContext(ctx, ipc.Request{Action: ipc.ActionGetStatus})
	if err != nil {
		// Reconnect for next poll — the JS polls every 2s so
		// the next call will use the fresh connection.
		a.reconnectIPC()
		return StatusResponse{
			Status:  ipc.StatusDisconnected,
			Message: "Déconnecté",
		}
	}

	return a.mapResponse(resp)
}

// GetRegistry returns the list of available countries from the relay registry.
func (a *App) GetRegistry() RegistryResponse {
	ctx, cancel := context.WithTimeout(a.ctx, 5*time.Second)
	defer cancel()

	resp, err := a.ipcClient.SendContext(ctx, ipc.Request{Action: ipc.ActionGetRegistry})
	if err != nil {
		a.reconnectIPC()
		return RegistryResponse{}
	}

	var countries []CountryInfo
	for _, rc := range resp.RegistryCountries {
		countries = append(countries, CountryInfo{
			Code:       rc.Code,
			Name:       rc.Name,
			Flag:       rc.Flag,
			RelayCount: rc.RelayCount,
			Active:     rc.Active,
		})
	}
	return RegistryResponse{Countries: countries}
}

// SelectCountry sends a country selection command to the service via IPC.
func (a *App) SelectCountry(countryCode string) StatusResponse {
	ctx, cancel := context.WithTimeout(a.ctx, 10*time.Second)
	defer cancel()

	resp, err := a.ipcClient.SendContext(ctx, ipc.Request{
		Action: ipc.ActionSelectCountry,
		Value:  countryCode,
	})
	if err != nil {
		a.reconnectIPC()
		return StatusResponse{
			Status:  ipc.StatusError,
			Message: "Erreur de connexion",
		}
	}

	return StatusResponse{
		Status:  resp.Status,
		Message: statusMessage(resp.Status, resp.Error),
	}
}

// reconnectIPC closes and re-establishes the IPC connection.
// Called after IPC errors so the next call uses a fresh pipe.
func (a *App) reconnectIPC() {
	a.ipcClient.Close()
	a.ipcClient.Connect()
}

// Connect activates the tunnel via the service IPC.
func (a *App) Connect() StatusResponse {
	ctx, cancel := context.WithTimeout(a.ctx, 10*time.Second)
	defer cancel()

	resp, err := a.ipcClient.SendContext(ctx, ipc.Request{Action: ipc.ActionConnect})
	if err != nil {
		a.reconnectIPC()
		return StatusResponse{Status: ipc.StatusDisconnected, Message: "Déconnecté"}
	}
	return a.mapResponse(resp)
}

// Disconnect deactivates the tunnel via the service IPC.
func (a *App) Disconnect() StatusResponse {
	ctx, cancel := context.WithTimeout(a.ctx, 5*time.Second)
	defer cancel()

	resp, err := a.ipcClient.SendContext(ctx, ipc.Request{Action: ipc.ActionDisconnect})
	if err != nil {
		a.reconnectIPC()
		return StatusResponse{Status: ipc.StatusDisconnected, Message: "Déconnecté"}
	}
	return a.mapResponse(resp)
}

// Quit sends ActionQuit to the service and closes the Wails window.
// The service shuts down after a short delay; the tray detects the loss
// via its 2s polling and exits on its own.
func (a *App) Quit() {
	ctx, cancel := context.WithTimeout(a.ctx, 5*time.Second)
	defer cancel()
	a.ipcClient.SendContext(ctx, ipc.Request{Action: ipc.ActionQuit})
	a.runtimeQuit(a.ctx)
}

// mapResponse converts an IPC Response to a frontend StatusResponse.
func (a *App) mapResponse(resp ipc.Response) StatusResponse {
	sr := StatusResponse{
		Status:  resp.Status,
		IP:      resp.IP,
		Uptime:  resp.Uptime,
		Latency: resp.RelayLatency,
	}

	// Use relay metadata from the IPC response (dynamic, tracks country changes).
	// Fall back to local domain-based extraction if the service doesn't provide it.
	domain := resp.RelayDomain
	if domain == "" {
		domain = a.relayDomain
	}

	// Only populate relay info when connected or connecting.
	if resp.Status == ipc.StatusConnected || resp.Status == ipc.StatusConnecting {
		if resp.RelayID != "" {
			sr.RelayID = resp.RelayID
		} else {
			sr.RelayID = domain
		}
	}

	// Prefer country/flag from IPC response (uses relay ID for accurate extraction).
	if resp.Country != "" {
		sr.Country = resp.Country
		sr.Flag = resp.CountryFlag
	} else {
		sr.Country, sr.Flag = countryFromDomain(domain)
	}

	sr.Message = statusMessage(resp.Status, resp.Error)
	if resp.Status == ipc.StatusConnected && sr.Country != "" {
		sr.Message = fmt.Sprintf("Connecté — %s", sr.Country)
	}

	return sr
}

// statusMessage returns a French status message.
func statusMessage(status, errMsg string) string {
	switch status {
	case ipc.StatusConnected:
		return "Connecté"
	case ipc.StatusConnecting:
		return "Reconnexion en cours..."
	case ipc.StatusError:
		return fmt.Sprintf("Erreur — %s", errMsg)
	default:
		return "Déconnecté"
	}
}

// countryFromDomain extracts a country name and flag from the relay domain.
// Uses the centralized registry.CountryMetaMap; falls back to legacy prefixes.
func countryFromDomain(domain string) (string, string) {
	code := registry.ExtractCountryCode("", domain)
	if code != "" {
		if meta, ok := registry.CountryMetaMap[code]; ok {
			return meta.Name, meta.Flag
		}
	}
	if domain != "" {
		return domain, ""
	}
	return "", ""
}
