// Package config handles application configuration loading and management.
package config

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/BurntSushi/toml"
)

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
	AutoStart bool `toml:"auto_start"`
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
	}

	if _, err := toml.DecodeFile(path, cfg); err != nil {
		if os.IsNotExist(err) {
			return cfg, nil
		}
		return nil, fmt.Errorf("config: %w", err)
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
