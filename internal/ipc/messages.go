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
	ActionSTUNStatus   = "stun_status"
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
	StatusSTUNActive   = "active"
	StatusSTUNInactive = "inactive"
	StatusLeakPass    = "pass"
	StatusLeakFail    = "fail"
	StatusLeakPending = "pending"
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
	LeakStatus       string `json:"leak_status,omitempty"`
	LeakLastCheck    string `json:"leak_last_check,omitempty"`
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

	// CaptivePortal is true when the service is in captive portal mode
	// (firewall lockdown relaxed, waiting for portal authentication).
	CaptivePortal bool   `json:"captive_portal,omitempty"`
	// CaptiveProbeURL is the URL that triggered captive detection.
	CaptiveProbeURL string `json:"captive_probe_url,omitempty"`
}

// RegistryCountry holds country info for the registry response.
type RegistryCountry struct {
	Code       string `json:"code"`        // ISO: "is", "de", "fi", "us"
	Name       string `json:"name"`        // French: "Islande"
	Flag       string `json:"flag"`        // Emoji: "🇮🇸"
	RelayCount int    `json:"relay_count"` // Number of active relays
	Active     bool   `json:"active"`      // true if currently selected country
}
