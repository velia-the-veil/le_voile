//go:build linux

package service

import (
	"context"
	"net"
	"sync"
	"testing"

	"github.com/velia-the-veil/le_voile/linux/internal/firewall"
	"github.com/velia-the-veil/le_voile/linux/internal/routing"
	"github.com/velia-the-veil/le_voile/linux/internal/tun"
)

// callRecord enregistre l'ordre d'exécution des mocks pour valider la séquence
// stricte exigée par AC2 : tun.New → routing.Setup → firewall.Activate.
type callRecord struct {
	mu    sync.Mutex
	calls []string
}

func (r *callRecord) add(name string) {
	r.mu.Lock()
	r.calls = append(r.calls, name)
	r.mu.Unlock()
}

func (r *callRecord) snapshot() []string {
	r.mu.Lock()
	out := make([]string, len(r.calls))
	copy(out, r.calls)
	r.mu.Unlock()
	return out
}

// stubFirewall implémente firewall.Firewall avec enregistrement des appels.
type stubFirewall struct {
	rec       *callRecord
	activated bool
}

func (f *stubFirewall) Activate(_ context.Context, _ firewall.ActivateParams) error {
	f.rec.add("firewall.Activate")
	f.activated = true
	return nil
}
func (f *stubFirewall) Deactivate(_ context.Context) error {
	f.rec.add("firewall.Deactivate")
	f.activated = false
	return nil
}
func (f *stubFirewall) IsActive(_ context.Context) (bool, error)      { return f.activated, nil }
func (f *stubFirewall) SetIPv6Policy(_ context.Context, _ bool) error { return nil }
func (f *stubFirewall) CleanupOrphans(_ context.Context) (int, error) { return 0, nil }
func (f *stubFirewall) AlteredCh() <-chan struct{}                    { return nil }

// stubRouteManager implémente routing.RouteManager avec enregistrement.
type stubRouteManager struct {
	rec    *callRecord
	active bool
}

func (r *stubRouteManager) Setup(tunName string, relayIP net.IP, origGW net.IP, origIface string) error {
	r.rec.add("routing.Setup")
	r.active = true
	return nil
}
func (r *stubRouteManager) Teardown() error {
	r.rec.add("routing.Teardown")
	r.active = false
	return nil
}
func (r *stubRouteManager) Cleanup() error { return nil }
func (r *stubRouteManager) Saved() *routing.SavedRoutes { return nil }

// TestRecoverTUN_OrderStrict vérifie que recoverTUN exécute la séquence AC2 :
// tun.New → routing.Setup → firewall.Activate, et que firewall.Deactivate
// n'est JAMAIS appelé (AC3 — kill switch maintenu pendant recovery).
func TestRecoverTUN_OrderStrict(t *testing.T) {
	rec := &callRecord{}
	relayIP := net.IPv4(1, 2, 3, 4)

	// Injecter le mock TUN factory.
	origTun := tunFactory
	tunFactory = func(name string, mtu int) (tun.Device, error) {
		rec.add("tun.New")
		return &mockTUN{name: name, mtu: mtu}, nil
	}
	defer func() { tunFactory = origTun }()

	// Injecter le mock routing factory.
	stubRM := &stubRouteManager{rec: rec}
	origRouting := routingFactory
	routingFactory = func() routing.RouteManager { return stubRM }
	defer func() { routingFactory = origRouting }()

	// Injecter le mock firewall factory.
	stubFW := &stubFirewall{rec: rec}
	origFW := firewallFactory
	firewallFactory = func(_ firewall.Logger, _ firewall.Options) firewall.Firewall { return stubFW }
	defer func() { firewallFactory = origFW }()

	// Injecter CaptureOriginalRoute pour éviter l'appel système réel.
	origCapture := captureOriginalRouteFunc
	captureOriginalRouteFunc = func() (net.IP, string, error) {
		return net.IPv4(192, 168, 1, 1), "eth0", nil
	}
	defer func() { captureOriginalRouteFunc = origCapture }()

	// Construire le Program avec TUN et firewall activés.
	p := NewProgram(Config{
		TUNEnabled:      true,
		FirewallEnabled: true,
	})
	p.tunDev = &mockTUN{name: "levoile0", mtu: 1420}
	p.firewallRelayIP.Store(relayIP)
	// Pas de tunnelClient — recoverTUN skip le tunnel.Connect si nil.

	ctx := context.Background()
	if err := p.recoverTUN(ctx); err != nil {
		t.Fatalf("recoverTUN: %v", err)
	}

	calls := rec.snapshot()

	// Ordre strict post-fix C5 (audit sécurité 2026-04) : le firewall est
	// réactivé AVANT le routing setup. Raison : si routing envoie des
	// paquets vers le nouveau TUN avant que le firewall ne le gouverne,
	// une fenêtre microscopique de non-couverture existe. Activer le
	// firewall d'abord ferme cette fenêtre — flush+replace atomique
	// nftables/WFP garantit zéro downtime.
	wantOrder := []string{"tun.New", "firewall.Activate", "routing.Setup"}
	if len(calls) < len(wantOrder) {
		t.Fatalf("appels = %v, attendu au moins %v", calls, wantOrder)
	}
	for i, want := range wantOrder {
		if calls[i] != want {
			t.Errorf("appel[%d] = %q, want %q (séquence complète: %v)", i, calls[i], want, calls)
		}
	}

	// AC3 : firewall.Deactivate ne doit JAMAIS apparaître.
	for _, c := range calls {
		if c == "firewall.Deactivate" {
			t.Error("firewall.Deactivate appelé pendant recovery — viole AC3 (kill switch maintenu)")
		}
	}

	// Vérifier que tunDev a été recréé.
	if p.tunDev == nil {
		t.Error("tunDev est nil après recovery")
	}
}

// TestRecoverTUN_TunFactoryError vérifie que recoverTUN retourne proprement
// si tun.New échoue, sans toucher routing ni firewall.
func TestRecoverTUN_TunFactoryError(t *testing.T) {
	rec := &callRecord{}

	origTun := tunFactory
	tunFactory = func(name string, mtu int) (tun.Device, error) {
		rec.add("tun.New")
		return nil, tun.ErrPermission
	}
	defer func() { tunFactory = origTun }()

	p := NewProgram(Config{TUNEnabled: true, FirewallEnabled: true})
	p.tunDev = &mockTUN{name: "levoile0", mtu: 1420}
	p.firewallRelayIP.Store(net.IPv4(1, 2, 3, 4))

	err := p.recoverTUN(context.Background())
	if err == nil {
		t.Fatal("recoverTUN doit retourner une erreur si tun.New échoue")
	}
	calls := rec.snapshot()
	if len(calls) != 1 || calls[0] != "tun.New" {
		t.Errorf("appels = %v, attendu uniquement [tun.New]", calls)
	}
}

// TestRecoverTUN_NoFirewall vérifie que recoverTUN fonctionne quand
// FirewallEnabled=false (mode dégradé, pas de kill switch).
func TestRecoverTUN_NoFirewall(t *testing.T) {
	rec := &callRecord{}

	origTun := tunFactory
	tunFactory = func(name string, mtu int) (tun.Device, error) {
		rec.add("tun.New")
		return &mockTUN{name: name, mtu: mtu}, nil
	}
	defer func() { tunFactory = origTun }()

	stubRM := &stubRouteManager{rec: rec}
	origRouting := routingFactory
	routingFactory = func() routing.RouteManager { return stubRM }
	defer func() { routingFactory = origRouting }()

	origCapture := captureOriginalRouteFunc
	captureOriginalRouteFunc = func() (net.IP, string, error) {
		return net.IPv4(192, 168, 1, 1), "eth0", nil
	}
	defer func() { captureOriginalRouteFunc = origCapture }()

	p := NewProgram(Config{TUNEnabled: true, FirewallEnabled: false})
	p.tunDev = &mockTUN{name: "levoile0", mtu: 1420}
	p.firewallRelayIP.Store(net.IPv4(1, 2, 3, 4))

	if err := p.recoverTUN(context.Background()); err != nil {
		t.Fatalf("recoverTUN: %v", err)
	}

	calls := rec.snapshot()
	for _, c := range calls {
		if c == "firewall.Activate" {
			t.Error("firewall.Activate ne devrait pas être appelé quand FirewallEnabled=false")
		}
	}
}
