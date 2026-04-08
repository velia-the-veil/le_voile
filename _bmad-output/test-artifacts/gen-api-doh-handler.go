package relay

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestDoHHandler_BodyAtMaxSize(t *testing.T) {
	// Body exactly at maxDNSBodySize (65535 bytes) should be accepted.
	mockUpstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		w.Header().Set("Content-Type", dnsMessageContentType)
		w.WriteHeader(http.StatusOK)
		w.Write(body)
	}))
	defer mockUpstream.Close()

	handler := NewDoHHandler(mockUpstream.URL, mockUpstream.Client())

	body := make([]byte, maxDNSBodySize) // exactly 65535 bytes
	for i := range body {
		body[i] = 0xAA
	}

	req := httptest.NewRequest(http.MethodPost, "/dns-query", bytes.NewReader(body))
	req.Header.Set("Content-Type", dnsMessageContentType)
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("expected 200 for body at max size (%d bytes), got %d", maxDNSBodySize, rec.Code)
	}
}

func TestDoHHandler_BodyExceedsMaxSize(t *testing.T) {
	// Body at maxDNSBodySize+1 (65536 bytes) should be rejected.
	handler := NewDoHHandler("https://localhost", nil)

	body := make([]byte, maxDNSBodySize+1) // 65536 bytes
	for i := range body {
		body[i] = 0xBB
	}

	req := httptest.NewRequest(http.MethodPost, "/dns-query", bytes.NewReader(body))
	req.Header.Set("Content-Type", dnsMessageContentType)
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Errorf("expected 400 for body exceeding max size (%d bytes), got %d", maxDNSBodySize+1, rec.Code)
	}
}

func TestDoHHandler_ContextCancellation(t *testing.T) {
	// Upstream that blocks until context is cancelled, simulating a slow resolver.
	mockUpstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Block until the request context is done
		<-r.Context().Done()
		// Don't write anything — the client cancelled
	}))
	defer mockUpstream.Close()

	handler := NewDoHHandler(mockUpstream.URL, &http.Client{Timeout: 10 * time.Second})
	dnsQuery := buildDNSQuery("example.com", 1)

	ctx, cancel := context.WithCancel(context.Background())
	req := httptest.NewRequest(http.MethodPost, "/dns-query", bytes.NewReader(dnsQuery))
	req = req.WithContext(ctx)
	req.Header.Set("Content-Type", dnsMessageContentType)
	rec := httptest.NewRecorder()

	// Cancel context immediately to simulate client disconnection
	cancel()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadGateway {
		t.Errorf("expected 502 for cancelled context, got %d", rec.Code)
	}
}

// errReadCloser is an io.ReadCloser that returns an error on Read.
type errReadCloser struct {
	err error
}

func (e *errReadCloser) Read(p []byte) (int, error) {
	return 0, e.err
}

func (e *errReadCloser) Close() error {
	return nil
}

func TestDoHHandler_UpstreamResponseReadError(t *testing.T) {
	// Upstream returns a response whose body errors on read.
	// We use a real upstream that writes partial data then resets the connection.
	mockUpstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", dnsMessageContentType)
		w.WriteHeader(http.StatusOK)
		// Flush headers then hijack to force a read error on the client
		if f, ok := w.(http.Flusher); ok {
			f.Flush()
		}
		hj, ok := w.(http.Hijacker)
		if !ok {
			// Fallback: just write nothing (won't trigger read error but tests the path)
			return
		}
		conn, _, err := hj.Hijack()
		if err != nil {
			return
		}
		// Close the connection abruptly mid-response
		conn.Close()
	}))
	defer mockUpstream.Close()

	handler := NewDoHHandler(mockUpstream.URL, mockUpstream.Client())
	dnsQuery := buildDNSQuery("example.com", 1)

	req := httptest.NewRequest(http.MethodPost, "/dns-query", bytes.NewReader(dnsQuery))
	req.Header.Set("Content-Type", dnsMessageContentType)
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	// The handler should return either 502 (read error) or 200 (if hijack not supported).
	// On most Go httptest servers, hijack works, so we expect 502 or a response with empty body.
	// The key assertion: no panic, and status is a valid HTTP status.
	if rec.Code != http.StatusBadGateway && rec.Code != http.StatusOK {
		t.Errorf("expected 502 or 200 for upstream read error, got %d", rec.Code)
	}
}

func TestDoHHandler_UpstreamResponseReadError_Unit(t *testing.T) {
	// Unit-level test: use a custom RoundTripper that returns a response
	// with a body that errors on Read.
	handler := NewDoHHandler("https://fake-upstream.invalid", &http.Client{
		Transport: roundTripperFunc(func(req *http.Request) (*http.Response, error) {
			return &http.Response{
				StatusCode: http.StatusOK,
				Header:     http.Header{"Content-Type": []string{dnsMessageContentType}},
				Body:       &errReadCloser{err: fmt.Errorf("simulated read error")},
			}, nil
		}),
	})

	dnsQuery := buildDNSQuery("example.com", 1)
	req := httptest.NewRequest(http.MethodPost, "/dns-query", bytes.NewReader(dnsQuery))
	req.Header.Set("Content-Type", dnsMessageContentType)
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadGateway {
		t.Errorf("expected 502 for upstream response read error, got %d", rec.Code)
	}
}

// roundTripperFunc adapts a function to the http.RoundTripper interface.
type roundTripperFunc func(*http.Request) (*http.Response, error)

func (f roundTripperFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req)
}
