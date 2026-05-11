package relay

import (
	"encoding/binary"
	"net"
)

// IPv4 protocol numbers and header constants.
const (
	ipv4Version      = 4
	ipv4MinHeaderLen = 20
	ipv4ProtoTCP     = 6
	ipv4ProtoUDP     = 17
)

// TCP constants.
const (
	tcpMinHeaderLen = 20
	tcpFlagFIN      = 0x01
	tcpFlagSYN      = 0x02
	tcpFlagRST      = 0x04
	tcpFlagPSH      = 0x08
	tcpFlagACK      = 0x10
)

const udpHeaderLen = 8

// parsedPacket holds decoded fields of an IPv4+TCP/UDP packet.
type parsedPacket struct {
	headerLen int
	totalLen  int
	proto     uint8
	srcIP     net.IP
	dstIP     net.IP

	srcPort uint16
	dstPort uint16

	// TCP-specific
	tcpSeq     uint32
	tcpAck     uint32
	tcpFlags   uint8
	tcpWindow  uint16
	tcpDataOff int

	l4Offset      int
	payloadOffset int
}

// payload returns the L4 payload slice within pkt.
func (p *parsedPacket) payload(pkt []byte) []byte {
	if p.payloadOffset >= p.totalLen {
		return nil
	}
	return pkt[p.payloadOffset:p.totalLen]
}

// parseIPv4 decodes an IPv4 packet with TCP or UDP L4 header.
func parseIPv4(pkt []byte) (*parsedPacket, error) {
	if len(pkt) < ipv4MinHeaderLen {
		return nil, ErrInvalidPacket
	}
	version := pkt[0] >> 4
	if version == 6 {
		return nil, ErrUnsupportedProto
	}
	if version != ipv4Version {
		return nil, ErrInvalidPacket
	}
	ihl := int(pkt[0]&0x0f) * 4
	if ihl < ipv4MinHeaderLen || ihl > len(pkt) {
		return nil, ErrInvalidPacket
	}
	totalLen := int(binary.BigEndian.Uint16(pkt[2:4]))
	if totalLen < ihl || totalLen > len(pkt) {
		return nil, ErrInvalidPacket
	}
	p := &parsedPacket{
		headerLen: ihl,
		totalLen:  totalLen,
		proto:     pkt[9],
		srcIP:     net.IP(append(net.IP(nil), pkt[12:16]...)).To4(),
		dstIP:     net.IP(append(net.IP(nil), pkt[16:20]...)).To4(),
		l4Offset:  ihl,
	}

	switch p.proto {
	case ipv4ProtoTCP:
		if totalLen < ihl+tcpMinHeaderLen {
			return nil, ErrInvalidPacket
		}
		tcp := pkt[ihl:]
		p.srcPort = binary.BigEndian.Uint16(tcp[0:2])
		p.dstPort = binary.BigEndian.Uint16(tcp[2:4])
		p.tcpSeq = binary.BigEndian.Uint32(tcp[4:8])
		p.tcpAck = binary.BigEndian.Uint32(tcp[8:12])
		p.tcpDataOff = int(tcp[12]>>4) * 4
		if p.tcpDataOff < tcpMinHeaderLen || ihl+p.tcpDataOff > totalLen {
			return nil, ErrInvalidPacket
		}
		p.tcpFlags = tcp[13]
		p.tcpWindow = binary.BigEndian.Uint16(tcp[14:16])
		p.payloadOffset = ihl + p.tcpDataOff

	case ipv4ProtoUDP:
		if totalLen < ihl+udpHeaderLen {
			return nil, ErrInvalidPacket
		}
		udp := pkt[ihl:]
		p.srcPort = binary.BigEndian.Uint16(udp[0:2])
		p.dstPort = binary.BigEndian.Uint16(udp[2:4])
		p.payloadOffset = ihl + udpHeaderLen

	default:
		return nil, ErrUnsupportedProto
	}
	return p, nil
}

// rewriteSource rewrites source IP and port, then recalculates checksums.
func rewriteSource(pkt []byte, p *parsedPacket, newIP net.IP, newPort uint16) {
	copy(pkt[12:16], newIP.To4())
	binary.BigEndian.PutUint16(pkt[p.l4Offset:p.l4Offset+2], newPort)
	recalcIPv4Checksum(pkt[:p.headerLen])
	recalcL4Checksum(pkt, p)
}

// rewriteDest rewrites destination IP and port, then recalculates checksums.
func rewriteDest(pkt []byte, p *parsedPacket, newIP net.IP, newPort uint16) {
	copy(pkt[16:20], newIP.To4())
	binary.BigEndian.PutUint16(pkt[p.l4Offset+2:p.l4Offset+4], newPort)
	recalcIPv4Checksum(pkt[:p.headerLen])
	recalcL4Checksum(pkt, p)
}

func recalcL4Checksum(pkt []byte, p *parsedPacket) {
	switch p.proto {
	case ipv4ProtoTCP:
		recalcTCPChecksum(pkt, p)
	case ipv4ProtoUDP:
		recalcUDPChecksum(pkt, p)
	}
}

func recalcIPv4Checksum(hdr []byte) {
	hdr[10] = 0
	hdr[11] = 0
	binary.BigEndian.PutUint16(hdr[10:12], onesComplement(checksumRaw(hdr)))
}

func recalcTCPChecksum(pkt []byte, p *parsedPacket) {
	tcpLen := p.totalLen - p.headerLen
	tcp := pkt[p.l4Offset : p.l4Offset+tcpLen]
	tcp[16] = 0
	tcp[17] = 0
	sum := pseudoHeaderSum(pkt[12:16], pkt[16:20], ipv4ProtoTCP, tcpLen)
	sum += checksumRaw(tcp)
	binary.BigEndian.PutUint16(tcp[16:18], onesComplement(sum))
}

func recalcUDPChecksum(pkt []byte, p *parsedPacket) {
	udpLen := p.totalLen - p.headerLen
	udp := pkt[p.l4Offset : p.l4Offset+udpLen]
	udp[6] = 0
	udp[7] = 0
	sum := pseudoHeaderSum(pkt[12:16], pkt[16:20], ipv4ProtoUDP, udpLen)
	sum += checksumRaw(udp)
	result := onesComplement(sum)
	if result == 0 {
		result = 0xffff
	}
	binary.BigEndian.PutUint16(udp[6:8], result)
}

func pseudoHeaderSum(srcIP, dstIP []byte, proto uint8, l4Len int) uint32 {
	var s uint32
	s += uint32(srcIP[0])<<8 | uint32(srcIP[1])
	s += uint32(srcIP[2])<<8 | uint32(srcIP[3])
	s += uint32(dstIP[0])<<8 | uint32(dstIP[1])
	s += uint32(dstIP[2])<<8 | uint32(dstIP[3])
	s += uint32(proto)
	// #nosec G115 -- l4Len est la longueur d'un payload L4 (TCP/UDP) borné
	// par MTU IPv4 (≤ 65535 octets), donc << 2^32. Conversion sûre.
	s += uint32(l4Len)
	return s
}

func checksumRaw(data []byte) uint32 {
	var sum uint32
	for i := 0; i+1 < len(data); i += 2 {
		sum += uint32(data[i])<<8 | uint32(data[i+1])
	}
	if len(data)%2 == 1 {
		sum += uint32(data[len(data)-1]) << 8
	}
	return sum
}

func onesComplement(sum uint32) uint16 {
	for sum > 0xffff {
		sum = (sum >> 16) + (sum & 0xffff)
	}
	return ^uint16(sum)
}

// buildTCPPacket constructs a raw IPv4+TCP packet.
func buildTCPPacket(srcIP, dstIP net.IP, srcPort, dstPort uint16, payload []byte, flags uint8, seq, ack uint32, window uint16) []byte {
	ipHL := 20
	tcpHL := 20
	totalLen := ipHL + tcpHL + len(payload)
	pkt := make([]byte, totalLen)

	// IPv4 header.
	pkt[0] = 0x45
	binary.BigEndian.PutUint16(pkt[2:4], uint16(totalLen))
	pkt[8] = 64 // TTL
	pkt[9] = ipv4ProtoTCP
	copy(pkt[12:16], srcIP.To4())
	copy(pkt[16:20], dstIP.To4())

	// TCP header.
	tcp := pkt[ipHL:]
	binary.BigEndian.PutUint16(tcp[0:2], srcPort)
	binary.BigEndian.PutUint16(tcp[2:4], dstPort)
	binary.BigEndian.PutUint32(tcp[4:8], seq)
	binary.BigEndian.PutUint32(tcp[8:12], ack)
	tcp[12] = byte(tcpHL/4) << 4
	tcp[13] = flags
	binary.BigEndian.PutUint16(tcp[14:16], window)
	if len(payload) > 0 {
		copy(tcp[tcpHL:], payload)
	}

	recalcIPv4Checksum(pkt[:ipHL])
	p := &parsedPacket{headerLen: ipHL, totalLen: totalLen, proto: ipv4ProtoTCP, l4Offset: ipHL}
	recalcTCPChecksum(pkt, p)
	return pkt
}

// buildUDPPacket constructs a raw IPv4+UDP packet.
func buildUDPPacket(srcIP, dstIP net.IP, srcPort, dstPort uint16, payload []byte) []byte {
	ipHL := 20
	udpLen := udpHeaderLen + len(payload)
	totalLen := ipHL + udpLen
	pkt := make([]byte, totalLen)

	pkt[0] = 0x45
	// #nosec G115 -- IPv4 Total Length field est uint16 par RFC 791 (max
	// 65535). totalLen est ipHL(20) + udpLen, et udpLen est borné par MTU.
	binary.BigEndian.PutUint16(pkt[2:4], uint16(totalLen))
	pkt[8] = 64
	pkt[9] = ipv4ProtoUDP
	copy(pkt[12:16], srcIP.To4())
	copy(pkt[16:20], dstIP.To4())

	udp := pkt[ipHL:]
	binary.BigEndian.PutUint16(udp[0:2], srcPort)
	binary.BigEndian.PutUint16(udp[2:4], dstPort)
	// #nosec G115 -- UDP Length field est uint16 par RFC 768 (max 65535).
	// udpLen = udpHeaderLen(8) + len(payload), borné par MTU IPv4.
	binary.BigEndian.PutUint16(udp[4:6], uint16(udpLen))
	if len(payload) > 0 {
		copy(udp[udpHeaderLen:], payload)
	}

	recalcIPv4Checksum(pkt[:ipHL])
	p := &parsedPacket{headerLen: ipHL, totalLen: totalLen, proto: ipv4ProtoUDP, l4Offset: ipHL}
	recalcUDPChecksum(pkt, p)
	return pkt
}
