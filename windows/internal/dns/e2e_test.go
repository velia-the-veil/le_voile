//go:build e2e

package dns

import (
	"context"
	"net"
	"os"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// TestE2E_DNSProxyResolution starts a DNS proxy on an ephemeral port with a
// mock DoH upstream and verifies that a DNS query is forwarded and a valid
// response is returned.
func TestE2E_DNSProxyResolution(t *testing.T) {
	if os.Getenv("E2E") != "1" {
		t.Skip("set E2E=1 to run")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	addr := freeUDPAddr(t)

	// Mock DoH upstream: returns a valid DNS response with one A record.
	mockUpstream := func(_ context.Context, payload []byte) ([]byte, error) {
		resp := make([]byte, len(payload)+16)
		copy(resp, payload)
		resp[2] |= 0x80 // QR = response
		resp[6] = 0x00
		resp[7] = 0x01 // ANCOUNT = 1

		off := len(payload)
		resp = resp[:off+16]
		resp[off+0] = 0xC0  // name: pointer to QNAME
		resp[off+1] = 0x0C  // offset 12
		resp[off+2] = 0x00  // Type A
		resp[off+3] = 0x01
		resp[off+4] = 0x00  // Class IN
		resp[off+5] = 0x01
		resp[off+6] = 0x00  // TTL 60s
		resp[off+7] = 0x00
		resp[off+8] = 0x00
		resp[off+9] = 0x3C
		resp[off+10] = 0x00 // RDLENGTH 4
		resp[off+11] = 0x04
		resp[off+12] = 93 // 93.184.216.34
		resp[off+13] = 184
		resp[off+14] = 216
		resp[off+15] = 34

		return resp, nil
	}

	p := NewProxy(addr, mockUpstream)
	startProxy(t, p, ctx)

	conn, err := net.Dial("udp", addr)
	if err != nil {
		t.Fatalf("dial proxy: %v", err)
	}
	defer conn.Close()
	conn.SetDeadline(time.Now().Add(5 * time.Second))

	query := makeDNSPayload() // example.com A
	if _, err := conn.Write(query); err != nil {
		t.Fatalf("write query: %v", err)
	}

	buf := make([]byte, 4096)
	n, err := conn.Read(buf)
	if err != nil {
		t.Fatalf("read response: %v", err)
	}

	if n < minDNSSize {
		t.Fatalf("response too short: %d bytes", n)
	}

	if buf[2]&0x80 == 0 {
		t.Error("QR bit not set in response")
	}

	ancount := int(buf[6])<<8 | int(buf[7])
	if ancount == 0 {
		t.Error("expected ANCOUNT > 0")
	}

	// Verify the A record answer IP.
	ansOff := len(query)
	if n >= ansOff+16 {
		ip := net.IPv4(buf[ansOff+12], buf[ansOff+13], buf[ansOff+14], buf[ansOff+15])
		if ip.String() != "93.184.216.34" {
			t.Errorf("answer IP = %s, want 93.184.216.34", ip.String())
		}
	}

	t.Logf("DNS proxy resolution OK: %d bytes, %d answers", n, ancount)
}

// TestE2E_KillSwitchActivation verifies that the kill switch blocks all DNS
// resolution within 100ms (NFR15) and that a burst of 50 concurrent queries
// after activation yields zero successful resolutions.
func TestE2E_KillSwitchActivation(t *testing.T) {
	if os.Getenv("E2E") != "1" {
		t.Skip("set E2E=1 to run")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	addr := freeUDPAddr(t)
	proxyCtx, proxyCancel := context.WithCancel(ctx)

	p := NewProxy(addr, echoQueryFunc)
	startProxy(t, p, proxyCtx)

	// Verify proxy works before activation.
	func() {
		conn, err := net.Dial("udp", addr)
		if err != nil {
			t.Fatalf("dial proxy: %v", err)
		}
		defer conn.Close()
		conn.SetDeadline(time.Now().Add(2 * time.Second))

		if _, err := conn.Write(makeDNSPayload()); err != nil {
			t.Fatalf("write pre-activation: %v", err)
		}
		buf := make([]byte, 4096)
		if _, err := conn.Read(buf); err != nil {
			t.Fatalf("read pre-activation: %v", err)
		}
	}()

	// Kill switch with mock DNS manager (original already saved).
	mock := &mockDNSManager{originalAddr: "8.8.8.8"}
	ks := NewKillSwitch(mock, func() { proxyCancel() }, func(_ context.Context) error { return nil })
	ks.SetForceResolver(func(_ context.Context, _ string) error { return nil })

	// (a) Measure activation delay — NFR15: < 100ms.
	start := time.Now()
	if err := ks.Activate(ctx); err != nil {
		t.Fatalf("Activate: %v", err)
	}
	activationTime := time.Since(start)

	if activationTime > 100*time.Millisecond {
		t.Errorf("kill switch activation took %v, NFR15 requires < 100ms", activationTime)
	}
	t.Logf("kill switch activation time: %v", activationTime)

	// Wait for proxy to fully release the UDP socket instead of a fixed sleep.
	// Try to bind the same address — success means the proxy has shut down.
	proxyReleased := false
	for i := 0; i < 50; i++ {
		ln, err := net.ListenPacket("udp", addr)
		if err == nil {
			ln.Close()
			proxyReleased = true
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if !proxyReleased {
		t.Fatal("proxy did not release UDP socket within 500ms after kill switch activation")
	}

	// (b) Burst test: 50 concurrent DNS queries — expect 0 successes.
	var succeeded atomic.Int64
	var wg sync.WaitGroup
	query := makeDNSPayload()

	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			c, err := net.Dial("udp", addr)
			if err != nil {
				return
			}
			defer c.Close()
			c.SetDeadline(time.Now().Add(200 * time.Millisecond))

			if _, err := c.Write(query); err != nil {
				return
			}
			b := make([]byte, 4096)
			if _, err := c.Read(b); err == nil {
				succeeded.Add(1)
			}
		}()
	}

	wg.Wait()

	if s := succeeded.Load(); s > 0 {
		t.Errorf("burst test: %d/50 DNS queries succeeded after kill switch (expected 0)", s)
	}
	t.Logf("burst test: 0/50 succeeded — kill switch effective")
}
