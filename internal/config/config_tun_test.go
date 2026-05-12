package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestConfig_TUNDefaults(t *testing.T) {
	dir := t.TempDir()
	cfg, err := Load(filepath.Join(dir, "nonexistent.toml"))
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.TUN.Name != "levoile0" {
		t.Errorf("TUN.Name = %q, want levoile0", cfg.TUN.Name)
	}
	if cfg.TUN.MTU != 1420 {
		t.Errorf("TUN.MTU = %d, want 1420", cfg.TUN.MTU)
	}
	if cfg.TUN.Enabled {
		t.Error("TUN.Enabled doit être false par défaut")
	}
}

func TestConfig_TUNExplicitMTU0Rejected(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "bad.toml")
	if err := os.WriteFile(path, []byte(`[tun]
name = "levoile0"
mtu = 0
`), 0o600); err != nil {
		t.Fatal(err)
	}
	_, err := Load(path)
	if err == nil {
		t.Fatal("mtu=0 explicite doit être refusé")
	}
	if !strings.Contains(err.Error(), "mtu requis") {
		t.Errorf("erreur inattendue: %v", err)
	}
}

func TestConfig_TUNEnabledFromTOML(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "on.toml")
	if err := os.WriteFile(path, []byte(`[tun]
enabled = true
name = "levoile0"
mtu = 1420
`), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if !cfg.TUN.Enabled {
		t.Error("TUN.Enabled = false, attendu true")
	}
}

func TestConfig_TUNLegacyConfigNormalized(t *testing.T) {
	// Config existante sans section [tun] → valeurs par défaut appliquées.
	dir := t.TempDir()
	path := filepath.Join(dir, "legacy.toml")
	if err := os.WriteFile(path, []byte(`[client]
auto_start = true
`), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load legacy: %v", err)
	}
	if cfg.TUN.Name != "levoile0" || cfg.TUN.MTU != 1420 {
		t.Errorf("legacy config → TUN=%+v, attendu défauts", cfg.TUN)
	}
}

func TestConfig_TUNInvalidMTU(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "bad.toml")
	if err := os.WriteFile(path, []byte(`[tun]
name = "levoile0"
mtu = 100
`), 0o600); err != nil {
		t.Fatal(err)
	}
	_, err := Load(path)
	if err == nil {
		t.Fatal("Load doit échouer pour MTU=100")
	}
	if !strings.Contains(err.Error(), "hors bornes") {
		t.Errorf("erreur inattendue: %v", err)
	}
}

func TestConfig_TUNInvalidName(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "bad.toml")
	if err := os.WriteFile(path, []byte(`[tun]
name = "LE-VOILE"
mtu = 1420
`), 0o600); err != nil {
		t.Fatal(err)
	}
	_, err := Load(path)
	if err == nil {
		t.Fatal("Load doit échouer pour nom invalide")
	}
	if !strings.Contains(err.Error(), "invalide") {
		t.Errorf("erreur inattendue: %v", err)
	}
}

func TestConfig_TUNCustomMTU(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "ok.toml")
	if err := os.WriteFile(path, []byte(`[tun]
name = "vpn0"
mtu = 1280
`), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.TUN.Name != "vpn0" || cfg.TUN.MTU != 1280 {
		t.Errorf("TUN = %+v, attendu {vpn0 1280}", cfg.TUN)
	}
}
