package stun

import (
	"testing"
)

// validBindingRequest builds a minimal 20-byte STUN Binding Request.
func validBindingRequest() []byte {
	pkt := make([]byte, HeaderSize)
	// Type: Binding Request 0x0001
	byteOrder.PutUint16(pkt[0:2], TypeBindingRequest)
	// Length: 0 (no attributes)
	byteOrder.PutUint16(pkt[2:4], 0)
	// Magic Cookie
	byteOrder.PutUint32(pkt[4:8], MagicCookie)
	// Transaction ID: 12 bytes of 0xAA
	for i := 8; i < 20; i++ {
		pkt[i] = 0xAA
	}
	return pkt
}

func TestIsSTUN(t *testing.T) {
	tests := []struct {
		name   string
		packet []byte
		want   bool
	}{
		{
			name:   "valid STUN Binding Request",
			packet: validBindingRequest(),
			want:   true,
		},
		{
			name:   "nil packet",
			packet: nil,
			want:   false,
		},
		{
			name:   "empty packet",
			packet: []byte{},
			want:   false,
		},
		{
			name:   "too short (19 bytes)",
			packet: make([]byte, 19),
			want:   false,
		},
		{
			name: "wrong magic cookie",
			packet: func() []byte {
				pkt := validBindingRequest()
				byteOrder.PutUint32(pkt[4:8], 0xDEADBEEF)
				return pkt
			}(),
			want: false,
		},
		{
			name: "RTP packet (first 2 bits = 10)",
			packet: func() []byte {
				pkt := validBindingRequest()
				pkt[0] = 0x80 // 10000000 — RTP
				return pkt
			}(),
			want: false,
		},
		{
			name: "first 2 bits = 00, valid magic cookie, non-binding type",
			packet: func() []byte {
				pkt := validBindingRequest()
				byteOrder.PutUint16(pkt[0:2], 0x0101) // Binding Success Response
				return pkt
			}(),
			want: true,
		},
		{
			name:   "exactly 20 bytes with valid header",
			packet: validBindingRequest(),
			want:   true,
		},
		{
			name: "longer packet with valid header",
			packet: func() []byte {
				pkt := make([]byte, 100)
				copy(pkt, validBindingRequest())
				byteOrder.PutUint16(pkt[2:4], 80) // length = 80 attributes
				return pkt
			}(),
			want: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := IsSTUN(tt.packet)
			if got != tt.want {
				t.Errorf("IsSTUN() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestIsTURN(t *testing.T) {
	tests := []struct {
		name   string
		packet []byte
		want   bool
	}{
		{
			name: "Allocate Request (0x0003)",
			packet: func() []byte {
				pkt := validBindingRequest()
				byteOrder.PutUint16(pkt[0:2], TypeAllocateRequest)
				return pkt
			}(),
			want: true,
		},
		{
			name: "CreatePermission (0x0008)",
			packet: func() []byte {
				pkt := validBindingRequest()
				byteOrder.PutUint16(pkt[0:2], TypeCreatePermission)
				return pkt
			}(),
			want: true,
		},
		{
			name: "ChannelBind (0x0009)",
			packet: func() []byte {
				pkt := validBindingRequest()
				byteOrder.PutUint16(pkt[0:2], TypeChannelBind)
				return pkt
			}(),
			want: true,
		},
		{
			name: "SendIndication (0x0016)",
			packet: func() []byte {
				pkt := validBindingRequest()
				byteOrder.PutUint16(pkt[0:2], TypeSendIndication)
				return pkt
			}(),
			want: true,
		},
		{
			name: "DataIndication (0x0017)",
			packet: func() []byte {
				pkt := validBindingRequest()
				byteOrder.PutUint16(pkt[0:2], TypeDataIndication)
				return pkt
			}(),
			want: true,
		},
		{
			name:   "Binding Request (0x0001) — not TURN",
			packet: validBindingRequest(),
			want:   false,
		},
		{
			name:   "non-STUN packet — not TURN",
			packet: []byte("not a STUN packet"),
			want:   false,
		},
		{
			name:   "nil packet",
			packet: nil,
			want:   false,
		},
		{
			name:   "too short (1 byte)",
			packet: []byte{0x00},
			want:   false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := IsTURN(tt.packet)
			if got != tt.want {
				t.Errorf("IsTURN() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestIsBindingRequest(t *testing.T) {
	tests := []struct {
		name   string
		packet []byte
		want   bool
	}{
		{
			name:   "valid Binding Request",
			packet: validBindingRequest(),
			want:   true,
		},
		{
			name:   "nil packet",
			packet: nil,
			want:   false,
		},
		{
			name:   "too short",
			packet: make([]byte, 1),
			want:   false,
		},
		{
			name: "Binding Success Response (0x0101)",
			packet: func() []byte {
				pkt := validBindingRequest()
				byteOrder.PutUint16(pkt[0:2], 0x0101)
				return pkt
			}(),
			want: false,
		},
		{
			name: "Binding Error Response (0x0111)",
			packet: func() []byte {
				pkt := validBindingRequest()
				byteOrder.PutUint16(pkt[0:2], 0x0111)
				return pkt
			}(),
			want: false,
		},
		{
			name: "STUN Indication (0x0011)",
			packet: func() []byte {
				pkt := validBindingRequest()
				byteOrder.PutUint16(pkt[0:2], 0x0011)
				return pkt
			}(),
			want: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := IsBindingRequest(tt.packet)
			if got != tt.want {
				t.Errorf("IsBindingRequest() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestParseHeader(t *testing.T) {
	tests := []struct {
		name    string
		packet  []byte
		want    *Header
		wantErr bool
	}{
		{
			name:   "valid Binding Request",
			packet: validBindingRequest(),
			want: &Header{
				Type:          TypeBindingRequest,
				Length:        0,
				MagicCookie:   MagicCookie,
				TransactionID: [12]byte{0xAA, 0xAA, 0xAA, 0xAA, 0xAA, 0xAA, 0xAA, 0xAA, 0xAA, 0xAA, 0xAA, 0xAA},
			},
			wantErr: false,
		},
		{
			name:    "nil packet",
			packet:  nil,
			want:    nil,
			wantErr: true,
		},
		{
			name:    "too short (19 bytes)",
			packet:  make([]byte, 19),
			want:    nil,
			wantErr: true,
		},
		{
			name: "wrong magic cookie",
			packet: func() []byte {
				pkt := validBindingRequest()
				byteOrder.PutUint32(pkt[4:8], 0x00000000)
				return pkt
			}(),
			want:    nil,
			wantErr: true,
		},
		{
			name: "valid Binding Success Response",
			packet: func() []byte {
				pkt := validBindingRequest()
				byteOrder.PutUint16(pkt[0:2], 0x0101)
				return pkt
			}(),
			want: &Header{
				Type:          0x0101,
				Length:        0,
				MagicCookie:   MagicCookie,
				TransactionID: [12]byte{0xAA, 0xAA, 0xAA, 0xAA, 0xAA, 0xAA, 0xAA, 0xAA, 0xAA, 0xAA, 0xAA, 0xAA},
			},
			wantErr: false,
		},
		{
			name: "first 2 bits not 00 (RTP)",
			packet: func() []byte {
				pkt := validBindingRequest()
				pkt[0] = 0x80
				return pkt
			}(),
			want:    nil,
			wantErr: true,
		},
		{
			name: "with attributes length",
			packet: func() []byte {
				pkt := make([]byte, 40)
				copy(pkt, validBindingRequest())
				byteOrder.PutUint16(pkt[2:4], 20) // 20 bytes of attributes
				return pkt
			}(),
			want: &Header{
				Type:          TypeBindingRequest,
				Length:        20,
				MagicCookie:   MagicCookie,
				TransactionID: [12]byte{0xAA, 0xAA, 0xAA, 0xAA, 0xAA, 0xAA, 0xAA, 0xAA, 0xAA, 0xAA, 0xAA, 0xAA},
			},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ParseHeader(tt.packet)
			if (err != nil) != tt.wantErr {
				t.Errorf("ParseHeader() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if tt.want == nil && got != nil {
				t.Errorf("ParseHeader() = %v, want nil", got)
				return
			}
			if tt.want != nil {
				if got == nil {
					t.Fatalf("ParseHeader() = nil, want %+v", tt.want)
				}
				if got.Type != tt.want.Type {
					t.Errorf("Type = 0x%04X, want 0x%04X", got.Type, tt.want.Type)
				}
				if got.Length != tt.want.Length {
					t.Errorf("Length = %d, want %d", got.Length, tt.want.Length)
				}
				if got.MagicCookie != tt.want.MagicCookie {
					t.Errorf("MagicCookie = 0x%08X, want 0x%08X", got.MagicCookie, tt.want.MagicCookie)
				}
				if got.TransactionID != tt.want.TransactionID {
					t.Errorf("TransactionID = %v, want %v", got.TransactionID, tt.want.TransactionID)
				}
			}
		})
	}
}
