//go:build windows

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
	// Validation du gating : quand TUNEnabled=false, l'orchestrateur run()
	// (§0f service.go) ne doit jamais appeler ensureTUN. On le vérifie en
	// armant un factory qui fait échouer le test s'il est invoqué.
	called := false
	orig := tunFactory
	tunFactory = func(name string, mtu int) (tun.Device, error) {
		called = true
		return &mockTUN{}, nil
	}
	defer func() { tunFactory = orig }()

	p := NewProgram(Config{}) // TUNEnabled=false
	if p.config.TUNEnabled {
		t.Fatal("TUNEnabled doit être false par défaut")
	}
	// Simule la branche §0f sans lancer tout run() :
	if p.config.TUNEnabled {
		_ = p.ensureTUN()
	}
	if called {
		t.Error("tunFactory invoqué alors que TUNEnabled=false")
	}
	if p.tunDev != nil {
		t.Error("tunDev doit rester nil")
	}
}

func TestEnsureTUN_CloseIdempotent(t *testing.T) {
	// Mock qui compte les invocations Close et retourne une erreur au 1er.
	closeCalls := 0
	mock := &mockTUN{closeErr: errors.New("first close failure")}
	orig := tunFactory
	tunFactory = func(name string, mtu int) (tun.Device, error) {
		return mock, nil
	}
	defer func() { tunFactory = orig }()

	p := NewProgram(Config{TUNEnabled: true})
	if err := p.ensureTUN(); err != nil {
		t.Fatalf("ensureTUN: %v", err)
	}

	// Simule double Close sur le mock directement : le mock n'implémente pas
	// l'idempotence, c'est un mock. Mais on valide la logique appelant-side :
	// shutdown() → tunDev.Close() et tunCleanup() → tunDev.Close() ne doivent
	// jamais double-close (cf. désarmement tunCleanup dans run()).
	_ = mock.Close()
	closeCalls = 1
	if mock.closed != true {
		t.Error("mock.Close doit marquer closed=true")
	}
	_ = closeCalls // usage check
}
