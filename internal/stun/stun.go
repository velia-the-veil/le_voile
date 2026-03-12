// Package stun implements minimal STUN (RFC 5389) packet parsing for
// transparent interception of Binding Requests. Only the 20-byte header
// is parsed; attribute decoding is out of scope.
package stun

import "encoding/binary"

const (
	// MagicCookie is the fixed value at bytes 4-7 of every STUN message.
	MagicCookie uint32 = 0x2112A442

	// TypeBindingRequest is the STUN message type for Binding Request.
	TypeBindingRequest uint16 = 0x0001

	// TURN message types (RFC 5766) — these are NOT intercepted by Le Voile.
	// TURN traffic passes through transparently because TURN inherently hides
	// the client's IP (the peer sees the TURN server's IP, not the client's).
	TypeAllocateRequest  uint16 = 0x0003
	TypeCreatePermission uint16 = 0x0008
	TypeChannelBind      uint16 = 0x0009
	TypeSendIndication   uint16 = 0x0016
	TypeDataIndication   uint16 = 0x0017

	// HeaderSize is the fixed size of a STUN header in bytes.
	HeaderSize = 20

	// TransactionIDSize is the size of the transaction ID field.
	TransactionIDSize = 12
)

// Header represents a parsed STUN message header.
type Header struct {
	Type          uint16
	Length        uint16
	MagicCookie   uint32
	TransactionID [TransactionIDSize]byte
}

// byteOrder used for STUN header fields (network byte order).
var byteOrder = binary.BigEndian
