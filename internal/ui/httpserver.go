// Package ui implements the unified UI binary combining systray, webview, and local HTTP server.
package ui

import (
	"context"
	"encoding/json"
	"fmt"
	"io/fs"
	"net"
	"net/http"

	"github.com/velia-the-veil/le_voile/internal/config"
	"github.com/velia-the-veil/le_voile/internal/ipc"
)

// APIStatusResponse is the JSON response for GET /api/status.
type APIStatusResponse struct {
	Status           string `json:"status"`
	IP               string `json:"ip"`
	RealIP           string `json:"real_ip"`
	Country          string `json:"country"`
	CountryFlag      string `json:"country_flag"`
	RelayID          string `json:"relay_id"`
	RelayLatency     string `json:"relay_latency"`
	Uptime           string `json:"uptime"`
	Message          string `json:"message"`
	HTTPProxyActive  bool   `json:"http_proxy_active"`
	BlocklistEnabled bool   `json:"blocklist_enabled"`
	AutoStart        bool   `json:"auto_start"`
}

// HTTPServer serves frontend assets and exposes a REST JSON API that proxies to the service via IPC.
type HTTPServer struct {
	mux      *http.ServeMux
	server   *http.Server
	ipc      *SafeIPCClient
	listener net.Listener
	ready    chan struct{}
}

// NewHTTPServer creates an HTTP server bound to 127.0.0.1 with a dynamic port.
// frontendFS should be an embed.FS containing the frontend assets (index.html, src/, assets/).
func NewHTTPServer(ipcClient *SafeIPCClient, frontendFS fs.FS) *HTTPServer {
	s := &HTTPServer{
		mux:   http.NewServeMux(),
		ipc:   ipcClient,
		ready: make(chan struct{}),
	}

	// Serve frontend assets at root.
	s.mux.Handle("/", http.FileServer(http.FS(frontendFS)))

	// API endpoints.
	s.mux.HandleFunc("/api/status", s.handleStatus)
	s.mux.HandleFunc("/api/connect", s.handleConnect)
s.mux.HandleFunc("/api/registry", s.handleRegistry)
	s.mux.HandleFunc("/api/country", s.handleCountry)
	s.mux.HandleFunc("/api/settings", s.handleGetSettings)
	s.mux.HandleFunc("/api/settings/autostart", s.handleSetAutoStart)
	s.mux.HandleFunc("/api/settings/blocklist", s.handleSetBlocklist)
	s.mux.HandleFunc("/api/settings/httpproxy", s.handleSetHTTPProxy)

	return s
}

// Start begins listening on 127.0.0.1:0 (dynamic port) and serves until ctx is cancelled.
func (s *HTTPServer) Start(ctx context.Context) error {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		close(s.ready) // unblock Addr() callers even on failure
		return fmt.Errorf("ui: httpserver: listen: %w", err)
	}
	s.listener = ln
	s.server = &http.Server{Handler: s.mux}
	close(s.ready)

	go func() {
		<-ctx.Done()
		s.server.Close()
	}()

	err = s.server.Serve(ln)
	if err == http.ErrServerClosed {
		return nil
	}
	return fmt.Errorf("ui: httpserver: serve: %w", err)
}

// Shutdown gracefully shuts down the server, waiting for active requests to finish.
func (s *HTTPServer) Shutdown(ctx context.Context) error {
	<-s.ready // wait for Start to set s.server (or fail)
	if s.server != nil {
		return s.server.Shutdown(ctx)
	}
	return nil
}

// Addr returns the listener address. Blocks until the server is ready.
func (s *HTTPServer) Addr() string {
	<-s.ready
	if s.listener == nil {
		return ""
	}
	return s.listener.Addr().String()
}

// Ready returns a channel that is closed when the server is listening.
func (s *HTTPServer) Ready() <-chan struct{} {
	return s.ready
}

func (s *HTTPServer) handleStatus(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	resp := s.sendIPC(r.Context(), ipc.ActionGetStatus, "")
	api := APIStatusResponse{
		Status:           resp.Status,
		IP:               resp.IP,
		RealIP:           resp.RealIP,
		Country:          resp.Country,
		CountryFlag:      resp.CountryFlag,
		RelayID:          resp.RelayID,
		RelayLatency:     resp.RelayLatency,
		Uptime:           resp.Uptime,
		Message:          statusMessage(resp.Status, resp.Country),
		HTTPProxyActive:  resp.HTTPProxyActive,
		BlocklistEnabled: resp.BlocklistEnabled,
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(api)
}

func (s *HTTPServer) handleConnect(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	resp := s.sendIPC(r.Context(), ipc.ActionConnect, "")
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(actionResponse(resp))
}

// actionResponse builds a JSON-friendly map from an IPC response for connect actions.
func actionResponse(resp ipc.Response) map[string]string {
	m := map[string]string{"status": resp.Status}
	if resp.Error != "" {
		m["error"] = resp.Error
	}
	return m
}

func (s *HTTPServer) handleRegistry(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	resp := s.sendIPC(r.Context(), ipc.ActionGetRegistry, "")
	countries := resp.RegistryCountries
	if countries == nil {
		countries = []ipc.RegistryCountry{}
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"countries": countries,
	})
}

func (s *HTTPServer) handleCountry(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var body struct {
		Code string `json:"code"`
	}
	r.Body = http.MaxBytesReader(w, r.Body, 1024)
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.Code == "" {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}
	resp := s.sendIPC(r.Context(), ipc.ActionSelectCountry, body.Code)
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(actionResponse(resp))
}

// sendIPC sends an IPC request and returns the response. On error, returns a disconnected response.
func (s *HTTPServer) sendIPC(ctx context.Context, action, value string) ipc.Response {
	resp, err := s.ipc.SendContext(ctx, ipc.Request{Action: action, Value: value})
	if err != nil {
		return ipc.Response{Status: ipc.StatusDisconnected}
	}
	return resp
}

func (s *HTTPServer) handleGetSettings(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	resp := s.sendIPC(r.Context(), ipc.ActionGetStatus, "")

	// Read auto_start from config file (not in IPC status).
	autoStart := true // default
	if cfgPath, err := config.DefaultPath(); err == nil {
		if cfg, err := config.Load(cfgPath); err == nil {
			autoStart = cfg.Client.AutoStart
		}
	}

	settings := map[string]bool{
		"auto_start": autoStart,
		"blocklist":  resp.BlocklistEnabled,
		"http_proxy": resp.HTTPProxyActive,
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(settings)
}

func (s *HTTPServer) handleSetAutoStart(w http.ResponseWriter, r *http.Request) {
	s.handleBoolSetting(w, r, ipc.ActionSetAutoStart)
}

func (s *HTTPServer) handleSetBlocklist(w http.ResponseWriter, r *http.Request) {
	s.handleBoolSetting(w, r, ipc.ActionSetBlocklist)
}

func (s *HTTPServer) handleSetHTTPProxy(w http.ResponseWriter, r *http.Request) {
	s.handleBoolSetting(w, r, ipc.ActionSetHTTPProxy)
}

func (s *HTTPServer) handleBoolSetting(w http.ResponseWriter, r *http.Request, action string) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var body struct {
		Enabled bool `json:"enabled"`
	}
	r.Body = http.MaxBytesReader(w, r.Body, 1024)
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}
	value := "false"
	if body.Enabled {
		value = "true"
	}
	resp := s.sendIPC(r.Context(), action, value)
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(actionResponse(resp))
}

// statusMessage returns a French non-technical message for the given status.
func statusMessage(status, country string) string {
	switch status {
	case ipc.StatusConnected:
		if country != "" {
			return "Connecté — " + country
		}
		return "Connecté"
	case ipc.StatusConnecting:
		return "Reconnexion en cours..."
	default:
		return "Déconnecté"
	}
}
