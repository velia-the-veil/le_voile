//go:build windows

package service

import (
	"context"
	"net"
	"testing"
)

// TestReconfigureForRelay_FirewallAndRoutingUseNewIP is the regression for
// the country-switch loop: handleSelectCountry → tunnelClient.UpdateRelay
// flipped the relay metadata, but the firewall's "Allow Relay :443/UDP"
// filter and the /32 anti-loop route still pointed at the OLD relay IP.
// Connect to the new relay was then blocked by WFP → 5 retries → circuit
// breaker → user stranded with kill-switch active.
//
// Ensures Reconfigure: stores the new IP in firewallRelayIP, calls
// firewall.Activate with the new IP, and re-runs routing.Setup with the
// new IP.
func TestReconfigureForRelay_FirewallAndRoutingUseNewIP(t *testing.T) {
	p, _, _, fw := newTestProgramForAnomaly(t)

	// Initial state: program was Activated for relay 203.0.113.5 in the
	// helper. Now switch to a new country whose primary relay is .42.
	newRelayIP := net.IPv4(198, 51, 100, 42)

	// Wire a routeMgr that captures Setup args; the helper doesn't pre-create
	// one, so we install a fake here.
	rm := &fakeRouteMgr{}
	p.routeMgr = rm

	if err := p.ReconfigureForRelay(context.Background(), newRelayIP); err != nil {
		t.Fatalf("ReconfigureForRelay: %v", err)
	}

	// Stored IP — read back via the same getter recoverTUN uses.
	got := p.resolvedRelayIP()
	if !got.Equal(newRelayIP.To4()) {
		t.Errorf("firewallRelayIP = %v, want %v", got, newRelayIP)
	}

	// Firewall was re-Activated with the new IP. The fake captures
	// the most recent ActivateParams.RelayIP.
	if fwIP := fw.LastRelayIP(); !fwIP.Equal(newRelayIP.To4()) {
		t.Errorf("firewall.Activate RelayIP = %v, want %v", fwIP, newRelayIP)
	}
	if fw.activateCalls < 1 {
		t.Errorf("firewall.Activate calls = %d, want \u22651", fw.activateCalls)
	}

	// Routing was torn down (old) and Setup was called with the new IP.
	// routingFactory installed by the helper returns a fresh fakeRouteMgr;
	// we can read p.routeMgr after the call to assert on it.
	newRm, ok := p.routeMgr.(*fakeRouteMgr)
	if !ok {
		t.Fatalf("p.routeMgr type = %T, want *fakeRouteMgr", p.routeMgr)
	}
	if newRm.setupCalls.Load() == 0 {
		t.Errorf("routing.Setup was not called on new manager")
	}
	if rIP := newRm.LastRelayIP(); !rIP.Equal(newRelayIP.To4()) {
		t.Errorf("routing.Setup RelayIP = %v, want %v", rIP, newRelayIP)
	}
	if rm.teardownCalls.Load() == 0 {
		t.Errorf("old routing.Teardown was not called")
	}
}

func TestReconfigureForRelay_RejectsNilOrIPv6(t *testing.T) {
	p, _, _, _ := newTestProgramForAnomaly(t)

	if err := p.ReconfigureForRelay(context.Background(), nil); err == nil {
		t.Error("expected error for nil relay IP")
	}
	v6 := net.ParseIP("2001:db8::1")
	if err := p.ReconfigureForRelay(context.Background(), v6); err == nil {
		t.Error("expected error for non-IPv4 relay IP")
	}
}
