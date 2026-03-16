// Package desktop implements the Wails v2 desktop window and Go bindings.
package desktop

import (
	"context"
	"fmt"
	"time"

	"github.com/velia-the-veil/le_voile/internal/ipc"
)

// IPCClient abstracts the IPC client for testability.
type IPCClient interface {
	Connect() error
	Close() error
	SendContext(ctx context.Context, req ipc.Request) (ipc.Response, error)
}

// StatusResponse is the JSON payload returned to the Wails frontend.
type StatusResponse struct {
	Status  string `json:"status"`  // "connected", "connecting", "disconnected"
	IP      string `json:"ip"`      // visible IP or ""
	Country string `json:"country"` // relay country name or ""
	RelayID string `json:"relay_id"`
	Uptime  string `json:"uptime"`  // formatted duration or ""
	Message string `json:"message"` // French non-technical message
}

// relayCountryMap maps relay domain prefixes to French country names.
var relayCountryMap = map[string]string{
	"relay-iceland":  "Islande",
	"relay-finland":  "Finlande",
	"relay-germany":  "Allemagne",
	"relay-france":   "France",
	"relay-usa":      "États-Unis",
}

// App is the Wails application struct exposed to the frontend via bindings.
type App struct {
	ctx         context.Context
	ipcClient   IPCClient
	relayDomain string
}

// NewApp creates a new App with the given IPC client and relay domain.
func NewApp(client IPCClient, relayDomain string) *App {
	return &App{
		ipcClient:   client,
		relayDomain: relayDomain,
	}
}

// Startup is the Wails OnStartup callback. It connects the IPC client.
func (a *App) Startup(ctx context.Context) {
	a.ctx = ctx
	a.ipcClient.Connect()
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
		a.ipcClient.Close()
		a.ipcClient.Connect()
		return StatusResponse{
			Status:  ipc.StatusDisconnected,
			Message: "Déconnecté",
		}
	}

	return a.mapResponse(resp)
}

// mapResponse converts an IPC Response to a frontend StatusResponse.
func (a *App) mapResponse(resp ipc.Response) StatusResponse {
	sr := StatusResponse{
		Status: resp.Status,
		IP:     resp.IP,
		Uptime: resp.Uptime,
	}

	// Only populate relay info when connected or connecting.
	if resp.Status == ipc.StatusConnected || resp.Status == ipc.StatusConnecting {
		sr.RelayID = a.relayDomain
	}

	country := countryFromDomain(a.relayDomain)
	sr.Country = country

	switch resp.Status {
	case ipc.StatusConnected:
		if country != "" {
			sr.Message = fmt.Sprintf("Connecté — %s", country)
		} else {
			sr.Message = "Connecté"
		}
	case ipc.StatusConnecting:
		sr.Message = "Reconnexion en cours..."
	case ipc.StatusError:
		sr.Message = fmt.Sprintf("Erreur — %s", resp.Error)
	default:
		sr.Message = "Déconnecté"
	}

	return sr
}

// countryFromDomain extracts a country name from the relay domain.
// It checks known relay prefixes; falls back to the domain itself.
func countryFromDomain(domain string) string {
	for prefix, country := range relayCountryMap {
		if len(domain) >= len(prefix) && domain[:len(prefix)] == prefix {
			return country
		}
	}
	if domain != "" {
		return domain
	}
	return ""
}
