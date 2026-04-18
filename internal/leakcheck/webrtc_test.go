package leakcheck

import (
	"context"
	"net"
	"testing"
	"time"
)

// startMockSTUNServer starts a UDP server that responds to STUN Binding Requests
// with a Binding Success Response containing the given IP in XOR-MAPPED-ADDRESS.
// Returns the server address and a cleanup function.
func startMockSTUNServer(t *testing.T, responseIP net.IP) (string, func()) {
	t.Helper()

	conn, err := net.ListenPacket("udp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen mock STUN: %v", err)
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
			if n < stunHeaderSize {
				continue
			}

			// Extract transaction ID from request.
			var txID [12]byte
			copy(txID[:], buf[8:20])

			// Build response with the configured IP.
			resp := buildSTUNResponse(responseIP, txID)
			conn.WriteTo(resp, clientAddr)
		}
	}()

	cleanup := func() {
		conn.Close()
		<-done
	}

	return addr, cleanup
}

func TestCheckSTUNLeak_MockServer(t *testing.T) {
	expectedIP := net.IPv4(198, 51, 100, 42)
	serverAddr, cleanup := startMockSTUNServer(t, expectedIP)
	defer cleanup()

	checker := NewWebRTCLeakChecker(nil)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	result, err := checker.CheckSTUNLeak(ctx, serverAddr)
	if err != nil {
		t.Fatalf("CheckSTUNLeak() error = %v", err)
	}

	if !result.IP.Equal(expectedIP.To4()) {
		t.Errorf("CheckSTUNLeak() IP = %v, want %v", result.IP, expectedIP)
	}
	if result.Server != serverAddr {
		t.Errorf("CheckSTUNLeak() Server = %q, want %q", result.Server, serverAddr)
	}
}

func TestCheckSTUNLeak_Timeout(t *testing.T) {
	// Connect to a port that won't respond.
	conn, err := net.ListenPacket("udp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	addr := conn.LocalAddr().String()
	conn.Close() // Close immediately — no responses

	checker := NewWebRTCLeakChecker(nil)
	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()

	_, err = checker.CheckSTUNLeak(ctx, addr)
	if err == nil {
		t.Error("expected error for unresponsive server, got nil")
	}
}

func TestRunFullCheck_AllPass(t *testing.T) {
	// All servers return the same IP.
	vpsIP := net.IPv4(198, 51, 100, 1)
	server1Addr, cleanup1 := startMockSTUNServer(t, vpsIP)
	defer cleanup1()
	server2Addr, cleanup2 := startMockSTUNServer(t, vpsIP)
	defer cleanup2()
	server3Addr, cleanup3 := startMockSTUNServer(t, vpsIP)
	defer cleanup3()

	getPublicIP := func(ctx context.Context) (net.IP, error) {
		return vpsIP, nil
	}
	checker := NewWebRTCLeakChecker(getPublicIP).
		WithSTUNServers([]string{server1Addr, server2Addr, server3Addr})

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	report, err := checker.RunFullCheck(ctx)
	if err != nil {
		t.Fatalf("RunFullCheck() error = %v", err)
	}

	if report.Status != statusOK {
		t.Errorf("RunFullCheck() Status = %q, want %q", report.Status, statusOK)
	}
	if report.STUNIP != vpsIP.String() {
		t.Errorf("RunFullCheck() STUNIP = %q, want %q", report.STUNIP, vpsIP.String())
	}
	if len(report.Results) != 3 {
		t.Errorf("RunFullCheck() results = %d, want 3", len(report.Results))
	}
	for _, r := range report.Results {
		if r.Leaked {
			t.Errorf("result for %s marked as leaked, expected not leaked", r.Server)
		}
	}
}

func TestRunFullCheck_LeakDetected(t *testing.T) {
	// One server returns a different IP — leak detected.
	vpsIP := net.IPv4(198, 51, 100, 1)
	realIP := net.IPv4(192, 168, 1, 100) // real IP leaked

	server1Addr, cleanup1 := startMockSTUNServer(t, vpsIP)
	defer cleanup1()
	server2Addr, cleanup2 := startMockSTUNServer(t, realIP) // leak!
	defer cleanup2()
	server3Addr, cleanup3 := startMockSTUNServer(t, vpsIP)
	defer cleanup3()

	getPublicIP := func(ctx context.Context) (net.IP, error) {
		return vpsIP, nil
	}
	checker := NewWebRTCLeakChecker(getPublicIP).
		WithSTUNServers([]string{server1Addr, server2Addr, server3Addr})

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	report, err := checker.RunFullCheck(ctx)
	if err != nil {
		t.Fatalf("RunFullCheck() error = %v", err)
	}

	if report.Status != statusLeakDetected {
		t.Errorf("RunFullCheck() Status = %q, want %q", report.Status, statusLeakDetected)
	}

	// Verify at least one result is marked as leaked.
	leakedCount := 0
	for _, r := range report.Results {
		if r.Leaked {
			leakedCount++
		}
	}
	if leakedCount == 0 {
		t.Error("expected at least one result marked as leaked")
	}
}

func TestLeakCheck_WithProxy(t *testing.T) {
	// This test simulates the full leak check scenario:
	// - Mock STUN servers all return the VPS IP (simulating interception working)
	// - The public IP function returns the same VPS IP
	// - Result should be PASS (no leak)
	vpsIP := net.IPv4(203, 0, 113, 1)

	server1Addr, cleanup1 := startMockSTUNServer(t, vpsIP)
	defer cleanup1()
	server2Addr, cleanup2 := startMockSTUNServer(t, vpsIP)
	defer cleanup2()
	server3Addr, cleanup3 := startMockSTUNServer(t, vpsIP)
	defer cleanup3()

	// Simulate: public IP obtained via tunnel matches VPS IP
	getPublicIP := func(ctx context.Context) (net.IP, error) {
		return vpsIP, nil
	}
	checker := NewWebRTCLeakChecker(getPublicIP).
		WithSTUNServers([]string{server1Addr, server2Addr, server3Addr})

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	report, err := checker.RunFullCheck(ctx)
	if err != nil {
		t.Fatalf("RunFullCheck() error = %v", err)
	}

	if report.Status != statusOK {
		t.Errorf("RunFullCheck() Status = %q, want %q", report.Status, statusOK)
	}
	if report.STUNIP != vpsIP.String() {
		t.Errorf("RunFullCheck() STUNIP = %q, want %q", report.STUNIP, vpsIP.String())
	}
	if report.ExpectedIP != vpsIP.String() {
		t.Errorf("RunFullCheck() ExpectedIP = %q, want %q", report.ExpectedIP, vpsIP.String())
	}
	for _, r := range report.Results {
		if r.Leaked {
			t.Errorf("result for %s marked as leaked, expected pass", r.Server)
		}
		if !r.IP.Equal(vpsIP) {
			t.Errorf("result for %s IP = %v, want %v", r.Server, r.IP, vpsIP)
		}
	}
}
