// Package ipc handles inter-process communication between service and tray.
package ipc

import "net"

// Listener abstracts platform-specific IPC transport (named pipe or unix socket).
type Listener interface {
	Listen() (net.Listener, error)
	Cleanup() error
}

// Action constants for IPC requests.
const (
	ActionGetStatus    = "get_status"
	ActionConnect      = "connect"
	ActionDisconnect   = "disconnect"
	ActionSetAutoStart = "set_auto_start"
	ActionQuit         = "quit"
	ActionLeakCheck    = "leak_check"
	ActionCheckUpdate  = "check_update"
	ActionUpdateStatus = "update_status"
	ActionNotifyUpdate = "notify_update"
	ActionSetBlocklist  = "set_blocklist"
	ActionSetHTTPProxy  = "set_http_proxy"
	ActionGetRegistry      = "get_registry"
	ActionSelectCountry    = "select_country"
	ActionRetryCaptive     = "retry_captive"
	ActionGetAllowIPv6Leak = "get_allow_ipv6_leak"
	ActionSetAllowIPv6Leak = "set_allow_ipv6_leak"
	// ActionGetUISupervision retourne l'état du watchdog UI (Story 5.7).
	ActionGetUISupervision = "get_ui_supervision"
	// ActionUIDisconnect est une notification envoyée par l'UI quand
	// l'utilisateur quitte le processus UI via le menu « Quitter » du tray.
	// Le service répond StatusOK et ne déclenche AUCUNE action lifecycle —
	// seul ActionQuit (réservé à levoile-ctl / SCM) arrête réellement le
	// service. Voir Story 5.8.
	ActionUIDisconnect = "ui_disconnect"
	// Story 5.9 — runtime kill-switch toggle (Mode dégradé).
	// ActionSetKillSwitchMode value must be one of "normal" or "degraded".
	// When the request is sourced from levoile-ctl, the Auth field carries
	// the machine-local token; UI requests leave it empty.
	ActionGetKillSwitchMode = "get_killswitch_mode"
	ActionSetKillSwitchMode = "set_killswitch_mode"
	// ActionTriggerRecovery forces a manual auto-recovery sequence
	// (Story 6.3 — Task 8). Intended for operator debugging and incident
	// response (invoked from levoile-ctl). The service replies StatusOK
	// immediately and runs recovery in the background; the UI observes
	// progress via the anomaly_active / anomaly_reason fields of
	// get_status. Auth required: the request carries the machine-local
	// ctl token just like the kill-switch toggle.
	ActionTriggerRecovery = "trigger_recovery"
)

// Story 5.9 — kill-switch mode values used by ActionSetKillSwitchMode and
// surfaced via Response.KillSwitchMode for UI rendering decisions.
const (
	KillSwitchModeNormal   = "normal"
	KillSwitchModeDegraded = "degraded"
)

// Status constant for captive portal mode.
const StatusCaptive = "captive"

// Status constants for IPC responses.
const (
	StatusConnected    = "connected"
	StatusConnecting   = "connecting"
	StatusDisconnected = "disconnected"
	StatusError        = "error"
	StatusOK           = "ok"
	// Story 6.2 renamed "pass"/"fail" to "ok"/"leak_detected" to align
	// with the Validation Anti-Fuite framing; Story 6.3 finalises the
	// migration by removing the deprecated aliases.
	StatusLeakOK       = "ok"
	StatusLeakDetected = "leak_detected"
	StatusLeakPending  = "pending"
	StatusUpdateReady  = "update_ready"
	StatusUpToDate     = "up_to_date"
	StatusDownloading  = "downloading"
	StatusInstalled     = "installed"
	StatusInstallFailed = "install_failed"
	StatusRollback      = "rollback"
)

// Request is a JSON message sent from client to service.
type Request struct {
	Action string `json:"action"`
	Value  string `json:"value,omitempty"`
	// Auth carries the machine-local token used by levoile-ctl to authenticate
	// privileged actions (Story 5.9 — kill-switch toggle from CLI). UI requests
	// leave this empty; the IPC handler detects empty Auth as "UI source" and
	// allows the action without a token check.
	Auth string `json:"auth,omitempty"`
}

// Response is a JSON message sent from service to client.
type Response struct {
	Status           string `json:"status"`
	IP               string `json:"ip,omitempty"`
	Uptime           string `json:"uptime,omitempty"`
	Error            string `json:"error,omitempty"`
	UpdateVersion    string `json:"update_version,omitempty"`
	UpdateStatus     string `json:"update_status,omitempty"`
	InstalledVersion string `json:"installed_version,omitempty"`
	InstallError     string `json:"install_error,omitempty"`
	RollbackVersion  string `json:"rollback_version,omitempty"`
	RollbackReason   string `json:"rollback_reason,omitempty"`
	LeakStatus     string `json:"leak_status,omitempty"`
	LeakLastCheck  string `json:"leak_last_check,omitempty"`
	// LeakExpectedIP is the relay's public IP that STUN servers SHOULD
	// report when the TUN capture is intact. Populated on every leak status
	// response for transparency (story 6.2). Empty when the checker has
	// never run or when the DoH resolver was not configured.
	LeakExpectedIP string `json:"leak_expected_ip,omitempty"`
	// LeakReason carries a short classification code when LeakStatus is
	// "leak_detected": "tun_capture_likely_down" or
	// "stun_ip_differs_from_relay". Empty when LeakStatus is "ok" or
	// "pending".
	LeakReason string `json:"leak_reason,omitempty"`
	AutoStart        bool   `json:"auto_start,omitempty"`
	BlocklistEnabled bool   `json:"blocklist_enabled,omitempty"`
	HTTPProxyActive  bool   `json:"http_proxy_active,omitempty"`
	HTTPProxyAddr    string `json:"http_proxy_addr,omitempty"`
	HTTPProxySeq     uint64 `json:"http_proxy_seq,omitempty"`

	BrowserPoliciesApplied []string `json:"browser_policies_applied,omitempty"`
	BrowserPoliciesFailed  []string `json:"browser_policies_failed,omitempty"`

	RealIP       string `json:"real_ip,omitempty"`       // client's real IP (detected before tunnel)

	RelayDomain  string `json:"relay_domain,omitempty"`  // active relay domain
	RelayID      string `json:"relay_id,omitempty"`      // active relay ID (e.g. "relay-fr-01")
	RelayLatency string `json:"relay_latency,omitempty"` // measured latency (e.g. "85ms")
	Country      string `json:"country,omitempty"`       // country name in French (e.g. "France")
	CountryFlag  string `json:"country_flag,omitempty"`  // country flag emoji (e.g. "🇫🇷")

	RegistryCountries []RegistryCountry `json:"registry_countries,omitempty"`

	// CircuitBreakerTripped is true when the tunnel reconnector has given up
	// after 5 consecutive failures. CircuitBreakerMessage carries a French
	// user-facing message suitable for a UI banner.
	CircuitBreakerTripped bool   `json:"circuit_breaker_tripped,omitempty"`
	CircuitBreakerMessage string `json:"circuit_breaker_message,omitempty"`

	// FailoverAlert carries a French user-facing message set when the
	// FailoverManager crosses a country boundary (Story 4.4). Cleared on
	// manual Connect or SelectCountry.
	FailoverAlert string `json:"failover_alert,omitempty"`
	// CurrentCountryCode is the ISO2 code of the country hosting the active
	// relay. May differ from the user's preferred country after an
	// inter-country failover.
	CurrentCountryCode string `json:"current_country_code,omitempty"`

	// ConcurrentVPN is true when the preflight scan (story 2.3) detected an
	// active third-party VPN on the machine and refused to start the tunnel.
	// When true, Error carries the French user-facing message from
	// preflight.ErrConcurrentVPN.
	ConcurrentVPN bool `json:"concurrent_vpn,omitempty"`

	// FirewallAltered is true when the WFP/nftables watchdog (Story 2.7) has
	// detected external tampering with kill-switch rules.
	FirewallAltered bool `json:"firewall_altered,omitempty"`

	// AllowIPv6Leak is true when IPv6 traffic is allowed to bypass the kill
	// switch (Story 2.9). The UI uses this to show a permanent warning indicator.
	AllowIPv6Leak bool `json:"allow_ipv6_leak,omitempty"`

	// KillSwitchMode reports the current kill-switch mode (Story 5.9):
	//   - "normal":   OS firewall active, default safe state
	//   - "degraded": firewall disabled, traffic in clear; UI must show a
	//                 permanent red banner + red tray icon until restored
	// Always populated in get_status responses (and noop fast paths so
	// the UI never sees an empty value during preflight).
	KillSwitchMode string `json:"killswitch_mode,omitempty"`

	// CaptivePortal is true when the service is in captive portal mode
	// (firewall lockdown relaxed, waiting for portal authentication).
	CaptivePortal bool   `json:"captive_portal,omitempty"`
	// CaptiveProbeURL is the URL that triggered captive detection.
	CaptiveProbeURL string `json:"captive_probe_url,omitempty"`

	// UISupervision exposes the state of the levoile-ui supervisor
	// (Story 5.7). Pointer + omitempty so Linux installs (where the
	// watchdog is delegated to systemd) can leave it nil.
	UISupervision *UISupervisionState `json:"ui_supervision,omitempty"`

	// AnomalyActive is true while Program.RecoverFromAnomaly is running a
	// reconnect sequence (Story 6.3). When true, AnomalyReason carries one
	// of "leak_detected", "tun_altered", or "manual". Consumers must pair
	// the two fields: checking AnomalyReason alone without AnomalyActive
	// does not tell you whether the recovery is in flight or already
	// completed.
	AnomalyActive bool   `json:"anomaly_active,omitempty"`
	AnomalyReason string `json:"anomaly_reason,omitempty"`
}

// UISupervisionState mirrors uiwatchdog.Snapshot but lives in the IPC
// package to keep service-side packages free of cross-domain types.
type UISupervisionState struct {
	Enabled            bool   `json:"enabled"`
	LastRestartAt      string `json:"last_restart_at,omitempty"`
	RestartCountWindow int    `json:"restart_count_window"`
	BackoffUntil       string `json:"backoff_until,omitempty"`
}

// RegistryCountry holds country info for the registry response.
type RegistryCountry struct {
	Code       string `json:"code"`        // ISO: "is", "de", "fi", "us"
	Name       string `json:"name"`        // French: "Islande"
	Flag       string `json:"flag"`        // Emoji: "🇮🇸"
	RelayCount int    `json:"relay_count"` // Number of active relays
	Active     bool   `json:"active"`      // true if currently selected country
}
