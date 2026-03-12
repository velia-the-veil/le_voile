package leakcheck

import (
	"encoding/binary"
	"fmt"
	"net"
)

const (
	// STUN header constants.
	stunHeaderSize = 20
	stunMagicCookie uint32 = 0x2112A442

	// XOR-MAPPED-ADDRESS attribute type (RFC 5389 §15.2).
	attrTypeXORMappedAddress uint16 = 0x0020

	// Address family constants.
	familyIPv4 byte = 0x01
	familyIPv6 byte = 0x02
)

// parseXORMappedAddress extracts the XOR-MAPPED-ADDRESS attribute from a
// STUN Binding Response according to RFC 5389 §15.2.
//
// The response must include a valid 20-byte STUN header followed by
// attributes. The function scans all attributes looking for type 0x0020.
func ParseXORMappedAddress(response []byte) (net.IP, error) {
	if len(response) < stunHeaderSize {
		return nil, fmt.Errorf("leakcheck: stun: response too short (%d bytes)", len(response))
	}

	// Extract transaction ID for IPv6 XOR (bytes 8-19 of header).
	var transactionID [12]byte
	copy(transactionID[:], response[8:20])

	// Attribute length from header (bytes 2-3).
	attrLen := int(binary.BigEndian.Uint16(response[2:4]))
	if len(response) < stunHeaderSize+attrLen {
		attrLen = len(response) - stunHeaderSize
	}

	// Walk attributes.
	offset := stunHeaderSize
	end := stunHeaderSize + attrLen

	for offset+4 <= end {
		aType := binary.BigEndian.Uint16(response[offset : offset+2])
		aLen := int(binary.BigEndian.Uint16(response[offset+2 : offset+4]))
		offset += 4

		if offset+aLen > end {
			break
		}

		if aType == attrTypeXORMappedAddress {
			return decodeXORMappedAddress(response[offset:offset+aLen], transactionID)
		}

		// Attributes are padded to 4-byte boundaries.
		offset += aLen
		if pad := aLen % 4; pad != 0 {
			offset += 4 - pad
		}
	}

	return nil, fmt.Errorf("leakcheck: stun: XOR-MAPPED-ADDRESS attribute not found")
}

// decodeXORMappedAddress decodes the value of an XOR-MAPPED-ADDRESS attribute.
func decodeXORMappedAddress(data []byte, transactionID [12]byte) (net.IP, error) {
	if len(data) < 4 {
		return nil, fmt.Errorf("leakcheck: stun: XOR-MAPPED-ADDRESS too short (%d bytes)", len(data))
	}

	family := data[1]

	switch family {
	case familyIPv4:
		if len(data) < 8 {
			return nil, fmt.Errorf("leakcheck: stun: XOR-MAPPED-ADDRESS IPv4 too short (%d bytes)", len(data))
		}
		// X-Address: IP XOR with Magic Cookie (4 bytes).
		cookieBytes := make([]byte, 4)
		binary.BigEndian.PutUint32(cookieBytes, stunMagicCookie)

		ip := make(net.IP, 4)
		for i := 0; i < 4; i++ {
			ip[i] = data[4+i] ^ cookieBytes[i]
		}
		return ip, nil

	case familyIPv6:
		if len(data) < 20 {
			return nil, fmt.Errorf("leakcheck: stun: XOR-MAPPED-ADDRESS IPv6 too short (%d bytes)", len(data))
		}
		// X-Address: IP XOR with Magic Cookie (4 bytes) + Transaction ID (12 bytes).
		xorKey := make([]byte, 16)
		binary.BigEndian.PutUint32(xorKey[0:4], stunMagicCookie)
		copy(xorKey[4:16], transactionID[:])

		ip := make(net.IP, 16)
		for i := 0; i < 16; i++ {
			ip[i] = data[4+i] ^ xorKey[i]
		}
		return ip, nil

	default:
		return nil, fmt.Errorf("leakcheck: stun: unknown address family 0x%02X", family)
	}
}
