// Package ui implements the unified UI binary combining systray, webview, and local HTTP server.
package ui

import (
	"context"
	"crypto/rand"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io/fs"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/velia-the-veil/le_voile/internal/config"
	"github.com/velia-the-veil/le_voile/internal/ipc"
)

// APIStatusResponse is the JSON response for GET /api/status.
type APIStatusResponse struct {
	Status             string `json:"status"`
	IP                 string `json:"ip"`
	RealIP             string `json:"real_ip"`
	Country            string `json:"country"`
	CountryFlag        string `json:"country_flag"`
	RelayID            string `json:"relay_id"`
	RelayLatency       string `json:"relay_latency"`
	Uptime             string `json:"uptime"`
	Message            string `json:"message"`
	HTTPProxyActive    bool   `json:"http_proxy_active"`
	BlocklistEnabled   bool   `json:"blocklist_enabled"`
	AutoStart          bool   `json:"auto_start"`
	CaptivePortal      bool   `json:"captive_portal,omitempty"`
	AllowIPv6Leak      bool   `json:"allow_ipv6_leak,omitempty"`
	// KillSwitchMode: "normal" or "degraded" (Story 5.9). Always emitted so
	// the frontend can drive the permanent banner without ambiguity.
	KillSwitchMode     string `json:"killswitch_mode"`
	FailoverAlert      string `json:"failover_alert,omitempty"`
	CurrentCountryCode string `json:"current_country_code,omitempty"`
	// ServiceReachable is true when the IPC transport successfully reached
	// the service for this request. When false, ServiceStartHint carries
	// the OS-specific command the user must run to start the service
	// (Story 5.6 AC1/AC2). Always emitted so the frontend can key off it
	// without probing for presence.
	ServiceReachable bool              `json:"service_reachable"`
	ServiceStartHint *ServiceStartHint `json:"service_start_hint,omitempty"`
	// AnomalyActive is true while the service is running an auto-recovery
	// sequence (Story 6.3 — STUN leak detected or TUN watchdog fired). The
	// frontend uses this to show the orange #anomaly-banner and (when the
	// tray has access to the same field) to swap in IconAlert. When false,
	// AnomalyReason is the empty string.
	AnomalyActive bool   `json:"anomaly_active,omitempty"`
	AnomalyReason string `json:"anomaly_reason,omitempty"`
	// IntegrityFailed is true when the service detected external tampering
	// with config.toml at startup (HMAC mismatch — NFR9j / Story 7.5). The
	// frontend shows a permanent recovery banner and hides connect actions
	// while this is true. No in-process reset is exposed by design.
	IntegrityFailed bool `json:"integrity_failed,omitempty"`
}

// HTTPServer serves frontend assets and exposes a REST JSON API that proxies to the service via IPC.
type HTTPServer struct {
	mux      *http.ServeMux
	server   *http.Server
	ipc      *SafeIPCClient
	listener net.Listener
	ready    chan struct{}
	prefs    *PrefsStore

	// pendingUIEvent carries a one-shot UI event triggered from outside the
	// webview (typically the system tray menu) — for example, "killswitch_modal"
	// to ask the frontend to display the destructive confirmation modal
	// (Story 5.9). Read-and-clear semantics: the next GET /api/ui-event call
	// returns the value once and resets it to "".
	pendingUIEvent eventSlot

	// csrfToken is a per-process random token guarding sensitive POST endpoints
	// (currently /api/settings/killswitch — Story 5.9 M2 fix). The frontend
	// fetches it from /api/csrf-token on init and sends it back via
	// X-CSRF-Token. Defense-in-depth only: a determined same-user process can
	// trivially fetch the token first; real isolation requires switching the
	// loopback TCP listener to a unix socket with restrictive perms (deferred
	// Epic 7 follow-up).
	csrfToken string
}

// NewHTTPServer creates an HTTP server bound to 127.0.0.1 with a dynamic port.
// frontendFS should be an embed.FS containing the frontend assets (index.html, src/, assets/).
func NewHTTPServer(ipcClient *SafeIPCClient, frontendFS fs.FS) *HTTPServer {
	s := &HTTPServer{
		mux:       http.NewServeMux(),
		ipc:       ipcClient,
		ready:     make(chan struct{}),
		prefs:     NewPrefsStore(),
		csrfToken: newCSRFToken(),
	}

	// Serve frontend assets at root.
	s.mux.Handle("/", http.FileServer(http.FS(frontendFS)))

	// API endpoints.
	s.mux.HandleFunc("/api/status", s.handleStatus)
	s.mux.HandleFunc("/api/connect", s.handleConnect)
	s.mux.HandleFunc("/api/disconnect", s.handleDisconnect)
	s.mux.HandleFunc("/api/leak-status", s.handleLeakStatus)
	s.mux.HandleFunc("/api/update-status", s.handleUpdateStatus)
	s.mux.HandleFunc("/api/registry", s.handleRegistry)
	s.mux.HandleFunc("/api/country", s.handleCountry)
	s.mux.HandleFunc("/api/settings", s.handleGetSettings)
	s.mux.HandleFunc("/api/settings/autostart", s.handleSetAutoStart)
	s.mux.HandleFunc("/api/settings/blocklist", s.handleSetBlocklist)
	s.mux.HandleFunc("/api/settings/httpproxy", s.handleSetHTTPProxy)
	s.mux.HandleFunc("/api/settings/ipv6leak", s.handleSetIPv6Leak)
	s.mux.HandleFunc("/api/settings/killswitch", s.handleSetKillSwitch)
	s.mux.HandleFunc("/api/captive/retry", s.handleCaptiveRetry)
	s.mux.HandleFunc("/api/ui-prefs", s.handleUIPrefs)
	s.mux.HandleFunc("/api/ui-event", s.handleUIEvent)
	s.mux.HandleFunc("/api/csrf-token", s.handleCSRFToken)

	return s
}

// originGuard rejects cross-origin requests. The UI webview issues same-origin
// fetches (Origin == http://127.0.0.1:PORT) or origin-less requests (file://
// context, browser extensions, curl). Any Origin/Referer header pointing at a
// non-loopback host is an attack: either a malicious page running in a tab the
// user happened to visit, or a DNS rebinding attempt targeting the dynamic
// listener port. Either way, reject before the handler runs (fix C2 audit
// sécurité).
//
// Requests without Origin and without Referer are allowed — this covers the
// webview's direct fetches, CLI curl for local debugging, and the frontend
// bundle fetching its own static assets. The Host header is also validated
// to catch raw IP attacks on 127.0.0.1 from a user-typed external DNS name.
func originGuard(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !isOriginAllowed(r) {
			http.Error(w, "Forbidden: cross-origin request", http.StatusForbidden)
			return
		}
		next.ServeHTTP(w, r)
	})
}

// isOriginAllowed returns true when the request's Origin/Referer (if any)
// and Host target the loopback interface. Exported for tests.
func isOriginAllowed(r *http.Request) bool {
	// Host must be loopback — blocks DNS-rebinding where an attacker hosts
	// evil.example resolving to 127.0.0.1. Browsers send the user-typed
	// Host name, not the resolved IP.
	host := r.Host
	if colon := strings.LastIndexByte(host, ':'); colon >= 0 {
		host = host[:colon]
	}
	if !isLoopbackHost(host) {
		return false
	}
	// Origin takes precedence. When absent, fall back to Referer's origin
	// prefix. When both absent, accept (same-origin fetches and non-browser
	// clients like curl don't send either).
	if o := r.Header.Get("Origin"); o != "" {
		return isLoopbackOrigin(o)
	}
	if ref := r.Header.Get("Referer"); ref != "" {
		return isLoopbackOrigin(ref)
	}
	return true
}

// isLoopbackHost matches 127.0.0.1, ::1, and localhost. Conservative: no
// wildcard hosts, no resolving — purely textual.
func isLoopbackHost(host string) bool {
	switch host {
	case "127.0.0.1", "::1", "[::1]", "localhost":
		return true
	}
	return false
}

// isLoopbackOrigin matches an Origin/Referer header that begins with a
// loopback scheme+host prefix. Any port is accepted (our listener picks
// dynamically). Schemes other than http/https are rejected outright.
func isLoopbackOrigin(origin string) bool {
	for _, prefix := range []string{
		"http://127.0.0.1",
		"http://[::1]",
		"http://localhost",
		"https://127.0.0.1",
		"https://[::1]",
		"https://localhost",
	} {
		if strings.HasPrefix(origin, prefix) {
			// Ensure the next char is ":" (port), "/" (path), or EOS —
			// prevents bypass via http://127.0.0.1.evil.com.
			rest := origin[len(prefix):]
			if rest == "" || rest[0] == ':' || rest[0] == '/' {
				return true
			}
		}
	}
	return false
}

// newCSRFToken returns 32 hex-encoded random bytes for CSRF defense-in-depth.
// Falls back to a static placeholder only on the (effectively impossible)
// crypto/rand failure — better to keep the server runnable than to crash.
func newCSRFToken() string {
	buf := make([]byte, 32)
	if _, err := rand.Read(buf); err != nil {
		return "fallback-token-rand-failed"
	}
	return hex.EncodeToString(buf)
}

// handleCSRFToken returns the per-process CSRF token for protected endpoints.
// Story 5.9 M2 fix.
func (s *HTTPServer) handleCSRFToken(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"token": s.csrfToken})
}

// requireCSRF returns true when the request's X-CSRF-Token header matches
// (constant time). Endpoints that mutate destructive state should call this
// first and 403 on mismatch.
func (s *HTTPServer) requireCSRF(r *http.Request) bool {
	got := r.Header.Get("X-CSRF-Token")
	if got == "" || s.csrfToken == "" {
		return false
	}
	return subtle.ConstantTimeCompare([]byte(got), []byte(s.csrfToken)) == 1
}

// eventSlot is a thread-safe one-shot string slot used to hand events from
// the tray (Go side) to the webview (browser side). Stores a per-set
// timestamp and expires entries older than eventSlotTTL on read so a click
// queued just before the user closed the webview does not surface in a
// future, unrelated session (Story 5.9 L3 fix).
type eventSlot struct {
	mu    sync.Mutex
	value string
	setAt time.Time
}

// eventSlotTTL caps how long a queued event remains valid. 10 s is generous
// enough to cover a slow webview cold-start (~3-5 s on first launch) yet
// short enough that a stale event won't fire in a much-later session.
const eventSlotTTL = 10 * time.Second

func (e *eventSlot) set(v string) {
	e.mu.Lock()
	e.value = v
	e.setAt = time.Now()
	e.mu.Unlock()
}

func (e *eventSlot) take() string {
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.value == "" {
		return ""
	}
	if time.Since(e.setAt) > eventSlotTTL {
		e.value = ""
		return ""
	}
	v := e.value
	e.value = ""
	return v
}

// drain wipes the slot unconditionally — used when the webview lifecycle
// transitions so a click queued for the previous instance doesn't pop up in
// a fresh one.
func (e *eventSlot) drain() {
	e.mu.Lock()
	e.value = ""
	e.mu.Unlock()
}

// DrainPendingUIEvent clears any queued one-shot UI event. Call from the UI
// shutdown path so a stale "killswitch_modal" can't survive a webview close.
func (s *HTTPServer) DrainPendingUIEvent() {
	s.pendingUIEvent.drain()
}

// TriggerUIEvent stores a one-shot event consumed by the next /api/ui-event
// poll. Used by the systray menu to ask the webview to display a modal
// (Story 5.9 — "Mode dégradé" entry).
func (s *HTTPServer) TriggerUIEvent(name string) {
	s.pendingUIEvent.set(name)
}

func (s *HTTPServer) handleUIEvent(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"event": s.pendingUIEvent.take()})
}

// Start begins listening on 127.0.0.1:0 (dynamic port) and serves until ctx is cancelled.
func (s *HTTPServer) Start(ctx context.Context) error {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		close(s.ready) // unblock Addr() callers even on failure
		return fmt.Errorf("ui: httpserver: listen: %w", err)
	}
	s.listener = ln
	s.server = &http.Server{Handler: originGuard(s.mux)}
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
	reachable := resp.Error != "service_unreachable"
	msg := statusMessage(resp.Status, resp.Country)
	if resp.CaptivePortal {
		msg = "Portail Wi-Fi détecté — authentifiez-vous"
	}
	api := APIStatusResponse{
		Status:             resp.Status,
		IP:                 resp.IP,
		RealIP:             resp.RealIP,
		Country:            resp.Country,
		CountryFlag:        resp.CountryFlag,
		RelayID:            resp.RelayID,
		RelayLatency:       resp.RelayLatency,
		Uptime:             resp.Uptime,
		Message:            msg,
		HTTPProxyActive:    resp.HTTPProxyActive,
		BlocklistEnabled:   resp.BlocklistEnabled,
		CaptivePortal:      resp.CaptivePortal,
		AllowIPv6Leak:      resp.AllowIPv6Leak,
		KillSwitchMode:     resp.KillSwitchMode,
		FailoverAlert:      resp.FailoverAlert,
		CurrentCountryCode: resp.CurrentCountryCode,
		ServiceReachable:   reachable,
		AnomalyActive:      resp.AnomalyActive,
		AnomalyReason:      resp.AnomalyReason,
		IntegrityFailed:    resp.IntegrityFailed,
	}
	// Normalize: when service is unreachable, KillSwitchMode is empty —
	// surface "normal" so the frontend defaults to safe rendering rather
	// than flashing the degraded banner.
	if api.KillSwitchMode == "" {
		api.KillSwitchMode = ipc.KillSwitchModeNormal
	}
	// Emit the start hint only when the service is down; otherwise frontends
	// that blindly render the hint would show a phantom "service down" block.
	if !reachable {
		hint := CurrentServiceStartHint()
		api.ServiceStartHint = &hint
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

func (s *HTTPServer) handleDisconnect(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	resp := s.sendIPC(r.Context(), ipc.ActionDisconnect, "")
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(actionResponse(resp))
}

// APILeakStatusResponse is the JSON response for GET /api/leak-status.
// Story 6.2: Status uses "ok" / "leak_detected" / "pending" (renamed from
// "pass" / "fail"). ExpectedIP and Reason are populated pass-through from
// the IPC layer so the webview can display the reference IP and an
// explanation when a leak is detected.
type APILeakStatusResponse struct {
	Status     string `json:"status"` // ok / leak_detected / pending
	LastCheck  string `json:"last_check,omitempty"`
	ExpectedIP string `json:"expected_ip,omitempty"`
	Reason     string `json:"reason,omitempty"`
}

// handleLeakStatus returns the cached leak-check result. Uses ActionGetStatus
// (which fills LeakStatus/LeakLastCheck from the periodic scheduler cache) so
// that frontend polling does not trigger a 20 s live STUN check on every call.
func (s *HTTPServer) handleLeakStatus(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	resp := s.sendIPC(r.Context(), ipc.ActionGetStatus, "")
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(APILeakStatusResponse{
		Status:     resp.LeakStatus,
		LastCheck:  resp.LeakLastCheck,
		ExpectedIP: resp.LeakExpectedIP,
		Reason:     resp.LeakReason,
	})
}

// APIUpdateStatusResponse is the JSON response for GET /api/update-status.
type APIUpdateStatusResponse struct {
	Status           string `json:"status"`
	Version          string `json:"version,omitempty"`
	InstalledVersion string `json:"installed_version,omitempty"`
	InstallError     string `json:"install_error,omitempty"`
	RollbackVersion  string `json:"rollback_version,omitempty"`
	RollbackReason   string `json:"rollback_reason,omitempty"`
}

func (s *HTTPServer) handleUpdateStatus(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	resp := s.sendIPC(r.Context(), ipc.ActionUpdateStatus, "")
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(APIUpdateStatusResponse{
		Status:           resp.UpdateStatus,
		Version:          resp.UpdateVersion,
		InstalledVersion: resp.InstalledVersion,
		InstallError:     resp.InstallError,
		RollbackVersion:  resp.RollbackVersion,
		RollbackReason:   resp.RollbackReason,
	})
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

// sendIPC sends an IPC request and returns the response. On transport failure
// the response carries Status=disconnected AND Error="service_unreachable" so
// that POST handlers wrapping via actionResponse surface a machine-readable
// failure mode to the frontend (Story 5.4 review finding M5). Read-only
// handlers (/api/status, /api/registry, /api/leak-status, /api/update-status)
// build their own response structs and ignore the Error field, preserving
// their pre-existing silent-fallback UX.
func (s *HTTPServer) sendIPC(ctx context.Context, action, value string) ipc.Response {
	resp, err := s.ipc.SendContext(ctx, ipc.Request{Action: action, Value: value})
	if err != nil {
		return ipc.Response{Status: ipc.StatusDisconnected, Error: "service_unreachable"}
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

	settings := map[string]any{
		"auto_start":      autoStart,
		"blocklist":       resp.BlocklistEnabled,
		"http_proxy":      resp.HTTPProxyActive,
		"allow_ipv6_leak": resp.AllowIPv6Leak,
		"killswitch_mode": defaultKillSwitchMode(resp.KillSwitchMode),
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(settings)
}

// defaultKillSwitchMode normalizes empty IPC values to "normal" so frontends
// never have to special-case unreachable-service responses (Story 5.9).
func defaultKillSwitchMode(v string) string {
	if v == "" {
		return ipc.KillSwitchModeNormal
	}
	return v
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

func (s *HTTPServer) handleSetIPv6Leak(w http.ResponseWriter, r *http.Request) {
	s.handleBoolSetting(w, r, ipc.ActionSetAllowIPv6Leak)
}

// handleSetKillSwitch toggles the OS-level firewall (Story 5.9).
// Body: {"mode": "degraded"} or {"mode": "normal"}.
// Response: {"status": "ok"|"error", "killswitch_mode": "...", "error": "..."}.
//
// Requires X-CSRF-Token header matching the per-process server token (Story
// 5.9 M2 fix). The frontend fetches the token from /api/csrf-token on init
// and includes it on every kill-switch POST. Defense-in-depth against
// opportunistic same-user processes; loopback-only TCP cannot enforce
// process identity without a unix-socket migration (Epic 7 follow-up).
//
// UI requests carry no Auth IPC field — the IPC handler treats the call as
// "ui source" and skips token verification (CSRF is the sole gate at the HTTP
// boundary).
func (s *HTTPServer) handleSetKillSwitch(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if !s.requireCSRF(r) {
		http.Error(w, "csrf token missing or invalid", http.StatusForbidden)
		return
	}
	var body struct {
		Mode string `json:"mode"`
	}
	r.Body = http.MaxBytesReader(w, r.Body, 1024)
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}
	if body.Mode != ipc.KillSwitchModeNormal && body.Mode != ipc.KillSwitchModeDegraded {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}
	resp := s.sendIPC(r.Context(), ipc.ActionSetKillSwitchMode, body.Mode)
	out := map[string]string{"status": resp.Status}
	if resp.KillSwitchMode != "" {
		out["killswitch_mode"] = resp.KillSwitchMode
	}
	if resp.Error != "" {
		out["error"] = resp.Error
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(out)
}

func (s *HTTPServer) handleCaptiveRetry(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	resp := s.sendIPC(r.Context(), ipc.ActionRetryCaptive, "")
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(actionResponse(resp))
}

// handleUIPrefs reads or writes frontend-only preferences. GET returns the
// stored prefs (or defaults on first run). POST accepts a JSON body and
// persists the merged result.
//
// Server-side persistence is required because the webview's localStorage is
// scoped by origin, and the HTTP server binds to 127.0.0.1 with a DYNAMIC
// port — every app restart would reset localStorage. (Story 5.5 review H3.)
func (s *HTTPServer) handleUIPrefs(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		prefs, _ := s.prefs.Load() // errors → defaults; don't leak filesystem state to frontend
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(prefs)
	case http.MethodPost:
		var body UIPrefs
		r.Body = http.MaxBytesReader(w, r.Body, 1024)
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			http.Error(w, "bad request", http.StatusBadRequest)
			return
		}
		if err := s.prefs.Save(body); err != nil {
			// Swallow the filesystem error in the response body (no user
			// data to leak) and return 500 so the frontend can show a
			// generic "impossible d'enregistrer" feedback if desired.
			http.Error(w, "save failed", http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(body)
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
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
