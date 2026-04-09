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
	ActionGetRegistry   = "get_registry"
	ActionSelectCountry = "select_country"
)

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
}

// RegistryCountry holds country info for the registry response.
type RegistryCountry struct {
	Code       string `json:"code"`        // ISO: "is", "de", "fi", "us"
	Name       string `json:"name"`        // French: "Islande"
	Flag       string `json:"flag"`        // Emoji: "🇮🇸"
	RelayCount int    `json:"relay_count"` // Number of active relays
	Active     bool   `json:"active"`      // true if currently selected country
}
