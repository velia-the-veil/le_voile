package leakcheck

import (
	"encoding/binary"
	"net"
	"testing"
)

// buildSTUNResponse creates a STUN Binding Response with an XOR-MAPPED-ADDRESS
// attribute encoding the given IPv4 address.
func buildSTUNResponse(ip net.IP, transactionID [12]byte) []byte {
	ip4 := ip.To4()
	if ip4 == nil {
		panic("test helper only supports IPv4")
	}

	// XOR-MAPPED-ADDRESS attribute value (8 bytes for IPv4):
	// byte 0: reserved (0x00)
	// byte 1: family (0x01 = IPv4)
	// bytes 2-3: X-Port (port XOR'd with magic cookie MSB 16 bits)
	// bytes 4-7: X-Address (IP XOR'd with magic cookie)
	attrValue := make([]byte, 8)
	attrValue[0] = 0x00
	attrValue[1] = familyIPv4

	// X-Port: 12345 XOR 0x2112 = some value (we don't use port in our parser)
	binary.BigEndian.PutUint16(attrValue[2:4], 12345^0x2112)

	// X-Address: IP XOR magic cookie
	cookieBytes := make([]byte, 4)
	binary.BigEndian.PutUint32(cookieBytes, stunMagicCookie)
	for i := 0; i < 4; i++ {
		attrValue[4+i] = ip4[i] ^ cookieBytes[i]
	}

	// Full attribute: type (2) + length (2) + value (8) = 12 bytes
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

func TestParseXORMappedAddress(t *testing.T) {
	txID := [12]byte{0x01, 0x02, 0x03, 0x04, 0x05, 0x06, 0x07, 0x08, 0x09, 0x0A, 0x0B, 0x0C}

	tests := []struct {
		name    string
		resp    []byte
		wantIP  net.IP
		wantErr bool
	}{
		{
			name:    "valid IPv4 response — 203.0.113.42",
			resp:    buildSTUNResponse(net.IPv4(203, 0, 113, 42), txID),
			wantIP:  net.IPv4(203, 0, 113, 42).To4(),
			wantErr: false,
		},
		{
			name:    "valid IPv4 response — 192.168.1.1",
			resp:    buildSTUNResponse(net.IPv4(192, 168, 1, 1), txID),
			wantIP:  net.IPv4(192, 168, 1, 1).To4(),
			wantErr: false,
		},
		{
			name:    "valid IPv4 response — 10.0.0.1",
			resp:    buildSTUNResponse(net.IPv4(10, 0, 0, 1), txID),
			wantIP:  net.IPv4(10, 0, 0, 1).To4(),
			wantErr: false,
		},
		{
			name:    "too short response",
			resp:    make([]byte, 10),
			wantIP:  nil,
			wantErr: true,
		},
		{
			name: "no XOR-MAPPED-ADDRESS attribute",
			resp: func() []byte {
				// Valid STUN header but with a different attribute type.
				pkt := make([]byte, stunHeaderSize+8)
				binary.BigEndian.PutUint16(pkt[0:2], 0x0101)
				binary.BigEndian.PutUint16(pkt[2:4], 8)
				binary.BigEndian.PutUint32(pkt[4:8], stunMagicCookie)
				copy(pkt[8:20], txID[:])
				// Attribute type 0x0001 (MAPPED-ADDRESS, not XOR)
				binary.BigEndian.PutUint16(pkt[20:22], 0x0001)
				binary.BigEndian.PutUint16(pkt[22:24], 4)
				return pkt
			}(),
			wantIP:  nil,
			wantErr: true,
		},
		{
			name:    "nil response",
			resp:    nil,
			wantIP:  nil,
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ip, err := ParseXORMappedAddress(tt.resp)
			if (err != nil) != tt.wantErr {
				t.Errorf("ParseXORMappedAddress() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if tt.wantIP != nil && !ip.Equal(tt.wantIP) {
				t.Errorf("ParseXORMappedAddress() = %v, want %v", ip, tt.wantIP)
			}
		})
	}
}
