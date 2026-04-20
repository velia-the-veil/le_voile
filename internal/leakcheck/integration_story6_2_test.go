package leakcheck

import (
	"context"
	"net"
	"net/netip"
	"testing"
	"time"
)

// TestIntegration_CheckerWithRelayIPResolver_OK (Story 6.2 AC 8.1) — wires
// a RelayIPResolver (stubbed DoH) into a real WebRTCLeakChecker, runs the
// full check against a mock STUN server that echoes the expected IP, and
// verifies the report is "ok" with ExpectedIP populated.
func TestIntegration_CheckerWithRelayIPResolver_OK(t *testing.T) {
	relayIP := net.IPv4(198, 51, 100, 42)
	serverAddr, cleanup := startMockSTUNServer(t, relayIP)
	defer cleanup()

	doh := &stubDoH{addr: netip.MustParseAddr(relayIP.String())}
	resolver, err := NewRelayIPResolver(staticDomain("relay.example.com"), doh)
	if err != nil {
		t.Fatalf("NewRelayIPResolver: %v", err)
	}

	checker := NewWebRTCLeakChecker(resolver.ExpectedIP).
		WithSTUNServers([]string{serverAddr})

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	report, err := checker.RunFullCheck(ctx)
	if err != nil {
		t.Fatalf("RunFullCheck: %v", err)
	}
	if report.Status != statusOK {
		t.Errorf("Status = %q, want %q", report.Status, statusOK)
	}
	if report.ExpectedIP != relayIP.String() {
		t.Errorf("ExpectedIP = %q, want %q", report.ExpectedIP, relayIP.String())
	}
	if report.STUNIP != relayIP.String() {
		t.Errorf("STUNIP = %q, want %q", report.STUNIP, relayIP.String())
	}
	if report.LeakReason != "" {
		t.Errorf("LeakReason = %q, want empty on ok", report.LeakReason)
	}
}

// TestIntegration_CheckerWithRelayIPResolver_LeakDetected (Story 6.2 AC 8.2)
// — DoH returns the relay IP, STUN returns a different public IP (ISP
// fictive) → Status = leak_detected, LeakReason = stun_ip_differs_from_relay.
func TestIntegration_CheckerWithRelayIPResolver_LeakDetected(t *testing.T) {
	relayIP := net.IPv4(198, 51, 100, 42)
	ispIP := net.IPv4(203, 0, 113, 99)
	serverAddr, cleanup := startMockSTUNServer(t, ispIP)
	defer cleanup()

	doh := &stubDoH{addr: netip.MustParseAddr(relayIP.String())}
	resolver, err := NewRelayIPResolver(staticDomain("relay.example.com"), doh)
	if err != nil {
		t.Fatalf("NewRelayIPResolver: %v", err)
	}

	checker := NewWebRTCLeakChecker(resolver.ExpectedIP).
		WithSTUNServers([]string{serverAddr})

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	report, err := checker.RunFullCheck(ctx)
	if err != nil {
		t.Fatalf("RunFullCheck: %v", err)
	}
	if report.Status != statusLeakDetected {
		t.Errorf("Status = %q, want %q", report.Status, statusLeakDetected)
	}
	if report.LeakReason != LeakReasonStunIPDiffers {
		t.Errorf("LeakReason = %q, want %q", report.LeakReason, LeakReasonStunIPDiffers)
	}
	if report.ExpectedIP != relayIP.String() {
		t.Errorf("ExpectedIP = %q, want %q", report.ExpectedIP, relayIP.String())
	}
}

// TestIntegration_CheckerWithRelayIPResolver_TUNDownHeuristic (Story 6.2
// AC 8.3) — STUN returns an RFC1918 IP (the request never hit the public
// internet) → LeakReason = tun_capture_likely_down.
func TestIntegration_CheckerWithRelayIPResolver_TUNDownHeuristic(t *testing.T) {
	relayIP := net.IPv4(198, 51, 100, 42)
	privateIP := net.IPv4(192, 168, 1, 5)
	serverAddr, cleanup := startMockSTUNServer(t, privateIP)
	defer cleanup()

	doh := &stubDoH{addr: netip.MustParseAddr(relayIP.String())}
	resolver, err := NewRelayIPResolver(staticDomain("relay.example.com"), doh)
	if err != nil {
		t.Fatalf("NewRelayIPResolver: %v", err)
	}

	checker := NewWebRTCLeakChecker(resolver.ExpectedIP).
		WithSTUNServers([]string{serverAddr})

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	report, err := checker.RunFullCheck(ctx)
	if err != nil {
		t.Fatalf("RunFullCheck: %v", err)
	}
	if report.Status != statusLeakDetected {
		t.Errorf("Status = %q, want %q", report.Status, statusLeakDetected)
	}
	if report.LeakReason != LeakReasonTUNDown {
		t.Errorf("LeakReason = %q, want %q (private STUN IP ⇒ TUN broken)", report.LeakReason, LeakReasonTUNDown)
	}
}

// TestIntegration_CheckerWithRelayIPResolver_DoHFailurePropagates (Story 6.2
// AC5) — when DoH cannot resolve the relay, the checker propagates the
// error instead of producing a false leak_detected.
func TestIntegration_CheckerWithRelayIPResolver_DoHFailurePropagates(t *testing.T) {
	serverAddr, cleanup := startMockSTUNServer(t, net.IPv4(198, 51, 100, 42))
	defer cleanup()

	doh := &stubDoH{err: errDoHDown{}}
	resolver, err := NewRelayIPResolver(staticDomain("relay.example.com"), doh)
	if err != nil {
		t.Fatalf("NewRelayIPResolver: %v", err)
	}

	checker := NewWebRTCLeakChecker(resolver.ExpectedIP).
		WithSTUNServers([]string{serverAddr})

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	_, err = checker.RunFullCheck(ctx)
	if err == nil {
		t.Fatal("expected error when DoH is down, got nil (would mask a real leak)")
	}
}

type errDoHDown struct{}

func (errDoHDown) Error() string { return "simulated doh down" }
