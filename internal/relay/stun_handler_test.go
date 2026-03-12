package relay

import (
	"bytes"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestSTUNHandler_Forward(t *testing.T) {
	// Start a mock UDP "STUN server" that echoes back with response type.
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

	// Temporarily allow the ephemeral port and loopback for testing.
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
		resp := make([]byte, n)
		copy(resp, buf[:n])
		resp[0] = 0x01
		resp[1] = 0x01
		udpConn.WriteToUDP(resp, clientAddr)
	}()

	handler := NewSTUNHandler()
	stunPkt := validSTUNBindingRequest()

	req := httptest.NewRequest(http.MethodPost, "/stun-relay", bytes.NewReader(stunPkt))
	req.Header.Set("Content-Type", stunMessageContentType)
	req.Header.Set("X-Stun-Target", mockAddr)
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	resp := rec.Result()
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("expected 200, got %d", resp.StatusCode)
	}

	ct := resp.Header.Get("Content-Type")
	if ct != stunMessageContentType {
		t.Errorf("expected content-type %q, got %q", stunMessageContentType, ct)
	}

	respBody, _ := io.ReadAll(resp.Body)
	if len(respBody) == 0 {
		t.Error("response body is empty")
	}

	if len(respBody) >= 2 && (respBody[0] != 0x01 || respBody[1] != 0x01) {
		t.Errorf("expected Binding Success Response type 0x0101, got 0x%02X%02X", respBody[0], respBody[1])
	}
}

func TestSTUNHandler_Timeout(t *testing.T) {
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

	// Temporarily allow the ephemeral port and loopback for testing.
	_, portStr, _ := net.SplitHostPort(mockAddr)
	var mockPort int
	fmt.Sscanf(portStr, "%d", &mockPort)
	allowedSTUNPorts[mockPort] = true
	defer delete(allowedSTUNPorts, mockPort)
	testSkipIPValidation = true
	defer func() { testSkipIPValidation = false }()

	handler := NewSTUNHandlerWithTimeout(500 * time.Millisecond)
	stunPkt := validSTUNBindingRequest()

	req := httptest.NewRequest(http.MethodPost, "/stun-relay", bytes.NewReader(stunPkt))
	req.Header.Set("Content-Type", stunMessageContentType)
	req.Header.Set("X-Stun-Target", mockAddr)
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadGateway {
		t.Errorf("expected 502 for timeout, got %d", rec.Code)
	}
}

func TestSTUNHandler_WrongMethod(t *testing.T) {
	handler := NewSTUNHandler()

	methods := []string{http.MethodGet, http.MethodPut, http.MethodDelete}
	for _, method := range methods {
		t.Run(method, func(t *testing.T) {
			req := httptest.NewRequest(method, "/stun-relay", nil)
			rec := httptest.NewRecorder()

			handler.ServeHTTP(rec, req)

			if rec.Code != http.StatusMethodNotAllowed {
				t.Errorf("expected 405 for %s, got %d", method, rec.Code)
			}
		})
	}
}

func TestSTUNHandler_WrongContentType(t *testing.T) {
	handler := NewSTUNHandler()

	tests := []struct {
		name        string
		contentType string
	}{
		{"text/plain", "text/plain"},
		{"application/json", "application/json"},
		{"empty", ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodPost, "/stun-relay", bytes.NewReader([]byte("data")))
			req.Header.Set("Content-Type", tt.contentType)
			rec := httptest.NewRecorder()

			handler.ServeHTTP(rec, req)

			if rec.Code != http.StatusBadRequest {
				t.Errorf("expected 400 for content-type %q, got %d", tt.contentType, rec.Code)
			}
		})
	}
}

func TestSTUNHandler_EmptyBody(t *testing.T) {
	handler := NewSTUNHandler()

	req := httptest.NewRequest(http.MethodPost, "/stun-relay", bytes.NewReader([]byte{}))
	req.Header.Set("Content-Type", stunMessageContentType)
	req.Header.Set("X-Stun-Target", "stun.l.google.com:19302")
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Errorf("expected 400 for empty body, got %d", rec.Code)
	}
}

func TestSTUNHandler_MissingTarget(t *testing.T) {
	handler := NewSTUNHandler()
	stunPkt := validSTUNBindingRequest()

	req := httptest.NewRequest(http.MethodPost, "/stun-relay", bytes.NewReader(stunPkt))
	req.Header.Set("Content-Type", stunMessageContentType)
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Errorf("expected 400 for missing target, got %d", rec.Code)
	}
}

func TestSTUNHandler_ForbiddenTarget(t *testing.T) {
	handler := NewSTUNHandler()
	stunPkt := validSTUNBindingRequest()

	tests := []struct {
		name   string
		target string
	}{
		{"wrong port", "stun.l.google.com:8080"},
		{"loopback", "127.0.0.1:3478"},
		{"private 10.x", "10.0.0.1:3478"},
		{"private 192.168.x", "192.168.1.1:3478"},
		{"private 172.16.x", "172.16.0.1:3478"},
		{"no port", "stun.l.google.com"},
		{"empty host", ":3478"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodPost, "/stun-relay", bytes.NewReader(stunPkt))
			req.Header.Set("Content-Type", stunMessageContentType)
			req.Header.Set("X-Stun-Target", tt.target)
			rec := httptest.NewRecorder()

			handler.ServeHTTP(rec, req)

			if rec.Code != http.StatusForbidden && rec.Code != http.StatusBadRequest {
				t.Errorf("expected 403 or 400 for target %q, got %d", tt.target, rec.Code)
			}
		})
	}
}

func TestSTUNHandler_InvalidSTUNPacket(t *testing.T) {
	handler := NewSTUNHandler()

	tests := []struct {
		name   string
		packet []byte
	}{
		{"too short", []byte{0x00, 0x01, 0x00}},
		{"wrong magic cookie", func() []byte {
			pkt := validSTUNBindingRequest()
			pkt[4] = 0xFF
			return pkt
		}()},
		{"RTP packet (first 2 bits = 10)", func() []byte {
			pkt := validSTUNBindingRequest()
			pkt[0] = 0x80
			return pkt
		}()},
		{"random data", []byte("this is not a STUN packet at all")},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodPost, "/stun-relay", bytes.NewReader(tt.packet))
			req.Header.Set("Content-Type", stunMessageContentType)
			req.Header.Set("X-Stun-Target", "stun.l.google.com:19302")
			rec := httptest.NewRecorder()

			handler.ServeHTTP(rec, req)

			if rec.Code != http.StatusBadRequest {
				t.Errorf("expected 400 for %s, got %d", tt.name, rec.Code)
			}
		})
	}
}

func TestSTUNHandler_OversizedBody(t *testing.T) {
	handler := NewSTUNHandler()

	// Build a packet larger than maxSTUNBodySize (1500).
	bigPacket := make([]byte, 1501)
	copy(bigPacket, validSTUNBindingRequest())

	req := httptest.NewRequest(http.MethodPost, "/stun-relay", bytes.NewReader(bigPacket))
	req.Header.Set("Content-Type", stunMessageContentType)
	req.Header.Set("X-Stun-Target", "stun.l.google.com:19302")
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Errorf("expected 400 for oversized body, got %d", rec.Code)
	}
}

func TestIsAllowedTarget(t *testing.T) {
	tests := []struct {
		name    string
		target  string
		allowed bool
	}{
		{"valid STUN port 3478", "8.8.8.8:3478", true},
		{"valid STUN port 5349", "8.8.8.8:5349", true},
		{"valid Google STUN", "74.125.250.129:19302", true},
		{"hostname with STUN port", "stun.l.google.com:19302", true},
		{"wrong port", "8.8.8.8:80", false},
		{"loopback", "127.0.0.1:3478", false},
		{"private 10.x", "10.0.0.1:3478", false},
		{"private 192.168.x", "192.168.1.1:3478", false},
		{"private 172.16.x", "172.16.0.1:3478", false},
		{"no port", "stun.l.google.com", false},
		{"empty", "", false},
	}

	h := NewSTUNHandler()
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := func() bool { _, ok := h.resolveAndValidateTarget(tt.target); return ok }()
			if got != tt.allowed {
				t.Errorf("resolveAndValidateTarget(%q) = %v, want %v", tt.target, got, tt.allowed)
			}
		})
	}
}

// validSTUNBindingRequest builds a minimal 20-byte STUN Binding Request for tests.
func validSTUNBindingRequest() []byte {
	pkt := make([]byte, 20)
	pkt[0] = 0x00
	pkt[1] = 0x01
	pkt[2] = 0x00
	pkt[3] = 0x00
	pkt[4] = 0x21
	pkt[5] = 0x12
	pkt[6] = 0xA4
	pkt[7] = 0x42
	for i := 8; i < 20; i++ {
		pkt[i] = 0xBB
	}
	return pkt
}
