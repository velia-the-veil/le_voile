package leakcheck

import (
	"context"
	"encoding/binary"
	"errors"
	"net"
	"testing"
	"time"
)

// TestRunFullCheck_ExpectedIPError verifies that RunFullCheck returns an error
// when the ExpectedIPFunc fails (e.g., DoH resolve failure).
func TestRunFullCheck_ExpectedIPError(t *testing.T) {
	t.Parallel()

	getExpectedIP := func(ctx context.Context) (net.IP, error) {
		return nil, errors.New("doh upstream down")
	}
	checker := NewWebRTCLeakChecker(getExpectedIP)

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	_, err := checker.RunFullCheck(ctx)
	if err == nil {
		t.Fatal("[P0] RunFullCheck() should return error when getExpectedIP fails")
	}
	if got := err.Error(); got != "leakcheck: resolve relay ip: doh upstream down" {
		t.Errorf("[P0] error = %q, want 'leakcheck: resolve relay ip: doh upstream down'", got)
	}
}

// TestRunFullCheck_NilExpectedIP verifies that an ExpectedIPFunc returning
// a nil IP produces a clear error rather than a silent false OK.
func TestRunFullCheck_NilExpectedIP(t *testing.T) {
	t.Parallel()

	getExpectedIP := func(ctx context.Context) (net.IP, error) {
		return nil, nil
	}
	checker := NewWebRTCLeakChecker(getExpectedIP)

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	_, err := checker.RunFullCheck(ctx)
	if err == nil {
		t.Fatal("expected error when ExpectedIPFunc returns nil, got none")
	}
	if got := err.Error(); got != "leakcheck: empty expected ip" {
		t.Errorf("error = %q, want 'leakcheck: empty expected ip'", got)
	}
}

// TestRunFullCheck_AllServersUnreachable verifies that RunFullCheck returns an
// error when every STUN server is unreachable (no successful checks).
func TestRunFullCheck_AllServersUnreachable(t *testing.T) {
	t.Parallel()

	vpsIP := net.IPv4(198, 51, 100, 1)
	getPublicIP := func(ctx context.Context) (net.IP, error) {
		return vpsIP, nil
	}

	// Use ports that are closed (listen then immediately close).
	addrs := make([]string, 3)
	for i := range addrs {
		conn, err := net.ListenPacket("udp", "127.0.0.1:0")
		if err != nil {
			t.Fatalf("listen: %v", err)
		}
		addrs[i] = conn.LocalAddr().String()
		conn.Close() // Close immediately — no responses
	}

	checker := NewWebRTCLeakChecker(getPublicIP).
		WithSTUNServers(addrs)

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	_, err := checker.RunFullCheck(ctx)
	if err == nil {
		t.Fatal("[P0] RunFullCheck() should return error when all STUN servers unreachable")
	}
}

// TestRunFullCheck_PartialFailures verifies that RunFullCheck succeeds when
// some STUN servers fail but at least one succeeds.
// Working server is listed first so it succeeds quickly before the silent
// server consumes the remaining context budget via stunTimeout.
func TestRunFullCheck_PartialFailures(t *testing.T) {
	t.Parallel()

	vpsIP := net.IPv4(198, 51, 100, 1)

	// One working server.
	serverAddr, cleanup := startMockSTUNServer(t, vpsIP)
	defer cleanup()

	// One "silent" server: listens but never responds (guaranteed timeout).
	silentConn, err := net.ListenPacket("udp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen silent: %v", err)
	}
	silentAddr := silentConn.LocalAddr().String()
	defer silentConn.Close()

	getPublicIP := func(ctx context.Context) (net.IP, error) {
		return vpsIP, nil
	}
	// Working server first so it succeeds quickly; silent server last.
	checker := NewWebRTCLeakChecker(getPublicIP).
		WithSTUNServers([]string{serverAddr, silentAddr})

	// Long enough context: 1 quick success + 1 stunTimeout (5s) + margin.
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	report, err := checker.RunFullCheck(ctx)
	if err != nil {
		t.Fatalf("[P1] RunFullCheck() error = %v, want nil (partial success)", err)
	}

	if report.Status != statusOK {
		t.Errorf("[P1] Status = %q, want %q", report.Status, statusOK)
	}
	if len(report.Results) != 2 {
		t.Errorf("[P1] results count = %d, want 2", len(report.Results))
	}

	// Verify we have both errors and successes.
	errorCount := 0
	successCount := 0
	for _, r := range report.Results {
		if r.Error != "" {
			errorCount++
		} else {
			successCount++
		}
	}
	if successCount == 0 {
		t.Error("[P1] expected at least one successful result")
	}
	if errorCount == 0 {
		t.Error("[P1] expected at least one error result")
	}
}

// TestBuildBindingRequest_Format verifies the STUN Binding Request format:
// 20 bytes, type 0x0001, length 0, valid magic cookie, 12-byte transaction ID.
func TestBuildBindingRequest_Format(t *testing.T) {
	t.Parallel()

	pkt := BuildBindingRequest()

	if len(pkt) != 20 {
		t.Fatalf("[P1] BuildBindingRequest() length = %d, want 20", len(pkt))
	}

	// Message type: Binding Request (0x0001)
	msgType := binary.BigEndian.Uint16(pkt[0:2])
	if msgType != 0x0001 {
		t.Errorf("[P1] message type = 0x%04X, want 0x0001", msgType)
	}

	// Length: 0 (no attributes)
	length := binary.BigEndian.Uint16(pkt[2:4])
	if length != 0 {
		t.Errorf("[P1] length = %d, want 0", length)
	}

	// Magic Cookie: 0x2112A442
	cookie := binary.BigEndian.Uint32(pkt[4:8])
	if cookie != stunMagicCookie {
		t.Errorf("[P1] magic cookie = 0x%08X, want 0x%08X", cookie, stunMagicCookie)
	}

	// Transaction ID: 12 bytes, should not be all zeros (random).
	txID := pkt[8:20]
	allZero := true
	for _, b := range txID {
		if b != 0 {
			allZero = false
			break
		}
	}
	if allZero {
		t.Error("[P1] transaction ID is all zeros, expected random bytes")
	}
}

// TestBuildBindingRequest_UniqueTransactionIDs verifies that two consecutive
// calls produce different transaction IDs (randomness check).
func TestBuildBindingRequest_UniqueTransactionIDs(t *testing.T) {
	t.Parallel()

	pkt1 := BuildBindingRequest()
	pkt2 := BuildBindingRequest()

	txID1 := pkt1[8:20]
	txID2 := pkt2[8:20]

	match := true
	for i := range txID1 {
		if txID1[i] != txID2[i] {
			match = false
			break
		}
	}
	if match {
		t.Error("[P1] two BuildBindingRequest() calls produced identical transaction IDs")
	}
}

// TestCheckSTUNLeak_InvalidServerAddress verifies that CheckSTUNLeak returns
// a resolve error for an invalid STUN server address.
func TestCheckSTUNLeak_InvalidServerAddress(t *testing.T) {
	t.Parallel()

	checker := NewWebRTCLeakChecker(nil)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	_, err := checker.CheckSTUNLeak(ctx, ":::invalid:::address")
	if err == nil {
		t.Fatal("[P1] CheckSTUNLeak() should return error for invalid address")
	}
}

// TestRunFullCheck_CancelledContext verifies that RunFullCheck returns an error
// when the context is already cancelled.
func TestRunFullCheck_CancelledContext(t *testing.T) {
	t.Parallel()

	getPublicIP := func(ctx context.Context) (net.IP, error) {
		return nil, ctx.Err()
	}

	checker := NewWebRTCLeakChecker(getPublicIP)

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // Cancel immediately

	_, err := checker.RunFullCheck(ctx)
	if err == nil {
		t.Fatal("[P1] RunFullCheck() should return error with cancelled context")
	}
}
