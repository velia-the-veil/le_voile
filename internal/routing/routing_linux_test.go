//go:build linux

package routing

import (
	"errors"
	"net"
	"os"
	"testing"
)

func TestLinuxNew(t *testing.T) {
	rm := New()
	if rm == nil {
		t.Fatal("New() returned nil")
	}
	if rm.Saved() != nil {
		t.Error("Saved() should be nil before Setup")
	}
}

func TestLinuxSetup_NilGateway(t *testing.T) {
	rm := New()
	err := rm.Setup("levoile0", net.ParseIP("10.0.0.1"), nil, "eth0")
	if !errors.Is(err, ErrGatewayResolve) {
		t.Fatalf("Setup(nil gw) = %v, want ErrGatewayResolve", err)
	}
}

func TestLinuxSetup_IPv6Rejected(t *testing.T) {
	rm := New()
	err := rm.Setup("levoile0", net.ParseIP("::1"), net.ParseIP("192.168.1.1"), "eth0")
	if err == nil {
		t.Fatal("Setup with IPv6 relayIP should fail")
	}
}

func TestLinuxTeardown_Idempotent(t *testing.T) {
	rm := New()
	// Teardown without Setup should not error (idempotent).
	if err := rm.Teardown(); err != nil {
		t.Fatalf("Teardown without Setup: %v", err)
	}
}

// TestLinuxSetupTeardown_Real exécute un cycle Setup/Teardown réel.
// Nécessite CAP_NET_ADMIN (root). Skipé automatiquement sinon.
func TestLinuxSetupTeardown_Real(t *testing.T) {
	if os.Geteuid() != 0 {
		t.Skip("requires root (CAP_NET_ADMIN)")
	}

	rm := New()

	// Capturer gateway + interface en un seul appel atomique.
	gw, iface, err := CaptureOriginalRoute()
	if err != nil {
		t.Skipf("cannot determine default route: %v", err)
	}
	t.Logf("detected gateway: %s via %s", gw, iface)

	// Utiliser une IP dummy comme relais.
	relay := net.ParseIP("198.51.100.1")

	// Créer une interface TUN pour tester le routage.
	if err := ipCmd("tuntap", "add", "dev", "levoile0", "mode", "tun"); err != nil {
		t.Skipf("cannot create TUN: %v", err)
	}
	if err := ipCmd("link", "set", "levoile0", "up"); err != nil {
		_ = ipCmd("link", "del", "levoile0")
		t.Skipf("cannot bring up TUN: %v", err)
	}
	defer func() { _ = ipCmd("link", "del", "levoile0") }()

	if err := rm.Setup("levoile0", relay, gw, iface); err != nil {
		t.Fatalf("Setup: %v", err)
	}

	saved := rm.Saved()
	if saved == nil {
		t.Fatal("Saved() nil after Setup")
	}
	if !saved.OrigGateway.Equal(gw) {
		t.Errorf("OrigGateway = %v, want %v", saved.OrigGateway, gw)
	}

	// Double Setup should return ErrAlreadyActive.
	if err := rm.Setup("levoile0", relay, gw, iface); !errors.Is(err, ErrAlreadyActive) {
		t.Errorf("double Setup = %v, want ErrAlreadyActive", err)
	}

	if err := rm.Teardown(); err != nil {
		t.Fatalf("Teardown: %v", err)
	}

	if rm.Saved() != nil {
		t.Error("Saved() should be nil after Teardown")
	}
}

// TestLinuxCleanup_Real vérifie que Cleanup purge les résidus.
func TestLinuxCleanup_Real(t *testing.T) {
	if os.Geteuid() != 0 {
		t.Skip("requires root (CAP_NET_ADMIN)")
	}

	rm := New()
	if err := rm.Cleanup(); err != nil {
		t.Fatalf("Cleanup: %v", err)
	}
}

