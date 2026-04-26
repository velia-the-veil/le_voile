package relay

import (
	"encoding/binary"
	"net"
	"testing"
)

func TestParseIPv4_UDP(t *testing.T) {
	// Construct a minimal UDP packet: srcIP=10.0.0.1:1234, dstIP=8.8.8.8:53, payload="hello"
	pkt := buildUDPPacket(net.IPv4(10, 0, 0, 1), net.IPv4(8, 8, 8, 8), 1234, 53, []byte("hello"))

	p, err := parseIPv4(pkt)
	if err != nil {
		t.Fatalf("parseIPv4: %v", err)
	}
	if p.proto != ipv4ProtoUDP {
		t.Fatalf("proto=%d, want UDP(%d)", p.proto, ipv4ProtoUDP)
	}
	if !p.srcIP.Equal(net.IPv4(10, 0, 0, 1)) {
		t.Fatalf("srcIP=%v", p.srcIP)
	}
	if !p.dstIP.Equal(net.IPv4(8, 8, 8, 8)) {
		t.Fatalf("dstIP=%v", p.dstIP)
	}
	if p.srcPort != 1234 || p.dstPort != 53 {
		t.Fatalf("ports=%d:%d", p.srcPort, p.dstPort)
	}
	payload := p.payload(pkt)
	if string(payload) != "hello" {
		t.Fatalf("payload=%q", payload)
	}
}

func TestParseIPv4_TCP(t *testing.T) {
	pkt := buildTCPPacket(net.IPv4(192, 168, 1, 1), net.IPv4(93, 184, 216, 34),
		4567, 80, []byte("GET"), tcpFlagPSH|tcpFlagACK, 1000, 2000, 65535)

	p, err := parseIPv4(pkt)
	if err != nil {
		t.Fatalf("parseIPv4: %v", err)
	}
	if p.proto != ipv4ProtoTCP {
		t.Fatalf("proto=%d", p.proto)
	}
	if p.srcPort != 4567 || p.dstPort != 80 {
		t.Fatalf("ports=%d:%d", p.srcPort, p.dstPort)
	}
	if p.tcpSeq != 1000 || p.tcpAck != 2000 {
		t.Fatalf("seq=%d ack=%d", p.tcpSeq, p.tcpAck)
	}
	if p.tcpFlags != tcpFlagPSH|tcpFlagACK {
		t.Fatalf("flags=%02x", p.tcpFlags)
	}
	if string(p.payload(pkt)) != "GET" {
		t.Fatalf("payload=%q", p.payload(pkt))
	}
}

func TestParseIPv4_IPv6Rejected(t *testing.T) {
	pkt := make([]byte, 40)
	pkt[0] = 0x60 // IPv6
	_, err := parseIPv4(pkt)
	if err != ErrUnsupportedProto {
		t.Fatalf("expected ErrUnsupportedProto, got %v", err)
	}
}

func TestParseIPv4_TooShort(t *testing.T) {
	_, err := parseIPv4([]byte{0x45, 0x00})
	if err != ErrInvalidPacket {
		t.Fatalf("expected ErrInvalidPacket, got %v", err)
	}
}

func TestParseIPv4_ICMPRejected(t *testing.T) {
	pkt := make([]byte, 28)
	pkt[0] = 0x45
	binary.BigEndian.PutUint16(pkt[2:4], 28)
	pkt[9] = 1 // ICMP
	_, err := parseIPv4(pkt)
	if err != ErrUnsupportedProto {
		t.Fatalf("expected ErrUnsupportedProto, got %v", err)
	}
}

func TestRewriteSource_UDP(t *testing.T) {
	pkt := buildUDPPacket(net.IPv4(10, 0, 0, 1), net.IPv4(8, 8, 8, 8), 1234, 53, []byte("test"))
	p, _ := parseIPv4(pkt)

	newIP := net.IPv4(1, 2, 3, 4)
	rewriteSource(pkt, p, newIP, 15000)

	p2, err := parseIPv4(pkt)
	if err != nil {
		t.Fatalf("parse after rewrite: %v", err)
	}
	if !p2.srcIP.Equal(net.IPv4(1, 2, 3, 4)) {
		t.Fatalf("srcIP=%v", p2.srcIP)
	}
	if p2.srcPort != 15000 {
		t.Fatalf("srcPort=%d", p2.srcPort)
	}
	if !p2.dstIP.Equal(net.IPv4(8, 8, 8, 8)) || p2.dstPort != 53 {
		t.Fatalf("dst changed unexpectedly")
	}
	// Verify checksum is valid by re-checking.
	verifyIPv4Checksum(t, pkt)
	verifyUDPChecksum(t, pkt, p2)
}

func TestRewriteDest_TCP(t *testing.T) {
	pkt := buildTCPPacket(net.IPv4(1, 1, 1, 1), net.IPv4(2, 2, 2, 2),
		1000, 2000, []byte("data"), tcpFlagACK, 100, 200, 65535)
	p, _ := parseIPv4(pkt)

	rewriteDest(pkt, p, net.IPv4(10, 0, 0, 5), 3000)

	p2, _ := parseIPv4(pkt)
	if !p2.dstIP.Equal(net.IPv4(10, 0, 0, 5)) || p2.dstPort != 3000 {
		t.Fatalf("dst=%v:%d", p2.dstIP, p2.dstPort)
	}
	verifyIPv4Checksum(t, pkt)
	verifyTCPChecksum(t, pkt, p2)
}

func TestBuildUDPPacket_Checksums(t *testing.T) {
	pkt := buildUDPPacket(net.IPv4(192, 168, 0, 1), net.IPv4(1, 1, 1, 1), 5000, 53, []byte{0xAA, 0xBB})
	p, err := parseIPv4(pkt)
	if err != nil {
		t.Fatal(err)
	}
	verifyIPv4Checksum(t, pkt)
	verifyUDPChecksum(t, pkt, p)
}

func TestBuildTCPPacket_Checksums(t *testing.T) {
	pkt := buildTCPPacket(net.IPv4(10, 0, 0, 1), net.IPv4(93, 184, 216, 34),
		12345, 443, []byte("TLS data"), tcpFlagPSH|tcpFlagACK, 5000, 6000, 32768)
	p, err := parseIPv4(pkt)
	if err != nil {
		t.Fatal(err)
	}
	verifyIPv4Checksum(t, pkt)
	verifyTCPChecksum(t, pkt, p)
}

// Checksum verification helpers.

func verifyIPv4Checksum(t *testing.T, pkt []byte) {
	t.Helper()
	ihl := int(pkt[0]&0x0f) * 4
	hdr := make([]byte, ihl)
	copy(hdr, pkt[:ihl])
	// Checksum over header with existing checksum should be 0.
	sum := checksumRaw(hdr)
	for sum > 0xffff {
		sum = (sum >> 16) + (sum & 0xffff)
	}
	if uint16(sum) != 0xffff {
		t.Errorf("IPv4 checksum invalid: sum=%04x", sum)
	}
}

func verifyTCPChecksum(t *testing.T, pkt []byte, p *parsedPacket) {
	t.Helper()
	tcpLen := p.totalLen - p.headerLen
	tcp := make([]byte, tcpLen)
	copy(tcp, pkt[p.l4Offset:p.l4Offset+tcpLen])
	sum := pseudoHeaderSum(pkt[12:16], pkt[16:20], ipv4ProtoTCP, tcpLen)
	sum += checksumRaw(tcp)
	for sum > 0xffff {
		sum = (sum >> 16) + (sum & 0xffff)
	}
	if uint16(sum) != 0xffff {
		t.Errorf("TCP checksum invalid: sum=%04x", sum)
	}
}

func verifyUDPChecksum(t *testing.T, pkt []byte, p *parsedPacket) {
	t.Helper()
	udpLen := p.totalLen - p.headerLen
	udp := make([]byte, udpLen)
	copy(udp, pkt[p.l4Offset:p.l4Offset+udpLen])
	sum := pseudoHeaderSum(pkt[12:16], pkt[16:20], ipv4ProtoUDP, udpLen)
	sum += checksumRaw(udp)
	for sum > 0xffff {
		sum = (sum >> 16) + (sum & 0xffff)
	}
	if uint16(sum) != 0xffff {
		t.Errorf("UDP checksum invalid: sum=%04x", sum)
	}
}

func FuzzParseIPv4(f *testing.F) {
	// Seed with valid packets.
	f.Add(buildUDPPacket(net.IPv4(10, 0, 0, 1), net.IPv4(8, 8, 8, 8), 1234, 53, []byte("dns")))
	f.Add(buildTCPPacket(net.IPv4(10, 0, 0, 1), net.IPv4(93, 184, 216, 34), 4567, 80, nil, tcpFlagSYN, 0, 0, 65535))
	f.Fuzz(func(t *testing.T, data []byte) {
		// Just ensure no panic.
		parseIPv4(data)
	})
}
