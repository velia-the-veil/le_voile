package relay

import (
	"bytes"
	"context"
	"encoding/binary"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// buildDNSQuery constructs a minimal DNS wire-format query for the given domain
// and record type (e.g., 1 for A record). This follows RFC 1035 §4.1.
func buildDNSQuery(domain string, qtype uint16) []byte {
	var buf bytes.Buffer

	// Header (12 bytes)
	binary.Write(&buf, binary.BigEndian, uint16(0x1234)) // ID
	binary.Write(&buf, binary.BigEndian, uint16(0x0100)) // Flags: standard query, recursion desired
	binary.Write(&buf, binary.BigEndian, uint16(1))      // QDCOUNT
	binary.Write(&buf, binary.BigEndian, uint16(0))      // ANCOUNT
	binary.Write(&buf, binary.BigEndian, uint16(0))      // NSCOUNT
	binary.Write(&buf, binary.BigEndian, uint16(0))      // ARCOUNT

	// Question section
	labels := strings.Split(domain, ".")
	for _, label := range labels {
		buf.WriteByte(byte(len(label)))
		buf.WriteString(label)
	}
	buf.WriteByte(0) // Root label

	binary.Write(&buf, binary.BigEndian, qtype)        // QTYPE
	binary.Write(&buf, binary.BigEndian, uint16(0x01)) // QCLASS: IN

	return buf.Bytes()
}

func TestDoHHandler_ValidQuery(t *testing.T) {
	// Mock upstream that returns a valid DNS response
	mockUpstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Content-Type") != dnsMessageContentType {
			t.Errorf("upstream got wrong content-type: %q", r.Header.Get("Content-Type"))
		}

		body, _ := io.ReadAll(r.Body)
		if len(body) == 0 {
			t.Error("upstream got empty body")
		}

		// Return a mock DNS response (same body for simplicity, it's wire-format)
		w.Header().Set("Content-Type", dnsMessageContentType)
		w.WriteHeader(http.StatusOK)
		// Construct a minimal DNS response: flip QR bit in the query
		resp := make([]byte, len(body))
		copy(resp, body)
		resp[2] |= 0x80 // Set QR bit (response)
		w.Write(resp)
	}))
	defer mockUpstream.Close()

	handler := NewDoHHandler([]string{mockUpstream.URL}, mockUpstream.Client())
	dnsQuery := buildDNSQuery("example.com", 1) // Type A

	req := httptest.NewRequest(http.MethodPost, "/dns-query", bytes.NewReader(dnsQuery))
	req.Header.Set("Content-Type", dnsMessageContentType)
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	resp := rec.Result()
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("expected 200, got %d", resp.StatusCode)
	}

	ct := resp.Header.Get("Content-Type")
	if ct != dnsMessageContentType {
		t.Errorf("expected content-type %q, got %q", dnsMessageContentType, ct)
	}

	respBody, _ := io.ReadAll(resp.Body)
	if len(respBody) == 0 {
		t.Error("response body is empty")
	}

	// Verify QR bit is set (response flag)
	if respBody[2]&0x80 == 0 {
		t.Error("expected QR bit set in DNS response")
	}
}

func TestDoHHandler_WrongMethod(t *testing.T) {
	handler := NewDoHHandler([]string{"https://localhost"}, nil)

	methods := []string{http.MethodGet, http.MethodPut, http.MethodDelete, http.MethodPatch}
	for _, method := range methods {
		t.Run(method, func(t *testing.T) {
			req := httptest.NewRequest(method, "/dns-query", nil)
			rec := httptest.NewRecorder()

			handler.ServeHTTP(rec, req)

			if rec.Code != http.StatusMethodNotAllowed {
				t.Errorf("expected 405 for %s, got %d", method, rec.Code)
			}
		})
	}
}

func TestDoHHandler_WrongContentType(t *testing.T) {
	handler := NewDoHHandler([]string{"https://localhost"}, nil)

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
			req := httptest.NewRequest(http.MethodPost, "/dns-query", bytes.NewReader([]byte("data")))
			req.Header.Set("Content-Type", tt.contentType)
			rec := httptest.NewRecorder()

			handler.ServeHTTP(rec, req)

			if rec.Code != http.StatusBadRequest {
				t.Errorf("expected 400 for content-type %q, got %d", tt.contentType, rec.Code)
			}
		})
	}
}

func TestDoHHandler_EmptyBody(t *testing.T) {
	handler := NewDoHHandler([]string{"https://localhost"}, nil)

	req := httptest.NewRequest(http.MethodPost, "/dns-query", bytes.NewReader([]byte{}))
	req.Header.Set("Content-Type", dnsMessageContentType)
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Errorf("expected 400 for empty body, got %d", rec.Code)
	}
}

func TestDoHHandler_UpstreamError(t *testing.T) {
	// Mock upstream that always fails
	mockUpstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Close the connection immediately to simulate failure
		hj, ok := w.(http.Hijacker)
		if ok {
			conn, _, _ := hj.Hijack()
			conn.Close()
			return
		}
		// Fallback: return 500
		w.WriteHeader(http.StatusInternalServerError)
	}))
	// Close the server immediately so the client can't connect
	mockUpstream.Close()

	handler := NewDoHHandler([]string{mockUpstream.URL}, nil)
	dnsQuery := buildDNSQuery("example.com", 1)

	req := httptest.NewRequest(http.MethodPost, "/dns-query", bytes.NewReader(dnsQuery))
	req.Header.Set("Content-Type", dnsMessageContentType)
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadGateway {
		t.Errorf("expected 502 for upstream error, got %d", rec.Code)
	}
}

// TestDoHHandler_FallbackOnPrimaryError verifies that when the primary upstream
// fails, the handler falls back to the secondary and updates activeIdx.
func TestDoHHandler_FallbackOnPrimaryError(t *testing.T) {
	// Primary that always fails (closed immediately)
	failingPrimary := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	failingPrimary.Close()

	// Fallback that always succeeds
	var fallbackHits atomic.Int32
	fallback := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fallbackHits.Add(1)
		body, _ := io.ReadAll(r.Body)
		resp := make([]byte, len(body))
		copy(resp, body)
		if len(resp) > 2 {
			resp[2] |= 0x80
		}
		w.Header().Set("Content-Type", dnsMessageContentType)
		w.WriteHeader(http.StatusOK)
		w.Write(resp)
	}))
	defer fallback.Close()

	handler := NewDoHHandler([]string{failingPrimary.URL, fallback.URL}, fallback.Client())
	handler.recoveryInterval = 1 * time.Hour // prevent recovery during test

	dnsQuery := buildDNSQuery("example.com", 1)
	req := httptest.NewRequest(http.MethodPost, "/dns-query", bytes.NewReader(dnsQuery))
	req.Header.Set("Content-Type", dnsMessageContentType)
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("expected 200 via fallback, got %d", rec.Code)
	}
	if fallbackHits.Load() == 0 {
		t.Error("expected fallback upstream to be hit")
	}

	// activeIdx should have switched to 1
	handler.mu.RLock()
	active := handler.activeIdx
	handler.mu.RUnlock()
	if active != 1 {
		t.Errorf("expected activeIdx=1 after fallback, got %d", active)
	}
}

// TestDoHHandler_FallbackOnPrimaryHTTPError verifies that when the primary
// upstream returns HTTP 500, the handler falls back to the secondary.
func TestDoHHandler_FallbackOnPrimaryHTTPError(t *testing.T) {
	// Primary returns 500 (server error)
	var primaryHits atomic.Int32
	primary := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		primaryHits.Add(1)
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer primary.Close()

	// Fallback that succeeds
	var fallbackHits atomic.Int32
	fallback := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fallbackHits.Add(1)
		body, _ := io.ReadAll(r.Body)
		resp := make([]byte, len(body))
		copy(resp, body)
		if len(resp) > 2 {
			resp[2] |= 0x80
		}
		w.Header().Set("Content-Type", dnsMessageContentType)
		w.WriteHeader(http.StatusOK)
		w.Write(resp)
	}))
	defer fallback.Close()

	handler := NewDoHHandler([]string{primary.URL, fallback.URL}, primary.Client())
	handler.recoveryInterval = 1 * time.Hour

	dnsQuery := buildDNSQuery("example.com", 1)
	req := httptest.NewRequest(http.MethodPost, "/dns-query", bytes.NewReader(dnsQuery))
	req.Header.Set("Content-Type", dnsMessageContentType)
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("expected 200 via fallback after primary 500, got %d", rec.Code)
	}
	if primaryHits.Load() == 0 {
		t.Error("expected primary to be attempted")
	}
	if fallbackHits.Load() == 0 {
		t.Error("expected fallback to be hit after primary 500")
	}

	handler.mu.RLock()
	active := handler.activeIdx
	handler.mu.RUnlock()
	if active != 1 {
		t.Errorf("expected activeIdx=1 after HTTP error fallback, got %d", active)
	}
}

// TestDoHHandler_AllUpstreamsFail verifies that when all upstreams fail,
// the handler returns 502.
func TestDoHHandler_AllUpstreamsFail(t *testing.T) {
	primary := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	primary.Close()
	secondary := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	secondary.Close()

	handler := NewDoHHandler([]string{primary.URL, secondary.URL}, nil)

	dnsQuery := buildDNSQuery("example.com", 1)
	req := httptest.NewRequest(http.MethodPost, "/dns-query", bytes.NewReader(dnsQuery))
	req.Header.Set("Content-Type", dnsMessageContentType)
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadGateway {
		t.Errorf("expected 502 when all upstreams fail, got %d", rec.Code)
	}
}

// TestDoHHandler_RecoveryToPrimary verifies that the recovery goroutine resets
// activeIdx to 0 when the primary becomes reachable again.
func TestDoHHandler_RecoveryToPrimary(t *testing.T) {
	var primaryUp atomic.Bool

	primary := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !primaryUp.Load() {
			hj, ok := w.(http.Hijacker)
			if ok {
				conn, _, _ := hj.Hijack()
				conn.Close()
				return
			}
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		// Primary is up: return any HTTP response (200 or even 400 — just not a network error)
		w.Header().Set("Content-Type", dnsMessageContentType)
		w.WriteHeader(http.StatusOK)
	}))
	defer primary.Close()

	fallback := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", dnsMessageContentType)
		w.WriteHeader(http.StatusOK)
	}))
	defer fallback.Close()

	handler := NewDoHHandler([]string{primary.URL, fallback.URL}, primary.Client())
	handler.recoveryInterval = 20 * time.Millisecond

	// Simulate fallback is active (activeIdx=1)
	handler.mu.Lock()
	handler.activeIdx = 1
	handler.mu.Unlock()

	// Use a channel so the test is deterministic rather than relying on a polling sleep loop.
	recovered := make(chan struct{}, 1)
	handler.onRecovery = func() {
		select {
		case recovered <- struct{}{}:
		default:
		}
	}

	// Mark primary as up before starting the goroutine.
	primaryUp.Store(true)

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	handler.Start(ctx)

	select {
	case <-recovered:
	case <-time.After(500 * time.Millisecond):
		t.Fatal("recovery goroutine did not reset activeIdx within 500ms")
	}

	handler.mu.RLock()
	active := handler.activeIdx
	handler.mu.RUnlock()
	if active != 0 {
		t.Errorf("expected activeIdx=0 after recovery, got %d", active)
	}
}
