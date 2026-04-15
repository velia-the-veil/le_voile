// Package config handles application configuration loading and management.
package config

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"

	"github.com/BurntSushi/toml"
)

var tunNameRe = regexp.MustCompile(`^[a-z][a-z0-9]{0,14}$`)

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
}

// TUNConfig holds TUN/Wintun interface settings (Epic 2 — capture L3).
type TUNConfig struct {
	Name string `toml:"name"` // ex: "levoile0" — regex ^[a-z][a-z0-9]{0,14}$
	MTU  int    `toml:"mtu"`  // bornes [576, 9000], défaut 1420
}

// BrowserPoliciesConfig holds browser WebRTC policy settings.
type BrowserPoliciesConfig struct {
	Enabled bool `toml:"enabled"`
	// ChromeStoreUpdateURL overrides the local file:// update URL for Chrome extension
	// installation. Required for non-managed devices (no Active Directory domain).
	// Set to "https://clients2.google.com/service/update2/crx" for CWS-hosted extensions.
	ChromeStoreUpdateURL string `toml:"chrome_store_update_url,omitempty"`
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
}

// UpdateConfig holds auto-update settings.
type UpdateConfig struct {
	Enabled       bool   `toml:"enabled"`
	CheckInterval string `toml:"check_interval"`
	RateLimitKBps int    `toml:"rate_limit_kbps"`
	GitHubOwner   string `toml:"github_owner"`
	GitHubRepo    string `toml:"github_repo"`
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

// STUNConfig holds STUN relay configuration.
type STUNConfig struct {
	DefaultServer string `toml:"default_server"`
}

// Load reads a TOML configuration file. If the file does not exist,
// it returns a default configuration.
func Load(path string) (*Config, error) {
	cfg := &Config{
		Client: ClientConfig{AutoStart: true},
		STUN:   STUNConfig{DefaultServer: "stun.l.google.com:19302"},
		Update: UpdateConfig{
			Enabled:       true,
			CheckInterval: "6h",
			RateLimitKBps: 512,
			GitHubOwner:   "velia-the-veil",
			GitHubRepo:    "le_voile",
		},
		Blocklist: BlocklistConfig{
			Enabled:        false,
			UpdateInterval: "24h",
		},
		Registry: RegistryConfig{
			Enabled:         false,
			URL:             "https://levoile.dev",
			RefreshInterval: "1h",
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

	// Validate TUN bounds (MTU may be 0 in legacy configs → normalize to defaults).
	if cfg.TUN.Name == "" {
		cfg.TUN.Name = "levoile0"
	}
	if cfg.TUN.MTU == 0 {
		cfg.TUN.MTU = 1420
	}
	if cfg.TUN.MTU < 576 || cfg.TUN.MTU > 9000 {
		return nil, fmt.Errorf("config: tun.mtu=%d hors bornes [576,9000]", cfg.TUN.MTU)
	}
	if !tunNameRe.MatchString(cfg.TUN.Name) {
		return nil, fmt.Errorf("config: tun.name=%q invalide (regex ^[a-z][a-z0-9]{0,14}$)", cfg.TUN.Name)
	}

	return cfg, nil
}

// Save writes the configuration to a TOML file atomically, creating parent
// directories if necessary. It writes to a temp file first and renames on
// success to prevent corruption on crash.
func (c *Config) Save(path string) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return fmt.Errorf("config: %w", err)
	}

	tmp, err := os.CreateTemp(dir, "config-*.tmp")
	if err != nil {
		return fmt.Errorf("config: create temp: %w", err)
	}
	tmpName := tmp.Name()

	if err := toml.NewEncoder(tmp).Encode(c); err != nil {
		tmp.Close()
		os.Remove(tmpName)
		return fmt.Errorf("config: encode: %w", err)
	}
	if err := tmp.Close(); err != nil {
		os.Remove(tmpName)
		return fmt.Errorf("config: close temp: %w", err)
	}
	if err := os.Rename(tmpName, path); err != nil {
		os.Remove(tmpName)
		return fmt.Errorf("config: rename: %w", err)
	}
	return nil
}
