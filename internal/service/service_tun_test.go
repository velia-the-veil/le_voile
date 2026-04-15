package service

import (
	"errors"
	"testing"

	"github.com/velia-the-veil/le_voile/internal/tun"
)

// mockTUN implémente tun.Device pour valider l'intégration service sans
// nécessiter de privilèges OS.
type mockTUN struct {
	name     string
	mtu      int
	closed   bool
	closeErr error
}

func (m *mockTUN) Read(buf []byte) (int, error)  { return 0, nil }
func (m *mockTUN) Write(pkt []byte) (int, error) { return len(pkt), nil }
func (m *mockTUN) Name() string                  { return m.name }
func (m *mockTUN) MTU() int                      { return m.mtu }
func (m *mockTUN) Close() error {
	m.closed = true
	return m.closeErr
}

func TestEnsureTUN_Defaults(t *testing.T) {
	var gotName string
	var gotMTU int
	orig := tunFactory
	tunFactory = func(name string, mtu int) (tun.Device, error) {
		gotName = name
		gotMTU = mtu
		return &mockTUN{name: name, mtu: mtu}, nil
	}
	defer func() { tunFactory = orig }()

	p := NewProgram(Config{TUNEnabled: true})
	if err := p.ensureTUN(); err != nil {
		t.Fatalf("ensureTUN: %v", err)
	}
	if gotName != "levoile0" || gotMTU != 1420 {
		t.Errorf("factory appelée avec (%q, %d), attendu (levoile0, 1420)", gotName, gotMTU)
	}
	if p.tunDev == nil {
		t.Fatal("p.tunDev est nil après ensureTUN")
	}
}

func TestEnsureTUN_CustomNameMTU(t *testing.T) {
	orig := tunFactory
	tunFactory = func(name string, mtu int) (tun.Device, error) {
		return &mockTUN{name: name, mtu: mtu}, nil
	}
	defer func() { tunFactory = orig }()

	p := NewProgram(Config{TUNEnabled: true, TUNName: "vpn0", TUNMTU: 1280})
	if err := p.ensureTUN(); err != nil {
		t.Fatalf("ensureTUN: %v", err)
	}
	if got := p.tunDev.Name(); got != "vpn0" {
		t.Errorf("Name = %q, want vpn0", got)
	}
	if got := p.tunDev.MTU(); got != 1280 {
		t.Errorf("MTU = %d, want 1280", got)
	}
}

func TestEnsureTUN_FactoryError(t *testing.T) {
	wantErr := errors.New("mock tun failure")
	orig := tunFactory
	tunFactory = func(name string, mtu int) (tun.Device, error) {
		return nil, wantErr
	}
	defer func() { tunFactory = orig }()

	p := NewProgram(Config{TUNEnabled: true})
	err := p.ensureTUN()
	if err == nil {
		t.Fatal("ensureTUN doit propager l'erreur du factory")
	}
	if !errors.Is(err, wantErr) {
		t.Errorf("err = %v, attendu wrap de %v", err, wantErr)
	}
	if p.tunDev != nil {
		t.Error("tunDev doit rester nil après erreur")
	}
}

func TestEnsureTUN_DisabledByDefault(t *testing.T) {
	// Défaut Config{} → TUNEnabled=false → le run() skip ensureTUN. On vérifie
	// ici que tunDev reste nil sans appeler ensureTUN, en simulant un run
	// partiel.
	p := NewProgram(Config{})
	if p.config.TUNEnabled {
		t.Error("TUNEnabled doit être false par défaut")
	}
	if p.tunDev != nil {
		t.Error("tunDev doit être nil avant toute initialisation")
	}
}
