// Package leakcheck provides WebRTC IP leak detection by sending STUN
// Binding Requests to public STUN servers and comparing the discovered
// IP with the expected tunnel IP.
package leakcheck

import (
	"context"
	"crypto/rand"
	"encoding/binary"
	"fmt"
	"net"
	"time"
)

// statusPass and statusFail are the canonical FullLeakReport.Status values.
// Defined here (not in ipc) to avoid a circular import: leakcheck → ipc.
const (
	statusPass = "pass"
	statusFail = "fail"
)

// defaultSTUNServers are the STUN servers used for leak checks.
var defaultSTUNServers = []string{
	"stun.l.google.com:19302",
	"stun1.l.google.com:19302",
	"stun.cloudflare.com:3478",
}

// stunTimeout is the per-server timeout for STUN requests.
const stunTimeout = 5 * time.Second

// LeakResult holds the result of a single STUN leak check.
type LeakResult struct {
	Server string `json:"server"`
	IP     net.IP `json:"ip"`
	Leaked bool   `json:"leaked"`
	Error  string `json:"error,omitempty"`
}

// FullLeakReport holds the aggregated result of all STUN leak checks.
type FullLeakReport struct {
	Status  string       `json:"status"`  // "pass" or "fail"
	STUNIP  string       `json:"stun_ip"` // IP seen by STUN servers
	HTTPIP  string       `json:"http_ip"` // expected tunnel IP
	Results []LeakResult `json:"results"`
}

// PublicIPFunc returns the current public IP via the tunnel (e.g., HTTP check).
type PublicIPFunc func(ctx context.Context) (net.IP, error)

// WebRTCLeakChecker performs WebRTC leak checks using STUN.
type WebRTCLeakChecker struct {
	getPublicIP PublicIPFunc
	stunServers []string
}

// NewWebRTCLeakChecker creates a leak checker that uses getPublicIP to obtain
// the expected tunnel IP for comparison.
func NewWebRTCLeakChecker(getPublicIP PublicIPFunc) *WebRTCLeakChecker {
	return &WebRTCLeakChecker{
		getPublicIP: getPublicIP,
		stunServers: defaultSTUNServers,
	}
}

// WithSTUNServers sets custom STUN servers (for testing).
func (c *WebRTCLeakChecker) WithSTUNServers(servers []string) *WebRTCLeakChecker {
	c.stunServers = servers
	return c
}

// CheckSTUNLeak sends a STUN Binding Request to the specified server and
// returns the IP address discovered via XOR-MAPPED-ADDRESS.
func (c *WebRTCLeakChecker) CheckSTUNLeak(ctx context.Context, stunServer string) (*LeakResult, error) {
	result := &LeakResult{Server: stunServer}

	deadline, ok := ctx.Deadline()
	if !ok {
		deadline = time.Now().Add(stunTimeout)
	}

	// Resolve STUN server address.
	raddr, err := net.ResolveUDPAddr("udp", stunServer)
	if err != nil {
		return nil, fmt.Errorf("leakcheck: stun: resolve %s: %w", stunServer, err)
	}

	// Create UDP connection.
	conn, err := net.DialUDP("udp", nil, raddr)
	if err != nil {
		return nil, fmt.Errorf("leakcheck: stun: dial %s: %w", stunServer, err)
	}
	defer conn.Close()

	conn.SetDeadline(deadline)

	// Build STUN Binding Request.
	req := BuildBindingRequest()
	txnID := req[8:20] // 12-byte Transaction ID

	// Send request.
	if _, err := conn.Write(req); err != nil {
		return nil, fmt.Errorf("leakcheck: stun: write %s: %w", stunServer, err)
	}

	// Read response.
	buf := make([]byte, 1500)
	n, err := conn.Read(buf)
	if err != nil {
		return nil, fmt.Errorf("leakcheck: stun: read %s: %w", stunServer, err)
	}

	// Validate response transaction ID matches our request to prevent spoofed responses.
	if n < 20 {
		return nil, fmt.Errorf("leakcheck: stun: response too short from %s", stunServer)
	}
	for i := 0; i < 12; i++ {
		if buf[8+i] != txnID[i] {
			return nil, fmt.Errorf("leakcheck: stun: transaction ID mismatch from %s", stunServer)
		}
	}

	// Parse XOR-MAPPED-ADDRESS from response.
	ip, err := ParseXORMappedAddress(buf[:n])
	if err != nil {
		return nil, fmt.Errorf("leakcheck: stun: parse %s: %w", stunServer, err)
	}

	result.IP = ip
	return result, nil
}

// RunFullCheck executes CheckSTUNLeak on default STUN servers, compares the
// results with the expected public IP, and returns a pass/fail report.
func (c *WebRTCLeakChecker) RunFullCheck(ctx context.Context) (*FullLeakReport, error) {
	report := &FullLeakReport{
		Status: statusPass,
	}

	// Get expected public IP via tunnel.
	expectedIP, err := c.getPublicIP(ctx)
	if err != nil {
		return nil, fmt.Errorf("leakcheck: get public ip: %w", err)
	}
	report.HTTPIP = expectedIP.String()

	for _, server := range c.stunServers {
		serverCtx, cancel := context.WithTimeout(ctx, stunTimeout)
		result, err := c.CheckSTUNLeak(serverCtx, server)
		cancel()

		if err != nil {
			report.Results = append(report.Results, LeakResult{
				Server: server,
				Error:  err.Error(),
			})
			continue
		}

		// Compare STUN IP with expected tunnel IP.
		if !result.IP.Equal(expectedIP) {
			result.Leaked = true
			report.Status = statusFail
		}

		if report.STUNIP == "" {
			report.STUNIP = result.IP.String()
		}
		report.Results = append(report.Results, *result)
	}

	// Verify at least one STUN check succeeded.
	successCount := 0
	for _, r := range report.Results {
		if r.Error == "" {
			successCount++
		}
	}
	if successCount == 0 {
		return nil, fmt.Errorf("leakcheck: all %d STUN servers unreachable", len(c.stunServers))
	}

	return report, nil
}

// BuildBindingRequest creates a minimal STUN Binding Request (type 0x0001).
func BuildBindingRequest() []byte {
	pkt := make([]byte, 20)
	// Type: Binding Request (0x0001)
	binary.BigEndian.PutUint16(pkt[0:2], 0x0001)
	// Length: 0 (no attributes)
	binary.BigEndian.PutUint16(pkt[2:4], 0)
	// Magic Cookie
	binary.BigEndian.PutUint32(pkt[4:8], stunMagicCookie)
	// Transaction ID: 12 random bytes
	rand.Read(pkt[8:20])
	return pkt
}
