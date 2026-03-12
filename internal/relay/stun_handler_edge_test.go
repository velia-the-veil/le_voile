package relay

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
)

func TestSTUNHandler_ValidSTUNWithAttributes(t *testing.T) {
	// Not parallel: mutates package-level allowedSTUNPorts and testSkipIPValidation.

	// Start a mock UDP "STUN server" that echoes back a Binding Success Response.
	udpAddr, err := net.ResolveUDPAddr("udp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	udpConn, err := net.ListenUDP("udp", udpAddr)
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	defer udpConn.Close()

	mockAddr := udpConn.LocalAddr().String()
	_, portStr, _ := net.SplitHostPort(mockAddr)
	var mockPort int
	fmt.Sscanf(portStr, "%d", &mockPort)

	allowedSTUNPorts[mockPort] = true
	defer delete(allowedSTUNPorts, mockPort)
	testSkipIPValidation = true
	defer func() { testSkipIPValidation = false }()

	go func() {
		buf := make([]byte, 1500)
		n, clientAddr, err := udpConn.ReadFromUDP(buf)
		if err != nil {
			return
		}
		// Echo back as Binding Success Response.
		resp := make([]byte, n)
		copy(resp, buf[:n])
		resp[0] = 0x01
		resp[1] = 0x01
		udpConn.WriteToUDP(resp, clientAddr)
	}()

	// Build a STUN Binding Request with attribute data (> 20 bytes).
	pkt := validSTUNBindingRequest()
	// Add 8 bytes of attribute data (type=0x0001, length=4, value=4 bytes).
	attrData := []byte{0x00, 0x01, 0x00, 0x04, 0xDE, 0xAD, 0xBE, 0xEF}
	pkt = append(pkt, attrData...)
	// Update the STUN message length field (bytes 2-3) to reflect attribute size.
	binary.BigEndian.PutUint16(pkt[2:4], uint16(len(attrData)))

	handler := NewSTUNHandler()

	req := httptest.NewRequest(http.MethodPost, "/stun-relay", bytes.NewReader(pkt))
	req.Header.Set("Content-Type", stunMessageContentType)
	req.Header.Set("X-Stun-Target", mockAddr)
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", rec.Code)
	}

	respBody := rec.Body.Bytes()
	if len(respBody) < stunHeaderSize {
		t.Fatalf("response too short: %d bytes", len(respBody))
	}

	// Verify the echoed response preserves the attribute data length.
	if len(respBody) != len(pkt) {
		t.Errorf("response length = %d, want %d", len(respBody), len(pkt))
	}

	// Verify it's a Binding Success Response.
	if respBody[0] != 0x01 || respBody[1] != 0x01 {
		t.Errorf("expected Binding Success Response 0x0101, got 0x%02X%02X", respBody[0], respBody[1])
	}
}

func TestSTUNHandler_ConcurrentRequests(t *testing.T) {
	// Not parallel: mutates package-level allowedSTUNPorts and testSkipIPValidation.

	// Start a mock UDP server that responds to each request.
	udpAddr, err := net.ResolveUDPAddr("udp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	udpConn, err := net.ListenUDP("udp", udpAddr)
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	defer udpConn.Close()

	mockAddr := udpConn.LocalAddr().String()
	_, portStr, _ := net.SplitHostPort(mockAddr)
	var mockPort int
	fmt.Sscanf(portStr, "%d", &mockPort)

	allowedSTUNPorts[mockPort] = true
	defer delete(allowedSTUNPorts, mockPort)
	testSkipIPValidation = true
	defer func() { testSkipIPValidation = false }()

	// UDP echo goroutine that handles multiple requests.
	go func() {
		buf := make([]byte, 1500)
		for {
			n, clientAddr, err := udpConn.ReadFromUDP(buf)
			if err != nil {
				return
			}
			resp := make([]byte, n)
			copy(resp, buf[:n])
			resp[0] = 0x01
			resp[1] = 0x01
			udpConn.WriteToUDP(resp, clientAddr)
		}
	}()

	handler := NewSTUNHandler()
	const numRequests = 10

	var wg sync.WaitGroup
	wg.Add(numRequests)

	results := make([]int, numRequests)

	for i := 0; i < numRequests; i++ {
		go func(idx int) {
			defer wg.Done()

			stunPkt := validSTUNBindingRequest()
			req := httptest.NewRequest(http.MethodPost, "/stun-relay", bytes.NewReader(stunPkt))
			req.Header.Set("Content-Type", stunMessageContentType)
			req.Header.Set("X-Stun-Target", mockAddr)
			rec := httptest.NewRecorder()

			handler.ServeHTTP(rec, req)
			results[idx] = rec.Code
		}(i)
	}

	wg.Wait()

	for i, code := range results {
		if code != http.StatusOK {
			t.Errorf("request %d: expected 200, got %d", i, code)
		}
	}
}

func TestSTUNHandler_AllowedTarget_HostnameSTUNPort(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		target string
		want   bool
	}{
		{"google STUN hostname", "stun.l.google.com:3478", true},
		{"google STUN alt port", "stun.l.google.com:19302", true},
		// stun.example.org does not resolve — rejected to prevent SSRF via DNS rebinding.
		{"unresolvable hostname DTLS port", "stun.example.org:5349", false},
		{"hostname wrong port", "stun.example.org:8080", false},
	}

	h := NewSTUNHandler()
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			_, got := h.resolveAndValidateTarget(tt.target)
			if got != tt.want {
				t.Errorf("resolveAndValidateTarget(%q) = %v, want %v", tt.target, got, tt.want)
			}
		})
	}
}

func TestSTUNHandler_AllowedTarget_IPv6(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		target string
		want   bool
	}{
		{"public IPv6 STUN port", "[2607:f8b0:4004:800::200e]:3478", true},
		{"public IPv6 Google STUN", "[2607:f8b0:4004:800::200e]:19302", true},
		{"public IPv6 DTLS port", "[2001:4860:4864:20::e]:5349", true},
		{"public IPv6 wrong port", "[2607:f8b0:4004:800::200e]:8080", false},
		{"loopback IPv6", "[::1]:3478", false},
	}

	h := NewSTUNHandler()
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			_, got := h.resolveAndValidateTarget(tt.target)
			if got != tt.want {
				t.Errorf("resolveAndValidateTarget(%q) = %v, want %v", tt.target, got, tt.want)
			}
		})
	}
}
