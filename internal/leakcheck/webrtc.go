// Package leakcheck provides WebRTC IP leak detection by sending STUN
// Binding Requests to public STUN servers and comparing the discovered
// IP with the expected tunnel IP.
//
// Story 6.1 refactor: after Epic 2 (L3 capture via levoile0 + default route),
// STUN requests no longer need an applicative relay. The checker simply
// opens a UDP socket via net.DialUDP — the OS routes the packet through
// the TUN interface, the tunnel encapsulates it to the relay, the relay
// NAT-forwards to the STUN server. The reverse path brings the response
// back. No /stun-relay endpoint, no custom relay function.
//
// Story 6.2 refactor: the report status uses "ok"/"leak_detected" (renamed
// from "pass"/"fail") to align with the Validation Anti-Fuite framing. The
// "expected IP" is now the active relay's public IP (resolved via DoH from
// config.Relay.Domain), not a generic HTTP IP probe. A LEAK_DETECTED result
// is a VALIDATION signal (TUN down / routing misconfig / bug), not a normal
// product failure — Epic 2 (capture L3 + firewall) provides the structural
// defense.
package leakcheck

import (
	"context"
	"crypto/rand"
	"encoding/binary"
	"fmt"
	"net"
	"time"
)

// statusOK and statusLeakDetected are the canonical FullLeakReport.Status
// values. Defined here (not in ipc) to avoid a circular import: leakcheck → ipc.
const (
	statusOK           = "ok"
	statusLeakDetected = "leak_detected"
)

// LeakReason codes classify why a LEAK_DETECTED was reported. They give
// the caller a short machine-readable signal so the UI (story 6.3) and
// operators can differentiate a TUN outage from a routing misconfiguration.
const (
	// LeakReasonTUNDown indicates the STUN response carried a private or
	// loopback IP — the Binding Request never reached the public internet,
	// so the TUN pump or the OS routing is broken.
	LeakReasonTUNDown = "tun_capture_likely_down"
	// LeakReasonStunIPDiffers indicates the STUN response carried a public
	// IP that differs from the expected relay IP — typically the ISP IP,
	// meaning traffic bypassed the tunnel.
	LeakReasonStunIPDiffers = "stun_ip_differs_from_relay"
)

// defaultSTUNServers are the STUN servers used for leak checks.
var defaultSTUNServers = []string{
	"stun.l.google.com:19302",
	"stun1.l.google.com:19302",
	"stun.cloudflare.com:3478",
}

// DefaultSTUNServers returns a copy of the built-in STUN server list
// (Google x2 + Cloudflare). Callers can override the first entry to apply
// a legacy [stun] default_server config without duplicating the fallback.
func DefaultSTUNServers() []string {
	out := make([]string, len(defaultSTUNServers))
	copy(out, defaultSTUNServers)
	return out
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
	// Status is "ok" (no leak) or "leak_detected" (a STUN server returned
	// an IP that differs from the expected relay IP).
	Status string `json:"status"`
	// STUNIP is the IP observed by the first successful STUN server.
	STUNIP string `json:"stun_ip"`
	// ExpectedIP is the relay's public IP the STUN servers SHOULD see when
	// the TUN capture is intact. Story 6.2: resolved via DoH from the
	// active relay domain. Empty when the checker runs without an expected
	// source (validation-only mode, pre-6.2 transition).
	ExpectedIP string `json:"expected_ip,omitempty"`
	// LeakReason classifies the leak when Status == "leak_detected":
	// "tun_capture_likely_down" (private/loopback IP, TUN broken) or
	// "stun_ip_differs_from_relay" (public IP but not the relay's).
	// Empty when Status == "ok".
	LeakReason string       `json:"leak_reason,omitempty"`
	Results    []LeakResult `json:"results"`
}

// ExpectedIPFunc returns the IP that STUN servers SHOULD report when the
// tunnel is functioning correctly — typically the active relay's public
// IPv4, resolved via DoH from the relay domain. Returning an error (DoH
// unreachable, invalid domain) aborts the check without producing a
// LEAK_DETECTED false positive: the scheduler treats it as transient.
type ExpectedIPFunc func(ctx context.Context) (net.IP, error)

// PublicIPFunc is a legacy alias retained for backward compatibility with
// pre-6.2 call sites. New code should use ExpectedIPFunc, which has the
// same signature but documents the semantic clearly.
//
// Deprecated: use ExpectedIPFunc.
type PublicIPFunc = ExpectedIPFunc

// WebRTCLeakChecker performs WebRTC leak checks using STUN.
type WebRTCLeakChecker struct {
	getExpectedIP ExpectedIPFunc
	stunServers   []string
}

// NewWebRTCLeakChecker creates a leak checker. getExpectedIP returns the
// IP that STUN servers SHOULD observe when the TUN capture is intact
// (typically the active relay's public IP resolved from config.Relay.Domain
// via DoH). Pass nil to run in validation-only mode where no comparison is
// performed — useful during bootstrap before the relay is known.
func NewWebRTCLeakChecker(getExpectedIP ExpectedIPFunc) *WebRTCLeakChecker {
	return &WebRTCLeakChecker{
		getExpectedIP: getExpectedIP,
		stunServers:   defaultSTUNServers,
	}
}

// WithSTUNServers sets custom STUN servers (for testing or operator override).
func (c *WebRTCLeakChecker) WithSTUNServers(servers []string) *WebRTCLeakChecker {
	c.stunServers = servers
	return c
}

// CheckSTUNLeak sends a STUN Binding Request to the specified server via
// net.DialUDP and returns the IP address discovered via XOR-MAPPED-ADDRESS.
// The OS routes the UDP packet through the TUN interface (levoile0) because
// of the default route installed by Story 2.4 — no applicative relay needed.
func (c *WebRTCLeakChecker) CheckSTUNLeak(ctx context.Context, stunServer string) (*LeakResult, error) {
	result := &LeakResult{Server: stunServer}

	req := BuildBindingRequest()
	txnID := req[8:20] // 12-byte Transaction ID

	resp, err := sendBindingRequest(ctx, req, stunServer)
	if err != nil {
		return nil, err
	}

	// Validate response transaction ID.
	if len(resp) < 20 {
		return nil, fmt.Errorf("leakcheck: stun: response too short from %s", stunServer)
	}
	for i := 0; i < 12; i++ {
		if resp[8+i] != txnID[i] {
			return nil, fmt.Errorf("leakcheck: stun: transaction ID mismatch from %s", stunServer)
		}
	}

	ip, err := ParseXORMappedAddress(resp)
	if err != nil {
		return nil, fmt.Errorf("leakcheck: stun: parse %s: %w", stunServer, err)
	}

	result.IP = ip
	return result, nil
}

// sendBindingRequest sends a STUN Binding Request via net.DialUDP. The OS
// routes the packet through the default route — which, post-Story-2.4,
// points to levoile0 (the TUN interface).
func sendBindingRequest(ctx context.Context, req []byte, stunServer string) ([]byte, error) {
	deadline, ok := ctx.Deadline()
	if !ok {
		deadline = time.Now().Add(stunTimeout)
	}

	raddr, err := net.ResolveUDPAddr("udp", stunServer)
	if err != nil {
		return nil, fmt.Errorf("leakcheck: stun: resolve %s: %w", stunServer, err)
	}

	conn, err := net.DialUDP("udp", nil, raddr)
	if err != nil {
		return nil, fmt.Errorf("leakcheck: stun: dial %s: %w", stunServer, err)
	}
	defer conn.Close()

	conn.SetDeadline(deadline)

	if _, err := conn.Write(req); err != nil {
		return nil, fmt.Errorf("leakcheck: stun: write %s: %w", stunServer, err)
	}

	buf := make([]byte, 1500)
	n, err := conn.Read(buf)
	if err != nil {
		return nil, fmt.Errorf("leakcheck: stun: read %s: %w", stunServer, err)
	}

	return buf[:n], nil
}

// RunFullCheck executes CheckSTUNLeak on the configured STUN servers,
// compares each response with the expected relay IP (when getExpectedIP is
// non-nil), and returns a report.
//
// Status semantics (story 6.2):
//   - "ok"            : every successful STUN response matched ExpectedIP.
//   - "leak_detected" : at least one STUN response differed from ExpectedIP.
//     LeakReason classifies the cause (tun_capture_likely_down or
//     stun_ip_differs_from_relay).
//
// Safety contract:
//   - getExpectedIP returns nil IP → error, no comparison attempted.
//   - getExpectedIP errors → propagated; scheduler treats as transient.
//   - getExpectedIP is nil → validation-only mode (no comparison). Kept for
//     the bootstrap window before the relay is known.
func (c *WebRTCLeakChecker) RunFullCheck(ctx context.Context) (*FullLeakReport, error) {
	report := &FullLeakReport{
		Status: statusOK,
	}

	var expectedIP net.IP
	if c.getExpectedIP != nil {
		ip, err := c.getExpectedIP(ctx)
		if err != nil {
			return nil, fmt.Errorf("leakcheck: resolve relay ip: %w", err)
		}
		if ip == nil {
			return nil, fmt.Errorf("leakcheck: empty expected ip")
		}
		expectedIP = ip
		report.ExpectedIP = ip.String()
	}

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

		if expectedIP != nil && !result.IP.Equal(expectedIP) {
			result.Leaked = true
			report.Status = statusLeakDetected
			// Classify once: the first leak observed wins (subsequent
			// servers likely report the same IP, so the reason holds).
			if report.LeakReason == "" {
				report.LeakReason = ClassifyLeak(result.IP)
			}
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

// ClassifyLeak returns a short code describing why stunIP differs from the
// expected relay IP. Private/loopback IPs indicate the STUN request never
// reached the public internet (TUN pump broken or OS routing misconfig);
// other public IPs indicate traffic escaped the tunnel — typically the ISP
// path slipped past the kill-switch routes.
func ClassifyLeak(stunIP net.IP) string {
	if stunIP == nil {
		return LeakReasonStunIPDiffers
	}
	if stunIP.IsLoopback() || stunIP.IsPrivate() || stunIP.IsLinkLocalUnicast() || stunIP.IsUnspecified() {
		return LeakReasonTUNDown
	}
	return LeakReasonStunIPDiffers
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
