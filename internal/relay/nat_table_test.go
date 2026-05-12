package relay

import (
	"context"
	"encoding/binary"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// --- Mock helpers ---

// mockConn implements net.Conn for testing. Reads from readBuf, writes to writeBuf.
type mockConn struct {
	readBuf  chan []byte
	writeBuf chan []byte
	closed   atomic.Bool
	closeCh  chan struct{}
}

func newMockConn() *mockConn {
	return &mockConn{
		readBuf:  make(chan []byte, 64),
		writeBuf: make(chan []byte, 64),
		closeCh:  make(chan struct{}),
	}
}

func (c *mockConn) Read(b []byte) (int, error) {
	select {
	case data, ok := <-c.readBuf:
		if !ok {
			return 0, io.EOF
		}
		n := copy(b, data)
		return n, nil
	case <-c.closeCh:
		return 0, net.ErrClosed
	}
}

func (c *mockConn) Write(b []byte) (int, error) {
	if c.closed.Load() {
		return 0, net.ErrClosed
	}
	data := make([]byte, len(b))
	copy(data, b)
	select {
	case c.writeBuf <- data:
	default:
	}
	return len(b), nil
}

func (c *mockConn) Close() error {
	if c.closed.CompareAndSwap(false, true) {
		close(c.closeCh)
	}
	return nil
}

func (c *mockConn) LocalAddr() net.Addr                { return &net.TCPAddr{} }
func (c *mockConn) RemoteAddr() net.Addr               { return &net.TCPAddr{} }
func (c *mockConn) SetDeadline(time.Time) error        { return nil }
func (c *mockConn) SetReadDeadline(time.Time) error    { return nil }
func (c *mockConn) SetWriteDeadline(time.Time) error   { return nil }

// testNAT creates a NAT instance with mock dialers and a controllable clock.
func testNAT(t *testing.T, clock *func() time.Time) (*NAT, *mockConn) {
	t.Helper()
	mc := newMockConn()
	now := time.Now()
	clockFn := func() time.Time { return now }
	if clock != nil {
		*clock = clockFn
	}
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	nat := NewNAT(net.IPv4(1, 2, 3, 4),
		WithClock(clockFn),
		WithContext(ctx),
		WithDialTCP(func(_, _ *net.TCPAddr) (net.Conn, error) { return mc, nil }),
		WithDialUDP(func(_, _ *net.UDPAddr) (net.Conn, error) { return mc, nil }),
	)
	t.Cleanup(func() { nat.Shutdown(context.Background()) })
	return nat, mc
}

// testNATWithClock creates a NAT with a mutable clock reference.
func testNATWithClock(t *testing.T) (*NAT, *mockConn, *atomic.Value) {
	t.Helper()
	mc := newMockConn()
	clockVal := &atomic.Value{}
	clockVal.Store(time.Now())

	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)

	nat := NewNAT(net.IPv4(1, 2, 3, 4),
		WithClock(func() time.Time { return clockVal.Load().(time.Time) }),
		WithContext(ctx),
		WithDialTCP(func(_, _ *net.TCPAddr) (net.Conn, error) { return mc, nil }),
		WithDialUDP(func(_, _ *net.UDPAddr) (net.Conn, error) { return mc, nil }),
	)
	t.Cleanup(func() { nat.Shutdown(context.Background()) })
	return nat, mc, clockVal
}

const testSession SessionID = "test-session@12345"

func makeUDPPacket(srcIP, dstIP net.IP, srcPort, dstPort uint16, payload []byte) []byte {
	return buildUDPPacket(srcIP, dstIP, srcPort, dstPort, payload)
}

func makeTCPSYN(srcIP, dstIP net.IP, srcPort, dstPort uint16) []byte {
	return buildTCPPacket(srcIP, dstIP, srcPort, dstPort, nil, tcpFlagSYN, 1000, 0, 65535)
}

func makeTCPData(srcIP, dstIP net.IP, srcPort, dstPort uint16, data []byte, seq, ack uint32) []byte {
	return buildTCPPacket(srcIP, dstIP, srcPort, dstPort, data, tcpFlagPSH|tcpFlagACK, seq, ack, 65535)
}

// --- Tests ---

func TestNAT_TranslateAllocatesPort(t *testing.T) {
	nat, _ := testNAT(t, nil)

	pkt := makeUDPPacket(net.IPv4(10, 0, 0, 1), net.IPv4(8, 8, 8, 8), 1234, 53, []byte("dns"))

	result, err := nat.Translate(testSession, pkt)
	if err != nil {
		t.Fatalf("Translate: %v", err)
	}
	if result == nil {
		t.Fatal("result is nil")
	}

	// Verify source IP rewritten to relay IP.
	p, _ := parseIPv4(result)
	if !p.srcIP.Equal(net.IPv4(1, 2, 3, 4)) {
		t.Errorf("srcIP=%v, want 1.2.3.4", p.srcIP)
	}
	if p.srcPort < NATPortRangeMin || p.srcPort > NATPortRangeMax {
		t.Errorf("natPort=%d out of range", p.srcPort)
	}

	stats := nat.Stats()
	if stats.Entries != 1 || stats.PortsUsed != 1 {
		t.Errorf("stats: entries=%d ports=%d", stats.Entries, stats.PortsUsed)
	}
}

func TestNAT_TranslateReusesEntryForSameTuple(t *testing.T) {
	nat, _ := testNAT(t, nil)

	pkt := makeUDPPacket(net.IPv4(10, 0, 0, 1), net.IPv4(8, 8, 8, 8), 1234, 53, []byte("q"))

	_, err := nat.Translate(testSession, pkt)
	if err != nil {
		t.Fatal(err)
	}

	// Get the allocated port.
	p1, _ := parseIPv4(pkt)
	firstPort := p1.srcPort

	// Send 999 more packets on the same 5-tuple.
	for i := 0; i < 999; i++ {
		pkt2 := makeUDPPacket(net.IPv4(10, 0, 0, 1), net.IPv4(8, 8, 8, 8), 1234, 53, []byte("q"))
		_, err := nat.Translate(testSession, pkt2)
		if err != nil {
			t.Fatalf("iter %d: %v", i, err)
		}
		p2, _ := parseIPv4(pkt2)
		if p2.srcPort != firstPort {
			t.Fatalf("iter %d: port changed to %d", i, p2.srcPort)
		}
	}

	stats := nat.Stats()
	if stats.Entries != 1 {
		t.Errorf("entries=%d, want 1", stats.Entries)
	}
}

func TestNAT_ReverseRoutesBackToOriginalClient(t *testing.T) {
	nat, _ := testNAT(t, nil)

	// Create entry via Translate.
	pkt := makeUDPPacket(net.IPv4(10, 0, 0, 1), net.IPv4(8, 8, 8, 8), 1234, 53, []byte("q"))
	_, err := nat.Translate(testSession, pkt)
	if err != nil {
		t.Fatal(err)
	}
	p, _ := parseIPv4(pkt)
	natPort := p.srcPort

	// Build a "response" packet from 8.8.8.8:53 to relayIP:natPort.
	resp := buildUDPPacket(net.IPv4(8, 8, 8, 8), net.IPv4(1, 2, 3, 4), 53, natPort, []byte("r"))

	result, sid, err := nat.Reverse(resp)
	if err != nil {
		t.Fatalf("Reverse: %v", err)
	}
	if sid != testSession {
		t.Errorf("session=%q, want %q", sid, testSession)
	}

	rp, _ := parseIPv4(result)
	if !rp.dstIP.Equal(net.IPv4(10, 0, 0, 1)) {
		t.Errorf("dstIP=%v, want 10.0.0.1", rp.dstIP)
	}
	if rp.dstPort != 1234 {
		t.Errorf("dstPort=%d, want 1234", rp.dstPort)
	}
}

func TestNAT_TTLEvictionTCP300s(t *testing.T) {
	nat, _, clockVal := testNATWithClock(t)

	pkt := makeTCPSYN(net.IPv4(10, 0, 0, 1), net.IPv4(93, 184, 216, 34), 4567, 80)
	_, err := nat.Translate(testSession, pkt)
	if err != nil {
		t.Fatal(err)
	}
	if nat.Stats().Entries != 1 {
		t.Fatal("entry not created")
	}

	// Advance clock past TCP TTL.
	clockVal.Store(clockVal.Load().(time.Time).Add(NATTTLTCP + time.Second))
	nat.sweepOnce(clockVal.Load().(time.Time))

	if nat.Stats().Entries != 0 {
		t.Errorf("entries=%d after TCP TTL eviction, want 0", nat.Stats().Entries)
	}
}

func TestNAT_TTLEvictionUDP120s(t *testing.T) {
	nat, _, clockVal := testNATWithClock(t)

	pkt := makeUDPPacket(net.IPv4(10, 0, 0, 1), net.IPv4(8, 8, 8, 8), 1234, 53, []byte("q"))
	_, err := nat.Translate(testSession, pkt)
	if err != nil {
		t.Fatal(err)
	}

	// Before TTL — should not evict.
	clockVal.Store(clockVal.Load().(time.Time).Add(NATTTLUDP - time.Second))
	nat.sweepOnce(clockVal.Load().(time.Time))
	if nat.Stats().Entries != 1 {
		t.Fatal("evicted too early")
	}

	// After TTL — should evict.
	clockVal.Store(clockVal.Load().(time.Time).Add(2 * time.Second))
	nat.sweepOnce(clockVal.Load().(time.Time))
	if nat.Stats().Entries != 0 {
		t.Errorf("entries=%d after UDP TTL eviction, want 0", nat.Stats().Entries)
	}
}

func TestNAT_PortExhaustionTriggersSweep(t *testing.T) {
	mc := newMockConn()
	now := time.Now()
	clockVal := &atomic.Value{}
	clockVal.Store(now)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Create NAT with tiny port pool (10 ports).
	nat := &NAT{
		relayIP:   net.IPv4(1, 2, 3, 4).To4(),
		pool:      newPortPool(10000, 10009), // only 10 ports
		clock:     func() time.Time { return clockVal.Load().(time.Time) },
		parentCtx: ctx,
		dialTCP:   func(_, _ *net.TCPAddr) (net.Conn, error) { return mc, nil },
		dialUDP:   func(_, _ *net.UDPAddr) (net.Conn, error) { return mc, nil },
		sessions:  make(map[SessionID]chan []byte),
	}
	ctx2, cancel2 := context.WithCancel(ctx)
	nat.cancel = cancel2
	go nat.sweepLoop(ctx2)
	defer func() {
		cancel2()
		nat.Shutdown(context.Background())
	}()

	// Fill all 10 ports.
	for i := 0; i < 10; i++ {
		pkt := makeUDPPacket(net.IPv4(10, 0, 0, 1), net.IPv4(8, 8, 8, 8), uint16(2000+i), 53, []byte("q"))
		_, err := nat.Translate(testSession, pkt)
		if err != nil {
			t.Fatalf("fill port %d: %v", i, err)
		}
	}
	if nat.Stats().Entries != 10 {
		t.Fatalf("entries=%d, want 10", nat.Stats().Entries)
	}

	// Next allocation should fail initially, trigger sweep, but entries are fresh so still fail.
	pkt := makeUDPPacket(net.IPv4(10, 0, 0, 1), net.IPv4(8, 8, 8, 8), 3000, 53, []byte("q"))
	_, err := nat.Translate(testSession, pkt)
	if err != ErrNATPortExhausted {
		t.Fatalf("expected ErrNATPortExhausted, got %v", err)
	}

	// Advance clock past UDP TTL so sweep can reclaim.
	clockVal.Store(now.Add(NATTTLUDP + time.Second))
	pkt2 := makeUDPPacket(net.IPv4(10, 0, 0, 1), net.IPv4(8, 8, 8, 8), 3001, 53, []byte("q"))
	_, err = nat.Translate(testSession, pkt2)
	if err != nil {
		t.Fatalf("after TTL sweep: %v", err)
	}
}

func TestNAT_SSRFBlocked(t *testing.T) {
	nat, _ := testNAT(t, nil)

	tests := []struct {
		name string
		dst  net.IP
	}{
		{"loopback", net.IPv4(127, 0, 0, 1)},
		{"10.x", net.IPv4(10, 0, 0, 1)},
		{"192.168.x", net.IPv4(192, 168, 1, 1)},
		{"172.16.x", net.IPv4(172, 16, 0, 1)},
		{"link-local", net.IPv4(169, 254, 1, 1)},
		{"broadcast", net.IPv4(255, 255, 255, 255)},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			pkt := makeUDPPacket(net.IPv4(10, 0, 0, 1), tc.dst, 1234, 53, []byte("q"))
			_, err := nat.Translate(testSession, pkt)
			if err != ErrSSRFBlocked {
				t.Errorf("dst=%v: got %v, want ErrSSRFBlocked", tc.dst, err)
			}
		})
	}

	// Verify no entries/ports consumed.
	stats := nat.Stats()
	if stats.Entries != 0 || stats.PortsUsed != 0 {
		t.Errorf("SSRF leaked resources: entries=%d ports=%d", stats.Entries, stats.PortsUsed)
	}
}

func TestNAT_ShutdownClosesAllConns(t *testing.T) {
	mc := newMockConn()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	nat := NewNAT(net.IPv4(1, 2, 3, 4),
		WithContext(ctx),
		WithDialTCP(func(_, _ *net.TCPAddr) (net.Conn, error) { return mc, nil }),
		WithDialUDP(func(_, _ *net.UDPAddr) (net.Conn, error) { return newMockConn(), nil }),
	)

	// Create a few entries.
	for i := 0; i < 5; i++ {
		pkt := makeUDPPacket(net.IPv4(10, 0, 0, 1), net.IPv4(8, 8, 8, byte(i+1)), uint16(1000+i), 53, []byte("q"))
		_, err := nat.Translate(testSession, pkt)
		if err != nil {
			t.Fatal(err)
		}
	}
	if nat.Stats().Entries != 5 {
		t.Fatalf("entries=%d", nat.Stats().Entries)
	}

	nat.Shutdown(context.Background())

	if nat.Stats().Entries != 0 {
		t.Errorf("entries=%d after shutdown, want 0", nat.Stats().Entries)
	}
	if nat.Stats().PortsUsed != 0 {
		t.Errorf("ports=%d after shutdown, want 0", nat.Stats().PortsUsed)
	}
}

func TestNAT_RaceFree(t *testing.T) {
	// This test is meaningful with -race flag.
	mc := newMockConn()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	connIdx := atomic.Int64{}
	conns := make([]*mockConn, 1000)
	for i := range conns {
		conns[i] = newMockConn()
	}

	nat := NewNAT(net.IPv4(1, 2, 3, 4),
		WithContext(ctx),
		WithDialTCP(func(_, _ *net.TCPAddr) (net.Conn, error) {
			idx := connIdx.Add(1) - 1
			if int(idx) < len(conns) {
				return conns[idx], nil
			}
			return mc, nil
		}),
		WithDialUDP(func(_, _ *net.UDPAddr) (net.Conn, error) {
			idx := connIdx.Add(1) - 1
			if int(idx) < len(conns) {
				return conns[idx], nil
			}
			return mc, nil
		}),
	)
	defer nat.Shutdown(context.Background())

	var wg sync.WaitGroup
	for g := 0; g < 100; g++ {
		wg.Add(1)
		go func(gid int) {
			defer wg.Done()
			for i := 0; i < 10; i++ {
				srcPort := uint16(2000 + gid*10 + i)
				pkt := makeUDPPacket(net.IPv4(10, 0, 0, 1), net.IPv4(8, 8, 8, 8), srcPort, 53, []byte("q"))
				nat.Translate(testSession, pkt)

				nat.Stats()
			}
		}(g)
	}
	wg.Wait()
}

func TestNAT_PacketForwarder_Integration(t *testing.T) {
	mc := newMockConn()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	nat := NewNAT(net.IPv4(1, 2, 3, 4),
		WithContext(ctx),
		WithDialUDP(func(_, _ *net.UDPAddr) (net.Conn, error) { return mc, nil }),
	)
	defer nat.Shutdown(context.Background())

	session := TunnelSession{
		ClientIPHash: "abc123",
		OpenedAt:     time.Now(),
	}

	outCh, cleanup := nat.OpenSession(ctx, session)
	defer cleanup()

	// Forward a UDP packet.
	pkt := makeUDPPacket(net.IPv4(10, 0, 0, 1), net.IPv4(8, 8, 8, 8), 1234, 53, []byte("query"))
	err := nat.Forward(ctx, session, pkt)
	if err != nil {
		t.Fatalf("Forward: %v", err)
	}

	// Verify data was written to mock conn.
	select {
	case data := <-mc.writeBuf:
		if string(data) != "query" {
			t.Errorf("written=%q, want 'query'", data)
		}
	case <-time.After(time.Second):
		t.Fatal("timeout waiting for write")
	}

	// Simulate response from destination.
	mc.readBuf <- []byte("response")

	// Read reverse packet from outCh.
	select {
	case rpkt := <-outCh:
		rp, err := parseIPv4(rpkt)
		if err != nil {
			t.Fatalf("parse reverse: %v", err)
		}
		if !rp.srcIP.Equal(net.IPv4(8, 8, 8, 8)) {
			t.Errorf("reverse srcIP=%v", rp.srcIP)
		}
		if !rp.dstIP.Equal(net.IPv4(10, 0, 0, 1)) {
			t.Errorf("reverse dstIP=%v", rp.dstIP)
		}
		payload := rp.payload(rpkt)
		if string(payload) != "response" {
			t.Errorf("reverse payload=%q", payload)
		}
	case <-time.After(time.Second):
		t.Fatal("timeout waiting for reverse packet")
	}
}

func TestNAT_TCP_SYNHandshake(t *testing.T) {
	mc := newMockConn()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	nat := NewNAT(net.IPv4(1, 2, 3, 4),
		WithContext(ctx),
		WithDialTCP(func(_, _ *net.TCPAddr) (net.Conn, error) { return mc, nil }),
	)
	defer nat.Shutdown(context.Background())

	session := TunnelSession{
		ClientIPHash: "tcp-test",
		OpenedAt:     time.Now(),
	}
	outCh, cleanup := nat.OpenSession(ctx, session)
	defer cleanup()

	// Send SYN.
	syn := makeTCPSYN(net.IPv4(10, 0, 0, 1), net.IPv4(93, 184, 216, 34), 4567, 80)
	err := nat.Forward(ctx, session, syn)
	if err != nil {
		t.Fatalf("Forward SYN: %v", err)
	}

	// Should receive SYN-ACK.
	select {
	case rpkt := <-outCh:
		rp, err := parseIPv4(rpkt)
		if err != nil {
			t.Fatalf("parse SYN-ACK: %v", err)
		}
		if rp.tcpFlags != tcpFlagSYN|tcpFlagACK {
			t.Errorf("flags=%02x, want SYN|ACK", rp.tcpFlags)
		}
		if rp.tcpAck != 1001 { // ISN(1000) + 1
			t.Errorf("ack=%d, want 1001", rp.tcpAck)
		}
		if !rp.srcIP.Equal(net.IPv4(93, 184, 216, 34)) {
			t.Errorf("srcIP=%v, want destination", rp.srcIP)
		}
	case <-time.After(time.Second):
		t.Fatal("timeout waiting for SYN-ACK")
	}
}

func TestNAT_TCP_DataForward(t *testing.T) {
	mc := newMockConn()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	nat := NewNAT(net.IPv4(1, 2, 3, 4),
		WithContext(ctx),
		WithDialTCP(func(_, _ *net.TCPAddr) (net.Conn, error) { return mc, nil }),
	)
	defer nat.Shutdown(context.Background())

	session := TunnelSession{
		ClientIPHash: "tcp-data",
		OpenedAt:     time.Now(),
	}
	outCh, cleanup := nat.OpenSession(ctx, session)
	defer cleanup()

	// SYN.
	syn := makeTCPSYN(net.IPv4(10, 0, 0, 1), net.IPv4(93, 184, 216, 34), 4567, 80)
	nat.Forward(ctx, session, syn)
	<-outCh // consume SYN-ACK

	// Get the relay ISN from the SYN-ACK — we know ack=1001 from the SYN.
	// Send data: seq=1001, ack=relayISN+1
	data := makeTCPData(net.IPv4(10, 0, 0, 1), net.IPv4(93, 184, 216, 34),
		4567, 80, []byte("GET / HTTP/1.1\r\n"), 1001, 5001)
	err := nat.Forward(ctx, session, data)
	if err != nil {
		t.Fatalf("Forward data: %v", err)
	}

	// Verify payload forwarded to destination.
	select {
	case written := <-mc.writeBuf:
		if string(written) != "GET / HTTP/1.1\r\n" {
			t.Errorf("forwarded=%q", written)
		}
	case <-time.After(time.Second):
		t.Fatal("timeout waiting for forwarded data")
	}

	// Should receive ACK.
	select {
	case rpkt := <-outCh:
		rp, _ := parseIPv4(rpkt)
		if rp.tcpFlags != tcpFlagACK {
			t.Errorf("expected ACK, got flags=%02x", rp.tcpFlags)
		}
	case <-time.After(time.Second):
		t.Fatal("timeout waiting for ACK")
	}
}

func TestNAT_StatsO1(t *testing.T) {
	nat, _ := testNAT(t, nil)

	// Create some entries.
	for i := 0; i < 100; i++ {
		pkt := makeUDPPacket(net.IPv4(10, 0, 0, 1), net.IPv4(8, 8, 8, 8), uint16(1000+i), 53, []byte("q"))
		nat.Translate(testSession, pkt)
	}

	stats := nat.Stats()
	if stats.Entries != 100 || stats.PortsUsed != 100 {
		t.Errorf("stats: entries=%d ports=%d", stats.Entries, stats.PortsUsed)
	}
}

func TestNAT_PortPool_NoDuplicates(t *testing.T) {
	pool := newPortPool(10000, 10099) // 100 ports
	seen := make(map[uint16]bool)

	for i := 0; i < 100; i++ {
		port, err := pool.allocate()
		if err != nil {
			t.Fatalf("allocate %d: %v", i, err)
		}
		if seen[port] {
			t.Fatalf("duplicate port %d at iteration %d", port, i)
		}
		seen[port] = true
	}

	_, err := pool.allocate()
	if err != ErrNATPortExhausted {
		t.Fatalf("expected exhausted, got %v", err)
	}

	// Release one and re-allocate.
	pool.release(10050)
	port, err := pool.allocate()
	if err != nil {
		t.Fatal(err)
	}
	if port != 10050 {
		t.Errorf("released port not re-allocated: got %d", port)
	}
}

func BenchmarkNATAllocate(b *testing.B) {
	pool := newPortPool(NATPortRangeMin, NATPortRangeMax)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		port, err := pool.allocate()
		if err != nil {
			pool = newPortPool(NATPortRangeMin, NATPortRangeMax) // reset
			port, _ = pool.allocate()
		}
		pool.release(port)
	}
}

func TestNAT_UnsupportedProto(t *testing.T) {
	nat, _ := testNAT(t, nil)

	// ICMP packet.
	pkt := make([]byte, 28)
	pkt[0] = 0x45
	binary.BigEndian.PutUint16(pkt[2:4], 28)
	pkt[9] = 1 // ICMP
	copy(pkt[12:16], net.IPv4(10, 0, 0, 1).To4())
	copy(pkt[16:20], net.IPv4(8, 8, 8, 8).To4())

	_, err := nat.Translate(testSession, pkt)
	if err != ErrUnsupportedProto {
		t.Errorf("got %v, want ErrUnsupportedProto", err)
	}
}

// --- DNS interception integration tests (story 3.5, review H2) ---

func TestNAT_DNSIntercept_UDP(t *testing.T) {
	// Build a mock DNS resolver that returns a fixed response.
	mockResolver := newTestDNSResolver(t)

	nat := NewNAT(
		net.IPv4(1, 2, 3, 4),
		WithContext(context.Background()),
		WithDNSResolver(mockResolver.DNSResolver),
		// Mock dialers to avoid real network calls.
		WithDialTCP(func(_, _ *net.TCPAddr) (net.Conn, error) {
			t.Fatal("TCP dialer should not be called for DNS intercept")
			return nil, nil
		}),
		WithDialUDP(func(_, _ *net.UDPAddr) (net.Conn, error) {
			t.Fatal("UDP dialer should not be called for DNS intercept")
			return nil, nil
		}),
	)
	defer nat.Shutdown(context.Background())

	session := TunnelSession{ClientIPHash: "abc123", OpenedAt: time.Now()}
	outCh, cleanup := nat.OpenSession(context.Background(), session)
	defer cleanup()

	// Build a UDP packet to port 53 with a DNS query payload.
	dnsQuery := mockResolver.queryWire
	pkt := buildUDPPacket(
		net.IPv4(10, 0, 0, 1),  // client src
		net.IPv4(1, 1, 1, 1),   // dst = DNS server
		12345, 53,               // src port, dst port = DNS
		dnsQuery,
	)

	err := nat.Forward(context.Background(), session, pkt)
	if err != nil {
		t.Fatalf("Forward() error: %v", err)
	}

	// Should receive response on session channel.
	select {
	case respPkt := <-outCh:
		// Verify it's a UDP response packet with swapped IPs.
		p, err := parseIPv4(respPkt)
		if err != nil {
			t.Fatalf("parse response packet: %v", err)
		}
		if p.proto != ipv4ProtoUDP {
			t.Errorf("response proto = %d, want UDP (%d)", p.proto, ipv4ProtoUDP)
		}
		// Source should be the original DNS server (1.1.1.1).
		if !p.srcIP.Equal(net.IPv4(1, 1, 1, 1)) {
			t.Errorf("response srcIP = %s, want 1.1.1.1", p.srcIP)
		}
		// Dest should be the original client (10.0.0.1).
		if !p.dstIP.Equal(net.IPv4(10, 0, 0, 1)) {
			t.Errorf("response dstIP = %s, want 10.0.0.1", p.dstIP)
		}
		if p.srcPort != 53 {
			t.Errorf("response srcPort = %d, want 53", p.srcPort)
		}
		if p.dstPort != 12345 {
			t.Errorf("response dstPort = %d, want 12345", p.dstPort)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timeout waiting for DNS response on session channel")
	}

	// NAT table should NOT have any entries (DNS was intercepted, not NAT'd).
	if nat.Stats().Entries != 0 {
		t.Errorf("NAT entries = %d, want 0 (DNS should be intercepted, not NAT'd)", nat.Stats().Entries)
	}
}

func TestNAT_DNSIntercept_NonDNS_NotIntercepted(t *testing.T) {
	mockResolver := newTestDNSResolver(t)

	mc := newMockConn()
	mc.readBuf <- []byte{} // immediate EOF for reverse loop

	nat := NewNAT(
		net.IPv4(1, 2, 3, 4),
		WithContext(context.Background()),
		WithDNSResolver(mockResolver.DNSResolver),
		WithDialUDP(func(_, _ *net.UDPAddr) (net.Conn, error) {
			return mc, nil
		}),
	)
	defer nat.Shutdown(context.Background())

	session := TunnelSession{ClientIPHash: "def456", OpenedAt: time.Now()}
	_, cleanup := nat.OpenSession(context.Background(), session)
	defer cleanup()

	// Build a UDP packet to port 443 (NOT DNS).
	pkt := buildUDPPacket(
		net.IPv4(10, 0, 0, 1),
		net.IPv4(8, 8, 8, 8),
		12345, 443,
		[]byte("not dns"),
	)

	err := nat.Forward(context.Background(), session, pkt)
	if err != nil {
		t.Fatalf("Forward() error: %v", err)
	}

	// Should have created a NAT entry (not intercepted).
	if nat.Stats().Entries != 1 {
		t.Errorf("NAT entries = %d, want 1 (non-DNS should be NAT'd)", nat.Stats().Entries)
	}
}

// newTestDNSResolver creates a DNSResolver with a local httptest.Server
// that always returns a valid DNS response. Also stores a pre-built query.
func newTestDNSResolver(t *testing.T) *testDNSResolverHelper {
	t.Helper()
	h := &testDNSResolverHelper{}

	// Pre-build a DNS query wire format.
	// We use a minimal hand-built DNS query for "test.example.com" type A.
	// QID=0x1234, QR=0, QDCOUNT=1, name=test.example.com, type=A, class=IN.
	h.queryWire = []byte{
		0x12, 0x34, // QID
		0x01, 0x00, // flags: RD=1
		0x00, 0x01, // QDCOUNT=1
		0x00, 0x00, 0x00, 0x00, 0x00, 0x00, // ANCOUNT, NSCOUNT, ARCOUNT
		0x04, 't', 'e', 's', 't',
		0x07, 'e', 'x', 'a', 'm', 'p', 'l', 'e',
		0x03, 'c', 'o', 'm',
		0x00,       // root label
		0x00, 0x01, // QTYPE=A
		0x00, 0x01, // QCLASS=IN
	}

	// Build a response: same QID, QR=1, RCODE=0, one A record.
	h.respWire = []byte{
		0x12, 0x34, // QID
		0x81, 0x80, // flags: QR=1, RD=1, RA=1
		0x00, 0x01, // QDCOUNT=1
		0x00, 0x01, // ANCOUNT=1
		0x00, 0x00, 0x00, 0x00, // NSCOUNT, ARCOUNT
		// Question section (same as query).
		0x04, 't', 'e', 's', 't',
		0x07, 'e', 'x', 'a', 'm', 'p', 'l', 'e',
		0x03, 'c', 'o', 'm',
		0x00,
		0x00, 0x01, 0x00, 0x01,
		// Answer: compressed name ptr, type A, class IN, TTL 300, rdlen 4, IP 93.184.216.34.
		0xc0, 0x0c, // name pointer to offset 12
		0x00, 0x01, // TYPE=A
		0x00, 0x01, // CLASS=IN
		0x00, 0x00, 0x01, 0x2c, // TTL=300
		0x00, 0x04, // RDLENGTH=4
		93, 184, 216, 34, // RDATA
	}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/dns-message")
		w.Write(h.respWire)
	}))
	t.Cleanup(srv.Close)

	h.DNSResolver = NewDNSResolver([]string{srv.URL}, nil, srv.Client())
	return h
}

type testDNSResolverHelper struct {
	*DNSResolver
	queryWire []byte
	respWire  []byte
}
