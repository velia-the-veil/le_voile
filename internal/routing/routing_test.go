package routing

import (
	"errors"
	"net"
	"testing"
)

// mockRouteManager is a minimal mock implementing RouteManager for
// contract-level tests that don't need OS privileges.
type mockRouteManager struct {
	active bool
	saved  *SavedRoutes
}

func (m *mockRouteManager) Setup(tunName string, relayIP net.IP, origGateway net.IP, origIface string) error {
	if m.active {
		return ErrAlreadyActive
	}
	if origGateway == nil {
		return ErrGatewayResolve
	}
	m.active = true
	m.saved = &SavedRoutes{
		OrigGateway:      origGateway,
		OrigDefaultIface: origIface,
		RelayIP:          relayIP,
		TUNName:          tunName,
	}
	return nil
}

func (m *mockRouteManager) Teardown() error {
	if !m.active {
		return nil // idempotent
	}
	m.active = false
	m.saved = nil
	return nil
}

func (m *mockRouteManager) Cleanup() error {
	return nil
}

func (m *mockRouteManager) Saved() *SavedRoutes {
	return m.saved
}

func TestMock_SetupTeardownCycle(t *testing.T) {
	t.Parallel()
	m := &mockRouteManager{}

	relay := net.ParseIP("10.0.0.1")
	gw := net.ParseIP("192.168.1.1")

	if err := m.Setup("levoile0", relay, gw, "eth0"); err != nil {
		t.Fatalf("Setup: %v", err)
	}
	if !m.active {
		t.Fatal("expected active after Setup")
	}
	saved := m.Saved()
	if saved == nil {
		t.Fatal("Saved() nil after Setup")
	}
	if saved.TUNName != "levoile0" {
		t.Errorf("TUNName = %q, want levoile0", saved.TUNName)
	}
	if !saved.RelayIP.Equal(relay) {
		t.Errorf("RelayIP = %v, want %v", saved.RelayIP, relay)
	}
	if !saved.OrigGateway.Equal(gw) {
		t.Errorf("OrigGateway = %v, want %v", saved.OrigGateway, gw)
	}

	if err := m.Teardown(); err != nil {
		t.Fatalf("Teardown: %v", err)
	}
	if m.active {
		t.Fatal("expected inactive after Teardown")
	}
	if m.Saved() != nil {
		t.Error("Saved() should be nil after Teardown")
	}
}

func TestMock_DoubleSetup_ReturnsErrAlreadyActive(t *testing.T) {
	t.Parallel()
	m := &mockRouteManager{}

	relay := net.ParseIP("10.0.0.1")
	gw := net.ParseIP("192.168.1.1")

	if err := m.Setup("levoile0", relay, gw, "eth0"); err != nil {
		t.Fatalf("first Setup: %v", err)
	}
	err := m.Setup("levoile0", relay, gw, "eth0")
	if !errors.Is(err, ErrAlreadyActive) {
		t.Fatalf("second Setup = %v, want ErrAlreadyActive", err)
	}
}

func TestMock_TeardownIdempotent(t *testing.T) {
	t.Parallel()
	m := &mockRouteManager{}

	// Teardown without Setup should be silent (idempotent).
	if err := m.Teardown(); err != nil {
		t.Fatalf("Teardown without Setup: %v", err)
	}
	// Double Teardown after Setup should also be silent.
	relay := net.ParseIP("10.0.0.1")
	gw := net.ParseIP("192.168.1.1")
	_ = m.Setup("levoile0", relay, gw, "eth0")
	_ = m.Teardown()
	if err := m.Teardown(); err != nil {
		t.Fatalf("double Teardown: %v", err)
	}
}

func TestMock_NilGateway_ReturnsErrGatewayResolve(t *testing.T) {
	t.Parallel()
	m := &mockRouteManager{}

	err := m.Setup("levoile0", net.ParseIP("10.0.0.1"), nil, "eth0")
	if !errors.Is(err, ErrGatewayResolve) {
		t.Fatalf("Setup(nil gw) = %v, want ErrGatewayResolve", err)
	}
}

func TestSentinelErrors_HaveRoutingPrefix(t *testing.T) {
	t.Parallel()
	for _, e := range []error{ErrAlreadyActive, ErrGatewayResolve} {
		if len(e.Error()) < 8 || e.Error()[:8] != "routing:" {
			t.Errorf("error %q should start with 'routing:'", e)
		}
	}
}
