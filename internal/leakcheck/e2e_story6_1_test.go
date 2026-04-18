package leakcheck

import (
	"context"
	"net"
	"strings"
	"testing"
	"time"

	"github.com/velia-the-veil/le_voile/internal/tunnel"
)

// startMockSTUNServerWithTxIDMismatch responds with an INCORRECT transaction
// ID to exercise the mismatch path (AC2 — defense against UDP spoofing).
// Requests without matching txID are replied to with the inverted txID.
func startMockSTUNServerWithTxIDMismatch(t *testing.T, responseIP net.IP) (string, func()) {
	t.Helper()

	conn, err := net.ListenPacket("udp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen mock: %v", err)
	}
	addr := conn.LocalAddr().String()
	done := make(chan struct{})

	go func() {
		defer close(done)
		buf := make([]byte, 1500)
		for {
			n, clientAddr, err := conn.ReadFrom(buf)
			if err != nil {
				return
			}
			if n < 20 {
				continue
			}
			// Deliberately scramble the txID.
			var bogusTxID [12]byte
			for i := 0; i < 12; i++ {
				bogusTxID[i] = buf[8+i] ^ 0xFF
			}
			resp := buildSTUNResponse(responseIP, bogusTxID)
			conn.WriteTo(resp, clientAddr)
		}
	}()

	cleanup := func() {
		conn.Close()
		<-done
	}
	return addr, cleanup
}

// startMockSTUNServerSilent never responds — used to test per-server timeout
// and all-servers-down failover.
func startMockSTUNServerSilent(t *testing.T) (string, func()) {
	t.Helper()
	conn, err := net.ListenPacket("udp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen silent: %v", err)
	}
	addr := conn.LocalAddr().String()
	return addr, func() { conn.Close() }
}

// TestCheckSTUNLeak_TransactionIDMismatch (Story 6.1 AC2) — the checker
// MUST reject a response whose transaction ID differs from the request.
// This guards against UDP spoofing / off-path injection.
func TestCheckSTUNLeak_TransactionIDMismatch(t *testing.T) {
	addr, cleanup := startMockSTUNServerWithTxIDMismatch(t, net.IPv4(198, 51, 100, 42))
	defer cleanup()

	checker := NewWebRTCLeakChecker(nil)
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	_, err := checker.CheckSTUNLeak(ctx, addr)
	if err == nil {
		t.Fatal("expected transaction ID mismatch error, got nil")
	}
	if err != nil && !strings.Contains(err.Error(), "transaction ID mismatch") {
		t.Errorf("expected transaction ID mismatch error, got %v", err)
	}
}

// TestRunFullCheck_AllServersSilent_Story61 (Story 6.1 AC3) — when every STUN
// server is silent, RunFullCheck returns a concrete error rather than
// swallowing the failure.
func TestRunFullCheck_AllServersSilent_Story61(t *testing.T) {
	a1, c1 := startMockSTUNServerSilent(t)
	defer c1()
	a2, c2 := startMockSTUNServerSilent(t)
	defer c2()
	a3, c3 := startMockSTUNServerSilent(t)
	defer c3()

	checker := NewWebRTCLeakChecker(func(_ context.Context) (net.IP, error) {
		return net.IPv4(198, 51, 100, 1), nil
	}).WithSTUNServers([]string{a1, a2, a3})

	ctx, cancel := context.WithTimeout(context.Background(), 25*time.Second)
	defer cancel()

	_, err := checker.RunFullCheck(ctx)
	if err == nil {
		t.Fatal("expected all-unreachable error, got nil")
	}
	if !strings.Contains(err.Error(), "all 3 STUN servers unreachable") {
		t.Errorf("expected all-unreachable message, got %v", err)
	}
}

// TestRunFullCheck_FailoverAcrossServers (Story 6.1 AC3) — when server #1 is
// silent but server #2 answers, the check succeeds and reports a pass.
func TestRunFullCheck_FailoverAcrossServers(t *testing.T) {
	vpsIP := net.IPv4(203, 0, 113, 5)
	a1, c1 := startMockSTUNServerSilent(t)
	defer c1()
	a2, c2 := startMockSTUNServer(t, vpsIP)
	defer c2()
	a3, c3 := startMockSTUNServer(t, vpsIP)
	defer c3()

	checker := NewWebRTCLeakChecker(func(_ context.Context) (net.IP, error) {
		return vpsIP, nil
	}).WithSTUNServers([]string{a1, a2, a3})

	ctx, cancel := context.WithTimeout(context.Background(), 25*time.Second)
	defer cancel()

	report, err := checker.RunFullCheck(ctx)
	if err != nil {
		t.Fatalf("RunFullCheck: %v", err)
	}
	if report.Status != statusOK {
		t.Errorf("status=%q want pass", report.Status)
	}
	if len(report.Results) != 3 {
		t.Fatalf("results len=%d want 3", len(report.Results))
	}
	if report.Results[0].Error == "" {
		t.Error("expected error for silent server #1")
	}
	if report.Results[1].Error != "" || report.Results[1].IP == nil {
		t.Errorf("expected success for server #2, got %+v", report.Results[1])
	}
}

// TestPeriodicScheduler_RunCheck_EmitsSTUNAndParsesResponse (Story 6.1 AC1) —
// end-to-end: the scheduler (triggered manually) runs the checker against a
// real UDP STUN mock server and LastResult captures a pass status.
// Real-TUN validation is covered by Epic 6 smoke test (Task 11.6), not here.
func TestPeriodicScheduler_RunCheck_EmitsSTUNAndParsesResponse(t *testing.T) {
	vpsIP := net.IPv4(198, 51, 100, 42)
	a, cleanup := startMockSTUNServer(t, vpsIP)
	defer cleanup()

	checker := NewWebRTCLeakChecker(func(_ context.Context) (net.IP, error) {
		return vpsIP, nil
	}).WithSTUNServers([]string{a})

	ts := &mockTunnelState{state: tunnel.StateConnected}
	sched := NewPeriodicScheduler(10*time.Minute, checker, ts, nil, nil)

	// TriggerCheck runs RunFullCheck synchronously.
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	sched.TriggerCheck(ctx)

	report, when := sched.LastResult()
	if report == nil {
		t.Fatal("LastResult is nil after TriggerCheck")
	}
	if report.Status != statusOK {
		t.Errorf("status=%q want pass", report.Status)
	}
	if report.STUNIP != vpsIP.String() {
		t.Errorf("STUNIP=%q want %q", report.STUNIP, vpsIP.String())
	}
	if when.IsZero() {
		t.Error("lastCheckAt should be set")
	}
}

// TestPeriodicScheduler_FailoverAcrossServers_E2E (Story 6.1 AC3) — the
// scheduler's view of a partial failover: silent #1, OK #2, OK #3.
func TestPeriodicScheduler_FailoverAcrossServers_E2E(t *testing.T) {
	vpsIP := net.IPv4(203, 0, 113, 7)
	a1, c1 := startMockSTUNServerSilent(t)
	defer c1()
	a2, c2 := startMockSTUNServer(t, vpsIP)
	defer c2()
	a3, c3 := startMockSTUNServer(t, vpsIP)
	defer c3()

	checker := NewWebRTCLeakChecker(func(_ context.Context) (net.IP, error) {
		return vpsIP, nil
	}).WithSTUNServers([]string{a1, a2, a3})

	ts := &mockTunnelState{state: tunnel.StateConnected}
	sched := NewPeriodicScheduler(10*time.Minute, checker, ts, nil, nil)

	ctx, cancel := context.WithTimeout(context.Background(), 25*time.Second)
	defer cancel()
	sched.TriggerCheck(ctx)

	report, _ := sched.LastResult()
	if report == nil {
		t.Fatal("LastResult nil")
	}
	if report.Status != statusOK {
		t.Errorf("status=%q want pass", report.Status)
	}
	if len(report.Results) != 3 {
		t.Fatalf("results len=%d want 3", len(report.Results))
	}
	// Silent server #1 must have errored out.
	if report.Results[0].Error == "" {
		t.Errorf("expected error for silent server #1, got IP=%v", report.Results[0].IP)
	}
	// Answering servers #2 and #3 must have succeeded with the expected IP.
	for i := 1; i <= 2; i++ {
		if report.Results[i].Error != "" {
			t.Errorf("unexpected error on answering server #%d: %v", i+1, report.Results[i].Error)
		}
		if !report.Results[i].IP.Equal(vpsIP) {
			t.Errorf("server #%d IP = %v, want %v", i+1, report.Results[i].IP, vpsIP)
		}
	}
	// End-to-end real-TUN validation is covered by Epic 6 smoke test (Task 11.6), not by unit tests.
}

