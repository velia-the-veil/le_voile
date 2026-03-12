package stun

import "fmt"

// IsSTUN reports whether packet is a valid STUN message by checking:
//   - minimum length (20 bytes)
//   - first 2 bits of byte 0 are 0b00 (distinguishes STUN from RTP/RTCP)
//   - magic cookie at bytes 4-7 equals 0x2112A442
func IsSTUN(packet []byte) bool {
	if len(packet) < HeaderSize {
		return false
	}
	// RFC 5764: first 2 bits must be 0b00 for STUN.
	if packet[0]&0xC0 != 0 {
		return false
	}
	cookie := byteOrder.Uint32(packet[4:8])
	return cookie == MagicCookie
}

// IsBindingRequest reports whether packet is a STUN Binding Request
// (message type 0x0001) at bytes 0-1.
func IsBindingRequest(packet []byte) bool {
	if len(packet) < 2 {
		return false
	}
	msgType := byteOrder.Uint16(packet[0:2])
	return msgType == TypeBindingRequest
}

// IsTURN reports whether packet is a TURN message (Allocate, CreatePermission,
// ChannelBind, Send Indication, or Data Indication). TURN messages use the
// STUN framing but are NOT intercepted — they pass through transparently.
// The packet must also pass basic STUN validation (length, first 2 bits, magic cookie).
func IsTURN(packet []byte) bool {
	if !IsSTUN(packet) {
		return false
	}
	msgType := byteOrder.Uint16(packet[0:2])
	switch msgType {
	case TypeAllocateRequest, TypeCreatePermission, TypeChannelBind,
		TypeSendIndication, TypeDataIndication:
		return true
	}
	return false
}

// ParseHeader extracts the 20-byte STUN header from packet.
// Returns an error if the packet is too short, has invalid first-two-bits,
// or contains a wrong magic cookie.
func ParseHeader(packet []byte) (*Header, error) {
	if len(packet) < HeaderSize {
		return nil, fmt.Errorf("stun: parse header: packet too short (%d bytes, need %d)", len(packet), HeaderSize)
	}
	// RFC 5764: first 2 bits must be 0b00.
	if packet[0]&0xC0 != 0 {
		return nil, fmt.Errorf("stun: parse header: first two bits are not 0b00 (byte 0 = 0x%02X)", packet[0])
	}
	cookie := byteOrder.Uint32(packet[4:8])
	if cookie != MagicCookie {
		return nil, fmt.Errorf("stun: parse header: invalid magic cookie 0x%08X", cookie)
	}

	h := &Header{
		Type:        byteOrder.Uint16(packet[0:2]),
		Length:      byteOrder.Uint16(packet[2:4]),
		MagicCookie: cookie,
	}
	copy(h.TransactionID[:], packet[8:20])
	return h, nil
}
