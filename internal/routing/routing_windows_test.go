//go:build windows

package routing

import (
	"errors"
	"net"
	"strings"
	"testing"

	"golang.org/x/sys/windows"
)

func TestWindowsNew(t *testing.T) {
	rm := New()
	if rm == nil {
		t.Fatal("New() returned nil")
	}
	if rm.Saved() != nil {
		t.Error("Saved() should be nil before Setup")
	}
}

func TestWindowsSetup_NilGateway(t *testing.T) {
	rm := New()
	err := rm.Setup("levoile0", net.ParseIP("10.0.0.1"), nil, "Ethernet")
	if !errors.Is(err, ErrGatewayResolve) {
		t.Fatalf("Setup(nil gw) = %v, want ErrGatewayResolve", err)
	}
}

func TestWindowsSetup_IPv6Rejected(t *testing.T) {
	rm := New()
	err := rm.Setup("levoile0", net.ParseIP("::1"), net.ParseIP("192.168.1.1"), "Ethernet")
	if err == nil {
		t.Fatal("Setup with IPv6 relayIP should fail")
	}
}

func TestWindowsSetup_EmptyIface(t *testing.T) {
	rm := New()
	err := rm.Setup("levoile0", net.ParseIP("10.0.0.1"), net.ParseIP("192.168.1.1"), "")
	if err == nil {
		t.Fatal("Setup with empty origIface should fail")
	}
}

func TestWindowsTeardown_Idempotent(t *testing.T) {
	rm := New()
	if err := rm.Teardown(); err != nil {
		t.Fatalf("Teardown without Setup: %v", err)
	}
}

func TestWindowsCleanup_NoError(t *testing.T) {
	rm := New()
	if err := rm.Cleanup(); err != nil {
		t.Fatalf("Cleanup: %v", err)
	}
}

func TestWindowsCaptureOriginalRoute(t *testing.T) {
	gw, iface, err := CaptureOriginalRoute()
	if err != nil {
		t.Skipf("cannot capture original route: %v", err)
	}
	if gw == nil {
		t.Error("gateway is nil")
	}
	if iface == "" {
		t.Error("iface is empty")
	}
	t.Logf("detected: gateway=%s iface=%s", gw, iface)
}

// isAdmin vérifie si le processus tourne avec des privilèges élevés.
func isAdmin() bool {
	var sid *windows.SID
	err := windows.AllocateAndInitializeSid(
		&windows.SECURITY_NT_AUTHORITY,
		2,
		windows.SECURITY_BUILTIN_DOMAIN_RID,
		windows.DOMAIN_ALIAS_RID_ADMINS,
		0, 0, 0, 0, 0, 0,
		&sid,
	)
	if err != nil {
		return false
	}
	defer windows.FreeSid(sid)
	token := windows.Token(0)
	member, err := token.IsMember(sid)
	if err != nil {
		return false
	}
	return member
}

// TestWindowsSetupTeardown_Real nécessite des privilèges admin et une
// interface levoile0 existante. Skipé sinon.
func TestWindowsSetupTeardown_Real(t *testing.T) {
	if !isAdmin() {
		t.Skip("requires admin privileges")
	}

	// Capturer gateway + interface en un seul appel atomique.
	gw, origIface, err := CaptureOriginalRoute()
	if err != nil {
		t.Skipf("cannot capture original route: %v", err)
	}
	t.Logf("detected: gateway=%s iface=%s", gw, origIface)

	// Vérifier que l'interface Wintun levoile0 existe.
	out, err := hiddenCmd("netsh", "interface", "show", "interface", "levoile0")
	if err != nil {
		t.Skipf("interface levoile0 not found: %v: %s", err, out)
	}

	relay := net.ParseIP("198.51.100.1")
	rm := New()

	if err := rm.Setup("levoile0", relay, gw, origIface); err != nil {
		t.Fatalf("Setup: %v", err)
	}

	saved := rm.Saved()
	if saved == nil {
		t.Fatal("Saved() nil after Setup")
	}

	if err := rm.Setup("levoile0", relay, gw, origIface); !errors.Is(err, ErrAlreadyActive) {
		t.Errorf("double Setup = %v, want ErrAlreadyActive", err)
	}

	if err := rm.Teardown(); err != nil {
		t.Fatalf("Teardown: %v", err)
	}
}

func TestIsRouteNotFound(t *testing.T) {
	t.Parallel()
	cases := []struct {
		msg  string
		want bool
	}{
		{"routing: netsh delete route: exit status 1: Element not found.", true},
		{"routing: netsh delete route: exit status 1: Élément introuvable.", true},
		{"routing: netsh delete route: exit status 1: The filename, directory name, or volume label syntax is incorrect.", false},
	}
	for _, tc := range cases {
		if got := isRouteNotFound(errors.New(tc.msg)); got != tc.want {
			t.Errorf("isRouteNotFound(%q) = %v, want %v", tc.msg, got, tc.want)
		}
	}
}

func TestExtractIfIdx(t *testing.T) {
	t.Parallel()
	line := "Non      Manuel    0    0.0.0.0/0                   6  192.168.1.1"
	idx := extractIfIdx(line)
	if idx != "6" {
		t.Errorf("extractIfIdx = %q, want 6", idx)
	}
}

func TestIfIdxToName(t *testing.T) {
	name, err := ifIdxToName("1")
	if err != nil {
		t.Skipf("cannot resolve idx 1: %v", err)
	}
	if !strings.Contains(name, "Loopback") {
		t.Logf("idx 1 resolved to %q (expected Loopback on most systems)", name)
	}
}
