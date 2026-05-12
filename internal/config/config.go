// Package config handles application configuration loading and management.
package config

import (
	"bytes"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"sync"

	"github.com/BurntSushi/toml"
)

var tunNameRe = regexp.MustCompile(`^[a-z][a-z0-9]{0,14}$`)

// Mu serializes load-modify-save sequences across all packages that mutate
// the on-disk TOML config. Story 5.9 H2 fix — previously the IPC handler held
// a package-internal mutex and the cmd/client kill-switch persister held no
// mutex at all, allowing concurrent writers to lose updates. Every config
// writer (IPC handlers, kill-switch persister, etc.) MUST take this mutex
// around its load → modify → save sequence.
var Mu sync.Mutex

// Config holds the application configuration.
type Config struct {
	Relay     RelayConfig     `toml:"relay"`
	Client    ClientConfig    `toml:"client"`
	STUN      STUNConfig      `toml:"stun"`
	Update    UpdateConfig    `toml:"update"`
	Blocklist BlocklistConfig `toml:"blocklist"`
	Registry  RegistryConfig  `toml:"registry"`
	HTTPProxy       HTTPProxyConfig       `toml:"http_proxy"`
	BrowserPolicies BrowserPoliciesConfig `toml:"browser_policies"`
	TUN             TUNConfig             `toml:"tun"`
	Firewall        FirewallConfig        `toml:"firewall"`
	Captive         CaptiveConfig         `toml:"captive"`
}

// FirewallConfig holds OS-level kill-switch settings (WFP on Windows,
// nftables on Linux). See Story 2.6/2.7.
type FirewallConfig struct {
	// EnableKillSwitch activates the kernel-level kill switch on connect.
	// When false, Activate() is a no-op (degraded mode, see Story 5.9).
	EnableKillSwitch bool `toml:"enable_killswitch"`
	// AllowIPv6Leak when true skips IPv6 BLOCK filters, letting native IPv6
	// bypass the kill switch. Default false (IPv6 blocked). See Story 2.9.
	AllowIPv6Leak bool `toml:"allow_ipv6_leak"`
}

// CaptiveConfig holds captive portal detection settings (Story 2.8).
type CaptiveConfig struct {
	// Enabled activates captive portal detection before tunnel connect.
	// When false, the probe is skipped and Connect proceeds directly.
	Enabled bool `toml:"enabled"`
	// ProbeURLs overrides the default probe endpoints. If empty, the built-in
	// Apple + Google URLs are used. Intentionally plain HTTP (not HTTPS) because
	// captive portals intercept HTTP redirects.
	ProbeURLs []string `toml:"probe_urls,omitempty"`
}

// TUNConfig holds TUN/Wintun interface settings (Epic 2 — capture L3).
type TUNConfig struct {
	// Enabled active la création de l'interface TUN/Wintun au démarrage.
	// Défaut false tant que les stories routing (2.4), firewall (2.6/2.7) et
	// pump tunnel (1.1 étendue) ne sont pas livrées. Quand enabled=true sans
	// ces dépendances, l'interface est créée mais inutile.
	Enabled bool   `toml:"enabled"`
	Name    string `toml:"name"` // ex: "levoile0" — regex ^[a-z][a-z0-9]{0,14}$
	MTU     int    `toml:"mtu"`  // bornes [576, 9000], défaut 1420
}

// BrowserPoliciesConfig holds browser WebRTC policy settings.
type BrowserPoliciesConfig struct {
	Enabled bool `toml:"enabled"`
}

// HTTPProxyConfig holds HTTP CONNECT proxy settings for IP camouflage.
type HTTPProxyConfig struct {
	Enabled bool `toml:"enabled"`
	Port    int  `toml:"port"`
}

// BlocklistConfig holds DNS blocklist settings.
type BlocklistConfig struct {
	Enabled        bool   `toml:"enabled"`
	UpdateInterval string `toml:"update_interval"`
}

// RegistryConfig holds dynamic relay discovery settings.
type RegistryConfig struct {
	Enabled         bool   `toml:"enabled"`
	URL             string `toml:"url"`
	MasterPublicKey string `toml:"master_public_key"`
	RefreshInterval string `toml:"refresh_interval"`
	// BootstrapDoHEnabled wraps the registry HTTP transport in a DoH resolver
	// so the first lookup of the registry hostname never hits the system
	// resolver (NFR9i). Default true when Registry is enabled.
	BootstrapDoHEnabled bool `toml:"bootstrap_doh_enabled"`
	// DoHUpstreams overrides the default Cloudflare+Quad9 endpoints.
	// Each entry must be an HTTPS URL. Empty list = built-in defaults.
	DoHUpstreams []string `toml:"doh_upstreams,omitempty"`
}

// UpdateConfig holds auto-update settings.
type UpdateConfig struct {
	Enabled       bool   `toml:"enabled"`
	CheckInterval string `toml:"check_interval"`
	RateLimitKBps int    `toml:"rate_limit_kbps"`
	GitHubOwner   string `toml:"github_owner"`
	GitHubRepo    string `toml:"github_repo"`
	// AllowWhenPackaged forces auto-update even when the binary was installed
	// by a system package manager (dpkg/rpm/pacman). Default false — package
	// manager is considered authoritative to avoid version conflicts.
	AllowWhenPackaged bool `toml:"allow_when_packaged"`
	// MaxInstallRetries caps the number of times the installer will retry
	// applying a given staged update before abandoning it and re-downloading
	// on the next check. Default 3. Values ≤ 0 fall back to 3 — there is
	// intentionally no "unlimited" mode to prevent boot-loop scenarios when
	// a binary is structurally broken (wrong arch, missing linker dep, etc.).
	MaxInstallRetries int `toml:"max_install_retries"`
}

// RelayConfig holds relay connection settings.
type RelayConfig struct {
	Domain           string `toml:"domain"`
	PublicKeyEd25519 string `toml:"public_key_ed25519"`
	Insecure         bool   `toml:"insecure,omitempty"` // skip TLS verification (development only)
}

// ClientConfig holds client behavior settings.
type ClientConfig struct {
	AutoStart        bool   `toml:"auto_start"`
	PreferredCountry string `toml:"preferred_country,omitempty"` // ISO 2-letter code: "is", "de", "fi", "us"
	SkipQuitModal    bool   `toml:"skip_quit_modal"`
}

// STUNConfig holds STUN server configuration used by the leakcheck scheduler.
// Story 6.1 refactor: no more applicative STUN relay — packets go via OS ->
// TUN -> relay NAT (L3 capture). The scheduler emits STUN Binding Requests
// via net.DialUDP, the OS routes them through levoile0 (default route).
type STUNConfig struct {
	// DefaultServer overrides the first entry of the scheduler's default
	// STUN server list ("stun.l.google.com:19302"). Optional — kept for
	// retro-compatibility with configs written before Story 6.1.
	DefaultServer string `toml:"default_server"`
	// Servers overrides the full default STUN server list. If empty, the
	// scheduler uses the built-in defaults (Google x2 + Cloudflare).
	Servers []string `toml:"servers,omitempty"`
	// LeakcheckInterval is the period between two leak checks. Default 10m.
	// Accepts any Go duration string ("5m", "1h", "30s").
	LeakcheckInterval string `toml:"leakcheck_interval,omitempty"`
}

// Bootstrap writes the embedded config.example.toml skeleton to path when
// the file does not exist. Idempotent: a second call with an existing
// config is a no-op. Applies restrictive perms (0600 / protected DACL) and
// creates parent dirs (0700) as needed. Used by the service on first start
// so a fresh install always has a signed, tightened skeleton before Load
// is called (NFR9j, Story 7.5 AC3).
//
// Callers: cmd/client/main.go only. The UI MUST NOT call Bootstrap — only
// the service owns the config file lifecycle.
func Bootstrap(path string) error {
	if _, err := os.Stat(path); err == nil {
		return nil
	} else if !os.IsNotExist(err) {
		return fmt.Errorf("config: bootstrap stat: %w", err)
	}
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return fmt.Errorf("config: bootstrap mkdir: %w", err)
	}
	tmp, err := os.CreateTemp(dir, "config-bootstrap-*.tmp")
	if err != nil {
		return fmt.Errorf("config: bootstrap create temp: %w", err)
	}
	tmpName := tmp.Name()
	if _, err := tmp.Write(exampleTOML); err != nil {
		tmp.Close()
		os.Remove(tmpName)
		return fmt.Errorf("config: bootstrap write: %w", err)
	}
	if err := tmp.Close(); err != nil {
		os.Remove(tmpName)
		return fmt.Errorf("config: bootstrap close: %w", err)
	}
	if err := os.Rename(tmpName, path); err != nil {
		os.Remove(tmpName)
		return fmt.Errorf("config: bootstrap rename: %w", err)
	}
	if err := applyRestrictedPerms(path); err != nil {
		return fmt.Errorf("config: bootstrap tighten perms: %w", err)
	}
	return nil
}

// Load reads a TOML configuration file. If the file does not exist,
// it returns a default configuration.
func Load(path string) (*Config, error) {
	cfg := &Config{
		Client: ClientConfig{AutoStart: true},
		STUN:   STUNConfig{DefaultServer: "stun.l.google.com:19302"},
		Update: UpdateConfig{
			Enabled:           true,
			CheckInterval:     "6h",
			RateLimitKBps:     500,
			GitHubOwner:       "velia-the-veil",
			GitHubRepo:        "le_voile",
			AllowWhenPackaged: false,
			MaxInstallRetries: 3,
		},
		Blocklist: BlocklistConfig{
			Enabled:        false,
			UpdateInterval: "24h",
		},
		Registry: RegistryConfig{
			Enabled:             false,
			URL:                 "https://levoile.dev",
			RefreshInterval:     "6h",
			BootstrapDoHEnabled: true,
		},
		HTTPProxy: HTTPProxyConfig{
			Enabled: false,
			Port:    50113,
		},
		BrowserPolicies: BrowserPoliciesConfig{
			Enabled: true,
		},
		TUN: TUNConfig{
			Name: "levoile0",
			MTU:  1420,
		},
		Firewall: FirewallConfig{
			EnableKillSwitch: true,
		},
		Captive: CaptiveConfig{
			Enabled: true,
		},
	}

	if _, err := toml.DecodeFile(path, cfg); err != nil {
		if os.IsNotExist(err) {
			return cfg, nil
		}
		return nil, fmt.Errorf("config: %w", err)
	}

	// Validate mandatory fields when relay is configured.
	if cfg.Relay.Domain != "" && cfg.Relay.PublicKeyEd25519 == "" && !cfg.Relay.Insecure {
		return nil, fmt.Errorf("config: relay.public_key_ed25519 is required when relay.domain is set")
	}

	// Validate TUN bounds. MTU=0 n'est plus normalisé silencieusement : on
	// distingue "section absente" (MTU reste à 0 donc on applique le défaut
	// seulement si Name est aussi vide, signe que la section entière n'a pas
	// été décodée) et "mtu=0 explicite dans le TOML" (on refuse).
	legacyMissing := cfg.TUN.Name == "" && cfg.TUN.MTU == 0
	if legacyMissing {
		cfg.TUN.Name = "levoile0"
		cfg.TUN.MTU = 1420
	}
	if cfg.TUN.Name == "" {
		return nil, fmt.Errorf("config: tun.name requis (ex: \"levoile0\")")
	}
	if cfg.TUN.MTU == 0 {
		return nil, fmt.Errorf("config: tun.mtu requis (ex: 1420) — 0 explicite interdit")
	}
	if cfg.TUN.MTU < 576 || cfg.TUN.MTU > 9000 {
		return nil, fmt.Errorf("config: tun.mtu=%d hors bornes [576,9000]", cfg.TUN.MTU)
	}
	if !tunNameRe.MatchString(cfg.TUN.Name) {
		return nil, fmt.Errorf("config: tun.name=%q invalide (regex ^[a-z][a-z0-9]{0,14}$)", cfg.TUN.Name)
	}

	// Validate DoH upstream URLs if the operator supplied a custom list.
	// Same rules as registry.validateUpstreams: must parse, scheme=https,
	// non-empty host.
	for _, upstream := range cfg.Registry.DoHUpstreams {
		parsed, perr := url.Parse(upstream)
		if perr != nil || parsed.Scheme != "https" || parsed.Host == "" {
			return nil, fmt.Errorf("config: registry.doh_upstreams[%q] must be a valid HTTPS URL", upstream)
		}
	}

	return cfg, nil
}

// Save writes the configuration to a TOML file atomically, creating parent
// directories if necessary. It writes to a temp file first and renames on
// success to prevent corruption on crash. After the rename succeeds, a
// restrictive permission mask (0600 + 0700 dir on Unix, protected DACL on
// Windows) is re-applied — os.Rename preserves the source DACL/mode on
// Windows, so tightening must happen on the final path (NFR9j).
func (c *Config) Save(path string) error {
	_, err := c.saveBytes(path)
	return err
}

// saveBytes is the shared encode+write+tighten core used by Save and
// SaveAndSign. It returns the encoded TOML bytes so SaveAndSign can hand
// them directly to SignBytes, avoiding a re-read of the on-disk file
// between the rename and the HMAC computation. That re-read would open a
// TOCTOU window: an attacker at the same privilege level as the service
// could overwrite the file in that split second and have the legitimate
// Sign path HMAC the malicious bytes (defense in depth for NFR9j — the
// DACL/0600 perms already block most attackers).
func (c *Config) saveBytes(path string) ([]byte, error) {
	var buf bytes.Buffer
	if err := toml.NewEncoder(&buf).Encode(c); err != nil {
		return nil, fmt.Errorf("config: encode: %w", err)
	}
	encoded := buf.Bytes()

	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, fmt.Errorf("config: %w", err)
	}

	tmp, err := os.CreateTemp(dir, "config-*.tmp")
	if err != nil {
		return nil, fmt.Errorf("config: create temp: %w", err)
	}
	tmpName := tmp.Name()

	if _, err := tmp.Write(encoded); err != nil {
		tmp.Close()
		os.Remove(tmpName)
		return nil, fmt.Errorf("config: write temp: %w", err)
	}
	if err := tmp.Close(); err != nil {
		os.Remove(tmpName)
		return nil, fmt.Errorf("config: close temp: %w", err)
	}
	if err := os.Rename(tmpName, path); err != nil {
		os.Remove(tmpName)
		return nil, fmt.Errorf("config: rename: %w", err)
	}
	if err := applyRestrictedPerms(path); err != nil {
		// Do not remove the freshly-written config — losing user data on a
		// perm-tightening failure would be worse than a temporarily loose ACL.
		// Caller is expected to log the warning; the integrity HMAC covers
		// tampering even if perms drift.
		return nil, fmt.Errorf("config: tighten perms: %w", err)
	}
	return encoded, nil
}
