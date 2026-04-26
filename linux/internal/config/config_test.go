//go:build linux

package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestConfig_LoadDefaults(t *testing.T) {
	cfg, err := Load(filepath.Join(t.TempDir(), "nonexistent.toml"))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !cfg.Client.AutoStart {
		t.Error("expected AutoStart to default to true")
	}
	if cfg.Relay.Domain != "" {
		t.Errorf("expected empty domain, got %q", cfg.Relay.Domain)
	}
	if want := "stun.l.google.com:19302"; cfg.STUN.DefaultServer != want {
		t.Errorf("expected default STUN server %q, got %q", want, cfg.STUN.DefaultServer)
	}
	if cfg.STUN.LeakcheckInterval != "" {
		t.Errorf("expected empty LeakcheckInterval by default, got %q", cfg.STUN.LeakcheckInterval)
	}
	if len(cfg.STUN.Servers) != 0 {
		t.Errorf("expected empty Servers by default, got %v", cfg.STUN.Servers)
	}
}

func TestConfig_STUN_LeakcheckInterval_Roundtrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.toml")
	original := &Config{
		Client: ClientConfig{AutoStart: true},
		TUN:    TUNConfig{Name: "levoile0", MTU: 1420},
		Firewall: FirewallConfig{EnableKillSwitch: true},
		STUN: STUNConfig{
			DefaultServer:     "stun.example.com:3478",
			Servers:           []string{"stun.a.com:3478", "stun.b.com:3478"},
			LeakcheckInterval: "5m",
		},
	}
	if err := original.Save(path); err != nil {
		t.Fatalf("save: %v", err)
	}
	loaded, err := Load(path)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if loaded.STUN.LeakcheckInterval != "5m" {
		t.Errorf("interval mismatch: got %q", loaded.STUN.LeakcheckInterval)
	}
	if len(loaded.STUN.Servers) != 2 || loaded.STUN.Servers[0] != "stun.a.com:3478" {
		t.Errorf("servers mismatch: got %v", loaded.STUN.Servers)
	}
}

func TestConfig_SaveRoundtrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "subdir", "config.toml")

	original := &Config{
		Relay: RelayConfig{
			Domain:           "levoile.dev",
			PublicKeyEd25519: "dGVzdGtleQ==",
		},
		Client: ClientConfig{
			AutoStart: false,
		},
		STUN: STUNConfig{
			DefaultServer: "stun.example.com:3478",
		},
	}

	if err := original.Save(path); err != nil {
		t.Fatalf("save error: %v", err)
	}

	loaded, err := Load(path)
	if err != nil {
		t.Fatalf("load error: %v", err)
	}

	if loaded.Relay.Domain != original.Relay.Domain {
		t.Errorf("domain mismatch: got %q, want %q", loaded.Relay.Domain, original.Relay.Domain)
	}
	if loaded.Relay.PublicKeyEd25519 != original.Relay.PublicKeyEd25519 {
		t.Errorf("pubkey mismatch: got %q, want %q", loaded.Relay.PublicKeyEd25519, original.Relay.PublicKeyEd25519)
	}
	if loaded.Client.AutoStart != original.Client.AutoStart {
		t.Errorf("auto_start mismatch: got %v, want %v", loaded.Client.AutoStart, original.Client.AutoStart)
	}
	if loaded.STUN.DefaultServer != original.STUN.DefaultServer {
		t.Errorf("stun default_server mismatch: got %q, want %q", loaded.STUN.DefaultServer, original.STUN.DefaultServer)
	}
}

func TestConfig_LoadInvalidFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "bad.toml")
	if err := os.WriteFile(path, []byte("{{invalid toml"), 0o644); err != nil {
		t.Fatal(err)
	}

	_, err := Load(path)
	if err == nil {
		t.Error("expected error for invalid TOML file")
	}
}

func TestConfig_RegistryDefaults(t *testing.T) {
	cfg, err := Load(filepath.Join(t.TempDir(), "nonexistent.toml"))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.Registry.Enabled {
		t.Error("expected Registry.Enabled to default to false")
	}
	if want := "https://levoile.dev"; cfg.Registry.URL != want {
		t.Errorf("Registry.URL: got %q, want %q", cfg.Registry.URL, want)
	}
	// AC Story 4.3 — refresh par défaut à 6h (auparavant "1h").
	if want := "6h"; cfg.Registry.RefreshInterval != want {
		t.Errorf("Registry.RefreshInterval: got %q, want %q", cfg.Registry.RefreshInterval, want)
	}
	if !cfg.Registry.BootstrapDoHEnabled {
		t.Error("expected Registry.BootstrapDoHEnabled to default to true (NFR9i)")
	}
	if len(cfg.Registry.DoHUpstreams) != 0 {
		t.Errorf("expected Registry.DoHUpstreams to default empty (built-ins), got %v", cfg.Registry.DoHUpstreams)
	}
}

func TestConfig_RegistryDoHUpstreams_RejectsHTTP(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.toml")
	tomlBody := `
[tun]
name = "levoile0"
mtu = 1420

[registry]
enabled = true
url = "https://relay.levoile.dev"
master_public_key = "rjgDdexo2SOeNXhdy0fUKAONXeQGAyN2d3ixCeuXOpk="
bootstrap_doh_enabled = true
doh_upstreams = ["http://evil.example.com/dns-query"]
`
	if err := os.WriteFile(path, []byte(tomlBody), 0o600); err != nil {
		t.Fatal(err)
	}
	_, err := Load(path)
	if err == nil {
		t.Fatal("expected Load to reject HTTP DoH upstream")
	}
}

func TestConfig_RegistryDoHUpstreams_AcceptsHTTPS(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.toml")
	tomlBody := `
[tun]
name = "levoile0"
mtu = 1420

[registry]
enabled = true
url = "https://relay.levoile.dev"
master_public_key = "rjgDdexo2SOeNXhdy0fUKAONXeQGAyN2d3ixCeuXOpk="
doh_upstreams = ["https://cloudflare-dns.com/dns-query", "https://dns.quad9.net/dns-query"]
`
	if err := os.WriteFile(path, []byte(tomlBody), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if got := len(cfg.Registry.DoHUpstreams); got != 2 {
		t.Fatalf("DoHUpstreams count: got %d, want 2", got)
	}
}

func TestConfig_UpdateDefaults(t *testing.T) {
	cfg, err := Load(filepath.Join(t.TempDir(), "nonexistent.toml"))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !cfg.Update.Enabled {
		t.Error("expected Update.Enabled to default to true")
	}
	if want := "6h"; cfg.Update.CheckInterval != want {
		t.Errorf("Update.CheckInterval: got %q, want %q", cfg.Update.CheckInterval, want)
	}
	if want := 500; cfg.Update.RateLimitKBps != want {
		t.Errorf("Update.RateLimitKBps: got %d, want %d", cfg.Update.RateLimitKBps, want)
	}
	if want := "velia-the-veil"; cfg.Update.GitHubOwner != want {
		t.Errorf("Update.GitHubOwner: got %q, want %q", cfg.Update.GitHubOwner, want)
	}
	if want := "le_voile"; cfg.Update.GitHubRepo != want {
		t.Errorf("Update.GitHubRepo: got %q, want %q", cfg.Update.GitHubRepo, want)
	}
}

func TestConfig_DefaultPath(t *testing.T) {
	p, err := DefaultPath()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if p == "" {
		t.Error("expected non-empty default path")
	}
}

func TestConfig_SavePreferredCountry(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.toml")

	original := &Config{
		Client: ClientConfig{
			AutoStart:        true,
			PreferredCountry: "is",
		},
	}

	if err := original.Save(path); err != nil {
		t.Fatalf("save error: %v", err)
	}

	loaded, err := Load(path)
	if err != nil {
		t.Fatalf("load error: %v", err)
	}

	if loaded.Client.PreferredCountry != "is" {
		t.Errorf("PreferredCountry: got %q, want %q", loaded.Client.PreferredCountry, "is")
	}
}

func TestConfig_PreferredCountryDefaultEmpty(t *testing.T) {
	cfg, err := Load(filepath.Join(t.TempDir(), "nonexistent.toml"))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.Client.PreferredCountry != "" {
		t.Errorf("expected PreferredCountry to default to empty, got %q", cfg.Client.PreferredCountry)
	}
}

func TestConfig_SkipQuitModal(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.toml")

	original := &Config{
		Client: ClientConfig{
			AutoStart:     true,
			SkipQuitModal: true,
		},
	}

	if err := original.Save(path); err != nil {
		t.Fatalf("save error: %v", err)
	}

	loaded, err := Load(path)
	if err != nil {
		t.Fatalf("load error: %v", err)
	}

	if !loaded.Client.SkipQuitModal {
		t.Error("expected SkipQuitModal = true after roundtrip")
	}
}

func TestConfig_SkipQuitModalDefaultFalse(t *testing.T) {
	cfg, err := Load(filepath.Join(t.TempDir(), "nonexistent.toml"))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.Client.SkipQuitModal {
		t.Error("expected SkipQuitModal to default to false")
	}
}

func TestConfig_AllowIPv6LeakDefaultFalse(t *testing.T) {
	cfg, err := Load(filepath.Join(t.TempDir(), "nonexistent.toml"))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.Firewall.AllowIPv6Leak {
		t.Error("expected AllowIPv6Leak to default to false")
	}
}

func TestConfig_AllowIPv6LeakRoundtrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.toml")

	original := &Config{
		Firewall: FirewallConfig{
			EnableKillSwitch: true,
			AllowIPv6Leak:    true,
		},
		TUN: TUNConfig{Name: "levoile0", MTU: 1420},
	}

	if err := original.Save(path); err != nil {
		t.Fatalf("save error: %v", err)
	}

	loaded, err := Load(path)
	if err != nil {
		t.Fatalf("load error: %v", err)
	}

	if !loaded.Firewall.AllowIPv6Leak {
		t.Error("expected AllowIPv6Leak = true after roundtrip")
	}
}

func TestConfig_AllowIPv6LeakFromTOML(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.toml")
	content := `[tun]
name = "levoile0"
mtu = 1420

[firewall]
enable_killswitch = true
allow_ipv6_leak = true
`
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}

	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("load error: %v", err)
	}
	if !cfg.Firewall.AllowIPv6Leak {
		t.Error("expected AllowIPv6Leak = true from TOML")
	}
	if !cfg.Firewall.EnableKillSwitch {
		t.Error("expected EnableKillSwitch = true from TOML")
	}
}

func TestStagingDir(t *testing.T) {
	dir, err := StagingDir()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if dir == "" {
		t.Fatal("expected non-empty staging directory")
	}
	// On Windows the path should contain "LeVoile"; on Unix "levoile".
	if !strings.Contains(dir, "LeVoile") && !strings.Contains(dir, "levoile") {
		t.Errorf("expected path to contain app directory name, got %q", dir)
	}
	if !strings.HasSuffix(dir, "updates") {
		t.Errorf("expected path to end with %q, got %q", "updates", dir)
	}
}
