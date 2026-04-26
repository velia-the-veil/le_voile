//go:build linux

package dns

import (
	"context"
	"errors"
	"net"
	"testing"
	"time"
)

// freeUDPAddr finds a free UDP port on localhost for testing.
func freeUDPAddr(t *testing.T) string {
	t.Helper()
	conn, err := net.ListenPacket("udp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("find free port: %v", err)
	}
	addr := conn.LocalAddr().String()
	conn.Close()
	return addr
}

// echoQueryFunc returns the DNS payload as-is (simulates a DoH response).
func echoQueryFunc(_ context.Context, payload []byte) ([]byte, error) {
	return payload, nil
}

// errorQueryFunc always returns an error.
func errorQueryFunc(_ context.Context, _ []byte) ([]byte, error) {
	return nil, errors.New("dns: upstream error")
}

// startProxy starts a proxy in a goroutine and waits for readiness.
func startProxy(t *testing.T, p *Proxy, ctx context.Context) {
	t.Helper()
	errCh := make(chan error, 1)
	go func() {
		errCh <- p.Start(ctx)
	}()

	select {
	case <-p.Ready():
	case err := <-errCh:
		t.Fatalf("proxy failed to start: %v", err)
	case <-time.After(2 * time.Second):
		t.Fatal("proxy did not become ready within timeout")
	}
}

func TestProxy_NewProxy(t *testing.T) {
	addr := freeUDPAddr(t)
	p := NewProxy(addr, echoQueryFunc)

	if p == nil {
		t.Fatal("NewProxy returned nil")
	}
	if p.listenAddr != addr {
		t.Errorf("listenAddr = %q, want %q", p.listenAddr, addr)
	}
}

func TestProxy_StartStop(t *testing.T) {
	addr := freeUDPAddr(t)
	p := NewProxy(addr, echoQueryFunc)

	ctx, cancel := context.WithCancel(context.Background())

	errCh := make(chan error, 1)
	go func() {
		errCh <- p.Start(ctx)
	}()

	select {
	case <-p.Ready():
	case <-time.After(2 * time.Second):
		t.Fatal("proxy did not become ready")
	}

	cancel()

	select {
	case err := <-errCh:
		if err != nil {
			t.Fatalf("Start returned error: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("proxy did not stop within timeout")
	}
}

// makeDNSPayload creates a minimal valid DNS query payload (>= 12 bytes).
func makeDNSPayload() []byte {
	return []byte{
		0x00, 0x01, // ID
		0x01, 0x00, // Flags: standard query
		0x00, 0x01, // Questions: 1
		0x00, 0x00, // Answers: 0
		0x00, 0x00, // Authority: 0
		0x00, 0x00, // Additional: 0
		0x07, 'e', 'x', 'a', 'm', 'p', 'l', 'e',
		0x03, 'c', 'o', 'm',
		0x00,       // Root
		0x00, 0x01, // Type A
		0x00, 0x01, // Class IN
	}
}

func TestProxy_ForwardQuery(t *testing.T) {
	addr := freeUDPAddr(t)
	p := NewProxy(addr, echoQueryFunc)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	startProxy(t, p, ctx)

	dnsPayload := makeDNSPayload()

	conn, err := net.Dial("udp", addr)
	if err != nil {
		t.Fatalf("dial proxy: %v", err)
	}
	defer conn.Close()

	conn.SetDeadline(time.Now().Add(2 * time.Second))

	if _, err := conn.Write(dnsPayload); err != nil {
		t.Fatalf("write query: %v", err)
	}

	buf := make([]byte, 4096)
	n, err := conn.Read(buf)
	if err != nil {
		t.Fatalf("read response: %v", err)
	}

	if n != len(dnsPayload) {
		t.Errorf("response length = %d, want %d", n, len(dnsPayload))
	}

	for i := 0; i < n; i++ {
		if buf[i] != dnsPayload[i] {
			t.Errorf("response byte[%d] = %x, want %x", i, buf[i], dnsPayload[i])
			break
		}
	}
}

func TestProxy_QueryFuncError(t *testing.T) {
	addr := freeUDPAddr(t)
	p := NewProxy(addr, errorQueryFunc)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	startProxy(t, p, ctx)

	conn, err := net.Dial("udp", addr)
	if err != nil {
		t.Fatalf("dial proxy: %v", err)
	}
	defer conn.Close()

	conn.SetDeadline(time.Now().Add(300 * time.Millisecond))

	// Send a valid DNS-sized payload
	if _, err := conn.Write(makeDNSPayload()); err != nil {
		t.Fatalf("write query: %v", err)
	}

	buf := make([]byte, 4096)
	_, err = conn.Read(buf)
	if err == nil {
		t.Error("expected timeout error when queryFunc fails, got response")
	}

	var netErr net.Error
	if errors.As(err, &netErr) && !netErr.Timeout() {
		t.Errorf("expected timeout error, got: %v", err)
	}
}

func TestProxy_ConcurrentQueries(t *testing.T) {
	addr := freeUDPAddr(t)
	p := NewProxy(addr, echoQueryFunc)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	startProxy(t, p, ctx)

	const numQueries = 20
	results := make(chan error, numQueries)

	for i := 0; i < numQueries; i++ {
		go func(id int) {
			conn, err := net.Dial("udp", addr)
			if err != nil {
				results <- err
				return
			}
			defer conn.Close()

			conn.SetDeadline(time.Now().Add(2 * time.Second))

			// Build a valid DNS-sized payload (>= 12 bytes)
			payload := make([]byte, 14)
			payload[0] = byte(id)
			payload[1] = 0x01
			payload[2] = 0x01 // flags
			payload[5] = 0x01 // 1 question

			if _, err := conn.Write(payload); err != nil {
				results <- err
				return
			}

			buf := make([]byte, 4096)
			n, err := conn.Read(buf)
			if err != nil {
				results <- err
				return
			}

			if n != len(payload) {
				results <- errors.New("response size mismatch")
				return
			}

			results <- nil
		}(i)
	}

	for i := 0; i < numQueries; i++ {
		if err := <-results; err != nil {
			t.Errorf("concurrent query %d: %v", i, err)
		}
	}
}

func TestProxy_TooSmallPayload(t *testing.T) {
	addr := freeUDPAddr(t)
	p := NewProxy(addr, echoQueryFunc)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	startProxy(t, p, ctx)

	conn, err := net.Dial("udp", addr)
	if err != nil {
		t.Fatalf("dial proxy: %v", err)
	}
	defer conn.Close()

	conn.SetDeadline(time.Now().Add(300 * time.Millisecond))

	// Send a payload smaller than minimum DNS size — should be dropped
	if _, err := conn.Write([]byte{0x00, 0x01}); err != nil {
		t.Fatalf("write query: %v", err)
	}

	buf := make([]byte, 4096)
	_, err = conn.Read(buf)
	if err == nil {
		t.Error("expected timeout for sub-DNS-size payload, got response")
	}
}

func TestProxy_ReadyChannel(t *testing.T) {
	addr := freeUDPAddr(t)
	p := NewProxy(addr, echoQueryFunc)

	// Ready channel should not be closed before Start
	select {
	case <-p.Ready():
		t.Fatal("Ready() should not be closed before Start")
	default:
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	go func() {
		p.Start(ctx)
	}()

	select {
	case <-p.Ready():
		// OK — proxy is ready
	case <-time.After(2 * time.Second):
		t.Fatal("proxy did not signal readiness")
	}
}

// mockBlocklist implements BlocklistChecker for testing.
type mockBlocklist struct {
	blockedDomains map[string]bool
	ready          bool
}

func (m *mockBlocklist) IsBlocked(domain string) bool { return m.blockedDomains[domain] }
func (m *mockBlocklist) IsReady() bool                { return m.ready }

// makeSingleLabelDNSPayload creates a minimal valid DNS query for the single-label domain "foo".
func makeSingleLabelDNSPayload() []byte {
	return []byte{
		0x00, 0x01, // ID
		0x01, 0x00, // Flags: standard query
		0x00, 0x01, // Questions: 1
		0x00, 0x00, // Answers: 0
		0x00, 0x00, // Authority: 0
		0x00, 0x00, // Additional: 0
		0x03, 'f', 'o', 'o', // QNAME: "foo"
		0x00,       // Root
		0x00, 0x01, // Type A
		0x00, 0x01, // Class IN
	}
}

func TestExtractDomain_BasicQuery(t *testing.T) {
	got := extractDomain(makeDNSPayload())
	if got != "example.com" {
		t.Errorf("extractDomain = %q, want %q", got, "example.com")
	}
}

func TestExtractDomain_ShortPayload(t *testing.T) {
	// Exactly 12 bytes (minDNSSize) — no room for QNAME.
	payload := make([]byte, minDNSSize)
	got := extractDomain(payload)
	if got != "" {
		t.Errorf("extractDomain(12-byte payload) = %q, want %q", got, "")
	}

	// Shorter than minDNSSize.
	got = extractDomain(payload[:8])
	if got != "" {
		t.Errorf("extractDomain(8-byte payload) = %q, want %q", got, "")
	}
}

func TestExtractDomain_SingleLabel(t *testing.T) {
	got := extractDomain(makeSingleLabelDNSPayload())
	if got != "foo" {
		t.Errorf("extractDomain = %q, want %q", got, "foo")
	}
}

func TestBuildNXDOMAINResponse_ValidQuery(t *testing.T) {
	query := makeDNSPayload()
	resp := buildNXDOMAINResponse(query)

	if resp == nil {
		t.Fatal("buildNXDOMAINResponse returned nil for valid query")
	}

	// QR bit must be set (byte[2] bit 7).
	if resp[2]&0x80 == 0 {
		t.Errorf("QR bit not set in byte[2]: got 0x%02X", resp[2])
	}

	// RCODE must be 3 (NXDOMAIN) in lower nibble of byte[3].
	if resp[3]&0x0F != 3 {
		t.Errorf("RCODE = %d, want 3", resp[3]&0x0F)
	}

	// ANCOUNT (bytes 6-7) must be 0.
	if resp[6] != 0 || resp[7] != 0 {
		t.Errorf("ANCOUNT = %d, want 0", int(resp[6])<<8|int(resp[7]))
	}
}

func TestBuildNXDOMAINResponse_TooShort(t *testing.T) {
	payload := make([]byte, minDNSSize-1)
	resp := buildNXDOMAINResponse(payload)
	if resp != nil {
		t.Errorf("buildNXDOMAINResponse(too-short) = non-nil, want nil")
	}
}

func TestProxy_BlocklistFiltering_Blocked(t *testing.T) {
	addr := freeUDPAddr(t)
	p := NewProxy(addr, echoQueryFunc)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	startProxy(t, p, ctx)

	bl := &mockBlocklist{
		blockedDomains: map[string]bool{"example.com": true},
		ready:          true,
	}
	p.SetBlocklist(bl)

	conn, err := net.Dial("udp", addr)
	if err != nil {
		t.Fatalf("dial proxy: %v", err)
	}
	defer conn.Close()

	conn.SetDeadline(time.Now().Add(2 * time.Second))

	if _, err := conn.Write(makeDNSPayload()); err != nil {
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
	if buf[3]&0x0F != 3 {
		t.Errorf("RCODE = %d, want 3 (NXDOMAIN)", buf[3]&0x0F)
	}
}

func TestProxy_BlocklistFiltering_NotBlocked(t *testing.T) {
	addr := freeUDPAddr(t)
	p := NewProxy(addr, echoQueryFunc)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	startProxy(t, p, ctx)

	bl := &mockBlocklist{
		blockedDomains: map[string]bool{},
		ready:          true,
	}
	p.SetBlocklist(bl)

	payload := makeDNSPayload()

	conn, err := net.Dial("udp", addr)
	if err != nil {
		t.Fatalf("dial proxy: %v", err)
	}
	defer conn.Close()

	conn.SetDeadline(time.Now().Add(2 * time.Second))

	if _, err := conn.Write(payload); err != nil {
		t.Fatalf("write query: %v", err)
	}

	buf := make([]byte, 4096)
	n, err := conn.Read(buf)
	if err != nil {
		t.Fatalf("read response: %v", err)
	}

	// echoQueryFunc returns payload unchanged — response should equal query.
	if n != len(payload) {
		t.Errorf("response length = %d, want %d", n, len(payload))
	}
	for i := 0; i < n; i++ {
		if buf[i] != payload[i] {
			t.Errorf("response byte[%d] = 0x%02X, want 0x%02X", i, buf[i], payload[i])
			break
		}
	}
}

func TestProxy_BlocklistFiltering_NotReady(t *testing.T) {
	addr := freeUDPAddr(t)
	p := NewProxy(addr, echoQueryFunc)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	startProxy(t, p, ctx)

	// Blocklist reports IsReady()=false, but would block if ready.
	bl := &mockBlocklist{
		blockedDomains: map[string]bool{"example.com": true},
		ready:          false,
	}
	p.SetBlocklist(bl)

	payload := makeDNSPayload()

	conn, err := net.Dial("udp", addr)
	if err != nil {
		t.Fatalf("dial proxy: %v", err)
	}
	defer conn.Close()

	conn.SetDeadline(time.Now().Add(2 * time.Second))

	if _, err := conn.Write(payload); err != nil {
		t.Fatalf("write query: %v", err)
	}

	buf := make([]byte, 4096)
	n, err := conn.Read(buf)
	if err != nil {
		t.Fatalf("read response (should be forwarded): %v", err)
	}

	// Should be echo, not NXDOMAIN.
	if n != len(payload) {
		t.Errorf("response length = %d, want %d", n, len(payload))
	}
}

func TestProxy_BlocklistFiltering_NilBlocklist(t *testing.T) {
	addr := freeUDPAddr(t)
	p := NewProxy(addr, echoQueryFunc)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	startProxy(t, p, ctx)

	// Set then clear the blocklist.
	bl := &mockBlocklist{
		blockedDomains: map[string]bool{"example.com": true},
		ready:          true,
	}
	p.SetBlocklist(bl)
	p.SetBlocklist(nil)

	payload := makeDNSPayload()

	conn, err := net.Dial("udp", addr)
	if err != nil {
		t.Fatalf("dial proxy: %v", err)
	}
	defer conn.Close()

	conn.SetDeadline(time.Now().Add(2 * time.Second))

	if _, err := conn.Write(payload); err != nil {
		t.Fatalf("write query: %v", err)
	}

	buf := make([]byte, 4096)
	n, err := conn.Read(buf)
	if err != nil {
		t.Fatalf("read response (should be forwarded after nil): %v", err)
	}

	if n != len(payload) {
		t.Errorf("response length = %d, want %d", n, len(payload))
	}
}

func TestProxy_SetBlocklist_ThreadSafe(t *testing.T) {
	addr := freeUDPAddr(t)
	p := NewProxy(addr, echoQueryFunc)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	startProxy(t, p, ctx)

	// Use a blocklist that actually blocks "example.com" to exercise the
	// blocking path (extractDomain + IsBlocked + buildNXDOMAINResponse)
	// under concurrent SetBlocklist toggling.
	bl := &mockBlocklist{
		blockedDomains: map[string]bool{"example.com": true},
		ready:          true,
	}

	// Spawn concurrent SetBlocklist calls alongside query traffic.
	done := make(chan struct{})
	go func() {
		defer close(done)
		for i := 0; i < 50; i++ {
			if i%2 == 0 {
				p.SetBlocklist(bl)
			} else {
				p.SetBlocklist(nil)
			}
		}
	}()

	// Send queries for a blocked domain concurrently — should not race.
	// Responses will be either echo (forwarded) or NXDOMAIN (blocked),
	// depending on the current blocklist state.
	results := make(chan error, 10)
	for i := 0; i < 10; i++ {
		go func() {
			conn, err := net.Dial("udp", addr)
			if err != nil {
				results <- err
				return
			}
			defer conn.Close()
			conn.SetDeadline(time.Now().Add(2 * time.Second))
			if _, err := conn.Write(makeDNSPayload()); err != nil {
				results <- err
				return
			}
			buf := make([]byte, 4096)
			_, err = conn.Read(buf)
			results <- err
		}()
	}

	for i := 0; i < 10; i++ {
		if err := <-results; err != nil {
			t.Errorf("concurrent query error: %v", err)
		}
	}

	<-done
}
