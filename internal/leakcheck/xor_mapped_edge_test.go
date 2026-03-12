package leakcheck

import (
	"encoding/binary"
	"net"
	"testing"
)

// buildSTUNResponseIPv6 creates a STUN Binding Response with an XOR-MAPPED-ADDRESS
// attribute encoding the given IPv6 address.
func buildSTUNResponseIPv6(ip net.IP, transactionID [12]byte) []byte {
	ip16 := ip.To16()
	if ip16 == nil {
		panic("invalid IPv6 address")
	}

	// XOR-MAPPED-ADDRESS attribute value (20 bytes for IPv6):
	// byte 0: reserved (0x00)
	// byte 1: family (0x02 = IPv6)
	// bytes 2-3: X-Port (port XOR'd with magic cookie MSB 16 bits)
	// bytes 4-19: X-Address (IP XOR'd with magic cookie + transaction ID)
	attrValue := make([]byte, 20)
	attrValue[0] = 0x00
	attrValue[1] = familyIPv6

	// X-Port: 12345 XOR 0x2112
	binary.BigEndian.PutUint16(attrValue[2:4], 12345^0x2112)

	// X-Address: IP XOR (magic cookie bytes + transaction ID)
	xorKey := make([]byte, 16)
	binary.BigEndian.PutUint32(xorKey[0:4], stunMagicCookie)
	copy(xorKey[4:16], transactionID[:])
	for i := 0; i < 16; i++ {
		attrValue[4+i] = ip16[i] ^ xorKey[i]
	}

	// Full attribute: type (2) + length (2) + value (20) = 24 bytes
	attr := make([]byte, 4+len(attrValue))
	binary.BigEndian.PutUint16(attr[0:2], attrTypeXORMappedAddress)
	binary.BigEndian.PutUint16(attr[2:4], uint16(len(attrValue)))
	copy(attr[4:], attrValue)

	// STUN header (20 bytes) + attributes
	pkt := make([]byte, stunHeaderSize+len(attr))
	// Type: Binding Success Response (0x0101)
	binary.BigEndian.PutUint16(pkt[0:2], 0x0101)
	// Length: attribute bytes
	binary.BigEndian.PutUint16(pkt[2:4], uint16(len(attr)))
	// Magic Cookie
	binary.BigEndian.PutUint32(pkt[4:8], stunMagicCookie)
	// Transaction ID
	copy(pkt[8:20], transactionID[:])
	// Attributes
	copy(pkt[20:], attr)

	return pkt
}

// TestParseXORMappedAddress_IPv6 verifies that IPv6 addresses are correctly
// decoded from XOR-MAPPED-ADDRESS using the magic cookie + transaction ID.
func TestParseXORMappedAddress_IPv6(t *testing.T) {
	t.Parallel()

	txID := [12]byte{0x01, 0x02, 0x03, 0x04, 0x05, 0x06, 0x07, 0x08, 0x09, 0x0A, 0x0B, 0x0C}

	tests := []struct {
		name   string
		ip     net.IP
	}{
		{
			name: "[P0] IPv6 global unicast — 2001:db8::1",
			ip:   net.ParseIP("2001:db8::1"),
		},
		{
			name: "[P0] IPv6 loopback — ::1",
			ip:   net.ParseIP("::1"),
		},
		{
			name: "[P0] IPv6 full — 2001:db8:85a3::8a2e:370:7334",
			ip:   net.ParseIP("2001:db8:85a3::8a2e:370:7334"),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			resp := buildSTUNResponseIPv6(tt.ip, txID)
			got, err := ParseXORMappedAddress(resp)
			if err != nil {
				t.Fatalf("ParseXORMappedAddress() error = %v", err)
			}
			if !got.Equal(tt.ip) {
				t.Errorf("ParseXORMappedAddress() = %v, want %v", got, tt.ip)
			}
		})
	}
}

// TestParseXORMappedAddress_UnknownFamily verifies that an unknown address
// family (not 0x01 or 0x02) produces an error.
func TestParseXORMappedAddress_UnknownFamily(t *testing.T) {
	t.Parallel()

	txID := [12]byte{0x01, 0x02, 0x03, 0x04, 0x05, 0x06, 0x07, 0x08, 0x09, 0x0A, 0x0B, 0x0C}

	// Build a valid STUN response but with family 0x03 (invalid).
	attrValue := make([]byte, 8)
	attrValue[0] = 0x00
	attrValue[1] = 0x03 // Unknown family
	binary.BigEndian.PutUint16(attrValue[2:4], 12345^0x2112)

	attr := make([]byte, 4+len(attrValue))
	binary.BigEndian.PutUint16(attr[0:2], attrTypeXORMappedAddress)
	binary.BigEndian.PutUint16(attr[2:4], uint16(len(attrValue)))
	copy(attr[4:], attrValue)

	pkt := make([]byte, stunHeaderSize+len(attr))
	binary.BigEndian.PutUint16(pkt[0:2], 0x0101)
	binary.BigEndian.PutUint16(pkt[2:4], uint16(len(attr)))
	binary.BigEndian.PutUint32(pkt[4:8], stunMagicCookie)
	copy(pkt[8:20], txID[:])
	copy(pkt[20:], attr)

	_, err := ParseXORMappedAddress(pkt)
	if err == nil {
		t.Fatal("[P1] ParseXORMappedAddress() should return error for unknown family 0x03")
	}
}

// TestParseXORMappedAddress_TruncatedIPv4 verifies that a too-short IPv4
// XOR-MAPPED-ADDRESS value produces an error.
func TestParseXORMappedAddress_TruncatedIPv4(t *testing.T) {
	t.Parallel()

	txID := [12]byte{0x01, 0x02, 0x03, 0x04, 0x05, 0x06, 0x07, 0x08, 0x09, 0x0A, 0x0B, 0x0C}

	// IPv4 needs 8 bytes but we only provide 6 (family + port + 2 bytes of IP).
	attrValue := make([]byte, 6)
	attrValue[0] = 0x00
	attrValue[1] = familyIPv4

	attr := make([]byte, 4+len(attrValue))
	binary.BigEndian.PutUint16(attr[0:2], attrTypeXORMappedAddress)
	binary.BigEndian.PutUint16(attr[2:4], uint16(len(attrValue)))
	copy(attr[4:], attrValue)

	pkt := make([]byte, stunHeaderSize+len(attr))
	binary.BigEndian.PutUint16(pkt[0:2], 0x0101)
	binary.BigEndian.PutUint16(pkt[2:4], uint16(len(attr)))
	binary.BigEndian.PutUint32(pkt[4:8], stunMagicCookie)
	copy(pkt[8:20], txID[:])
	copy(pkt[20:], attr)

	_, err := ParseXORMappedAddress(pkt)
	if err == nil {
		t.Fatal("[P1] ParseXORMappedAddress() should return error for truncated IPv4")
	}
}

// TestParseXORMappedAddress_TruncatedIPv6 verifies that a too-short IPv6
// XOR-MAPPED-ADDRESS value produces an error.
func TestParseXORMappedAddress_TruncatedIPv6(t *testing.T) {
	t.Parallel()

	txID := [12]byte{0x01, 0x02, 0x03, 0x04, 0x05, 0x06, 0x07, 0x08, 0x09, 0x0A, 0x0B, 0x0C}

	// IPv6 needs 20 bytes but we only provide 12.
	attrValue := make([]byte, 12)
	attrValue[0] = 0x00
	attrValue[1] = familyIPv6

	attr := make([]byte, 4+len(attrValue))
	binary.BigEndian.PutUint16(attr[0:2], attrTypeXORMappedAddress)
	binary.BigEndian.PutUint16(attr[2:4], uint16(len(attrValue)))
	copy(attr[4:], attrValue)

	pkt := make([]byte, stunHeaderSize+len(attr))
	binary.BigEndian.PutUint16(pkt[0:2], 0x0101)
	binary.BigEndian.PutUint16(pkt[2:4], uint16(len(attr)))
	binary.BigEndian.PutUint32(pkt[4:8], stunMagicCookie)
	copy(pkt[8:20], txID[:])
	copy(pkt[20:], attr)

	_, err := ParseXORMappedAddress(pkt)
	if err == nil {
		t.Fatal("[P1] ParseXORMappedAddress() should return error for truncated IPv6")
	}
}

// TestParseXORMappedAddress_MultipleAttributes verifies that XOR-MAPPED-ADDRESS
// is found even when preceded by other attributes (attribute walking + padding).
func TestParseXORMappedAddress_MultipleAttributes(t *testing.T) {
	t.Parallel()

	txID := [12]byte{0x01, 0x02, 0x03, 0x04, 0x05, 0x06, 0x07, 0x08, 0x09, 0x0A, 0x0B, 0x0C}
	expectedIP := net.IPv4(203, 0, 113, 42).To4()

	// Build an XOR-MAPPED-ADDRESS attribute.
	xorAttrValue := make([]byte, 8)
	xorAttrValue[0] = 0x00
	xorAttrValue[1] = familyIPv4
	binary.BigEndian.PutUint16(xorAttrValue[2:4], 12345^0x2112)
	cookieBytes := make([]byte, 4)
	binary.BigEndian.PutUint32(cookieBytes, stunMagicCookie)
	for i := 0; i < 4; i++ {
		xorAttrValue[4+i] = expectedIP[i] ^ cookieBytes[i]
	}

	// Preceding attribute: SOFTWARE (0x8022), 5 bytes value → padded to 8.
	softwareValue := []byte("test!")             // 5 bytes
	softwareAttr := make([]byte, 4+len(softwareValue)+3) // +3 for padding to 8
	binary.BigEndian.PutUint16(softwareAttr[0:2], 0x8022) // SOFTWARE
	binary.BigEndian.PutUint16(softwareAttr[2:4], uint16(len(softwareValue)))
	copy(softwareAttr[4:], softwareValue)
	// Padding bytes are zero (already zero from make).

	// XOR-MAPPED-ADDRESS attribute
	xorAttr := make([]byte, 4+len(xorAttrValue))
	binary.BigEndian.PutUint16(xorAttr[0:2], attrTypeXORMappedAddress)
	binary.BigEndian.PutUint16(xorAttr[2:4], uint16(len(xorAttrValue)))
	copy(xorAttr[4:], xorAttrValue)

	// Combine attributes
	attrs := append(softwareAttr, xorAttr...)

	// STUN header + all attributes
	pkt := make([]byte, stunHeaderSize+len(attrs))
	binary.BigEndian.PutUint16(pkt[0:2], 0x0101) // Binding Success Response
	binary.BigEndian.PutUint16(pkt[2:4], uint16(len(attrs)))
	binary.BigEndian.PutUint32(pkt[4:8], stunMagicCookie)
	copy(pkt[8:20], txID[:])
	copy(pkt[20:], attrs)

	ip, err := ParseXORMappedAddress(pkt)
	if err != nil {
		t.Fatalf("[P1] ParseXORMappedAddress() error = %v", err)
	}
	if !ip.Equal(expectedIP) {
		t.Errorf("[P1] ParseXORMappedAddress() = %v, want %v", ip, expectedIP)
	}
}

// TestDecodeXORMappedAddress_TooShort verifies that decodeXORMappedAddress
// returns an error when the attribute value is less than 4 bytes.
func TestDecodeXORMappedAddress_TooShort(t *testing.T) {
	t.Parallel()

	txID := [12]byte{}
	_, err := decodeXORMappedAddress([]byte{0x00, 0x01}, txID)
	if err == nil {
		t.Fatal("[P1] decodeXORMappedAddress() should return error for < 4 bytes")
	}
}
