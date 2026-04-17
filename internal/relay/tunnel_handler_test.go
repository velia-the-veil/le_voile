package relay

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	lecrypto "github.com/velia-the-veil/le_voile/internal/crypto"
)

// --- test helpers ---

// testEchoForwarder echoes every packet back through the outbound channel.
type testEchoForwarder struct {
	mu       sync.Mutex
	sessions map[string]chan<- []byte
}

func newTestEchoForwarder() *testEchoForwarder {
	return &testEchoForwarder{sessions: make(map[string]chan<- []byte)}
}

func (f *testEchoForwarder) OpenSession(_ context.Context, s TunnelSession) (<-chan []byte, func()) {
	ch := make(chan []byte, 64)
	f.mu.Lock()
	f.sessions[s.ClientIPHash] = ch
	f.mu.Unlock()
	return ch, func() {
		f.mu.Lock()
		delete(f.sessions, s.ClientIPHash)
		f.mu.Unlock()
	}
}

func (f *testEchoForwarder) Forward(_ context.Context, s TunnelSession, pkt []byte) error {
	f.mu.Lock()
	ch, ok := f.sessions[s.ClientIPHash]
	f.mu.Unlock()
	if !ok {
		return nil
	}
	cp := make([]byte, len(pkt))
	copy(cp, pkt)
	select {
	case ch <- cp:
	default:
	}
	return nil
}

// makeFrame builds a framed packet: [2-byte big-endian length][payload].
func makeFrame(payload []byte) []byte {
	frame := make([]byte, TunnelFrameHeaderSize+len(payload))
	binary.BigEndian.PutUint16(frame, uint16(len(payload)))
	copy(frame[TunnelFrameHeaderSize:], payload)
	return frame
}

// forgeExpiredToken creates a properly signed but expired session token.
func forgeExpiredToken(t *testing.T, privKey ed25519.PrivateKey, clientIP string) string {
	t.Helper()
	ipHash := fmt.Sprintf("%x", sha256.Sum256([]byte(clientIP)))
	payload := SessionTokenPayload{
		IPHash: ipHash,
		Issued: time.Now().Unix() - 20000,
		TTL:    100,
	}
	payloadJSON, _ := json.Marshal(payload)
	payloadB64 := base64.RawURLEncoding.EncodeToString(payloadJSON)
	sig, _ := lecrypto.Sign(privKey, payloadJSON)
	sigB64 := base64.RawURLEncoding.EncodeToString(sig)
	return payloadB64 + "." + sigB64
}

// safeLogBuf is a concurrency-safe log buffer for test logFunc.
type safeLogBuf struct {
	mu  sync.Mutex
	buf strings.Builder
}

func (s *safeLogBuf) Write(format string, args ...any) {
	s.mu.Lock()
	defer s.mu.Unlock()
	fmt.Fprintf(&s.buf, format+"\n", args...)
}

func (s *safeLogBuf) String() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.buf.String()
}

// tunnelTestEnv provides a ready-to-use tunnel test environment.
type tunnelTestEnv struct {
	handler   *TunnelHandler
	forwarder *testEchoForwarder
	pubKey    ed25519.PublicKey
	privKey   ed25519.PrivateKey
	clientIP  string
	token     string
	logBuf    *safeLogBuf
}

func newTunnelTestEnv(t *testing.T) *tunnelTestEnv {
	t.Helper()
	pub, priv, err := lecrypto.GenerateKeyPair()
	if err != nil {
		t.Fatalf("keygen: %v", err)
	}
	clientIP := "203.0.113.42"
	token, err := CreateSessionToken(priv, clientIP)
	if err != nil {
		t.Fatalf("create token: %v", err)
	}
	cfv := NewCloudflareIPValidator(true, nil) // insecure for tests
	ipLimiter := NewIPLimiter(IPLimiterMaxPerIP)
	fwd := newTestEchoForwarder()
	logBuf := &safeLogBuf{}
	handler := NewTunnelHandler(pub, cfv, ipLimiter, fwd, logBuf.Write)
	return &tunnelTestEnv{
		handler:   handler,
		forwarder: fwd,
		pubKey:    pub,
		privKey:   priv,
		clientIP:  clientIP,
		token:     token,
		logBuf:    logBuf,
	}
}

func (e *tunnelTestEnv) newRequest(body io.Reader) *http.Request {
	req := httptest.NewRequest(http.MethodPost, "/tunnel", body)
	req.Header.Set("Authorization", "Bearer "+e.token)
	req.Header.Set("Content-Type", "application/octet-stream")
	// Insecure mode extracts IP from RemoteAddr — must match token's IP.
	req.RemoteAddr = e.clientIP + ":12345"
	return req
}

// --- AC1: Happy Path ---

func TestTunnelHandler_HappyPath(t *testing.T) {
	env := newTunnelTestEnv(t)

	payload := bytes.Repeat([]byte{0xAB}, 100)
	body := makeFrame(payload)

	req := env.newRequest(bytes.NewReader(body))
	rec := httptest.NewRecorder()

	done := make(chan struct{})
	go func() {
		defer close(done)
		env.handler.ServeHTTP(rec, req)
	}()

	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("handler did not complete within timeout")
	}

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}

	respBytes := rec.Body.Bytes()
	if len(respBytes) < TunnelFrameHeaderSize {
		t.Fatalf("response too short: %d bytes", len(respBytes))
	}
	respLen := binary.BigEndian.Uint16(respBytes[:TunnelFrameHeaderSize])
	if int(respLen) != len(payload) {
		t.Fatalf("response frame length = %d, want %d", respLen, len(payload))
	}
	if !bytes.Equal(respBytes[TunnelFrameHeaderSize:TunnelFrameHeaderSize+respLen], payload) {
		t.Error("response payload does not match sent payload")
	}
}

// --- AC2: Framing Boundaries ---

func TestTunnelHandler_FramingBoundaries(t *testing.T) {
	tests := []struct {
		name      string
		frameLen  uint16
		wantClose bool
	}{
		{"min_valid_1", 1, false},
		{"max_valid_1420", TunnelMaxFrameSize, false},
		{"zero_closes", 0, true},
		{"over_max_1421", TunnelMaxFrameSize + 1, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			env := newTunnelTestEnv(t)

			var body bytes.Buffer
			hdr := make([]byte, TunnelFrameHeaderSize)
			binary.BigEndian.PutUint16(hdr, tt.frameLen)
			body.Write(hdr)
			if tt.frameLen > 0 {
				body.Write(bytes.Repeat([]byte{0x42}, int(tt.frameLen)))
			}

			req := env.newRequest(&body)
			rec := httptest.NewRecorder()

			done := make(chan struct{})
			go func() {
				defer close(done)
				env.handler.ServeHTTP(rec, req)
			}()

			select {
			case <-done:
			case <-time.After(5 * time.Second):
				t.Fatal("handler did not complete within timeout")
			}

			if rec.Code != http.StatusOK {
				t.Fatalf("status = %d, want 200 (stream opened before frame check)", rec.Code)
			}
		})
	}
}

func TestTunnelHandler_FramingTruncatedHeader(t *testing.T) {
	env := newTunnelTestEnv(t)

	req := env.newRequest(bytes.NewReader([]byte{0x00}))
	rec := httptest.NewRecorder()

	done := make(chan struct{})
	go func() {
		defer close(done)
		env.handler.ServeHTTP(rec, req)
	}()

	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("handler did not complete within timeout")
	}

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (stream opened before read error)", rec.Code)
	}
}

// --- AC3: Bidirectional roundtrip ---

func TestTunnelHandler_Bidirectional(t *testing.T) {
	env := newTunnelTestEnv(t)

	const numFrames = 10
	var body bytes.Buffer
	payloads := make([][]byte, numFrames)
	for i := range numFrames {
		p := bytes.Repeat([]byte{byte(i)}, 50+i*10)
		payloads[i] = p
		body.Write(makeFrame(p))
	}

	req := env.newRequest(&body)
	rec := httptest.NewRecorder()

	done := make(chan struct{})
	go func() {
		defer close(done)
		env.handler.ServeHTTP(rec, req)
	}()

	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("handler did not complete within timeout")
	}

	resp := rec.Body.Bytes()
	for i := 0; i < numFrames; i++ {
		if len(resp) < TunnelFrameHeaderSize {
			t.Fatalf("frame %d: response truncated (remaining %d bytes)", i, len(resp))
		}
		n := binary.BigEndian.Uint16(resp[:TunnelFrameHeaderSize])
		resp = resp[TunnelFrameHeaderSize:]
		if int(n) != len(payloads[i]) {
			t.Fatalf("frame %d: length = %d, want %d", i, n, len(payloads[i]))
		}
		if len(resp) < int(n) {
			t.Fatalf("frame %d: payload truncated", i)
		}
		if !bytes.Equal(resp[:n], payloads[i]) {
			t.Errorf("frame %d: payload mismatch", i)
		}
		resp = resp[n:]
	}
}

// --- AC4: Unauthorized cases ---

func TestTunnelHandler_Unauthorized(t *testing.T) {
	pub, priv, _ := lecrypto.GenerateKeyPair()
	clientIP := "203.0.113.42"
	cfv := NewCloudflareIPValidator(true, nil)
	fwd := newTestEchoForwarder()
	handler := NewTunnelHandler(pub, cfv, NewIPLimiter(IPLimiterMaxPerIP), fwd, nil)

	validToken, _ := CreateSessionToken(priv, clientIP)
	expiredToken := forgeExpiredToken(t, priv, clientIP)
	wrongIPToken, _ := CreateSessionToken(priv, "198.51.100.99")
	badSigToken := validToken[:len(validToken)-4] + "XXXX"

	tests := []struct {
		name  string
		token string
	}{
		{"no_bearer", ""},
		{"malformed_bearer", "not-a-valid-token-format"},
		{"invalid_signature", badSigToken},
		{"expired_token", expiredToken},
		{"ip_hash_mismatch", wrongIPToken},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodPost, "/tunnel", bytes.NewReader(makeFrame([]byte{0x01})))
			req.RemoteAddr = clientIP + ":12345"
			if tt.token != "" {
				req.Header.Set("Authorization", "Bearer "+tt.token)
			}
			rec := httptest.NewRecorder()

			handler.ServeHTTP(rec, req)

			if rec.Code != http.StatusUnauthorized {
				t.Errorf("status = %d, want 401 for case %q", rec.Code, tt.name)
			}
		})
	}
}

// --- AC5: Method Not Allowed ---

func TestTunnelHandler_MethodNotAllowed(t *testing.T) {
	env := newTunnelTestEnv(t)

	methods := []string{http.MethodGet, http.MethodPut, http.MethodDelete}
	for _, m := range methods {
		t.Run(m, func(t *testing.T) {
			req := httptest.NewRequest(m, "/tunnel", nil)
			rec := httptest.NewRecorder()

			env.handler.ServeHTTP(rec, req)

			if rec.Code != http.StatusMethodNotAllowed {
				t.Errorf("status = %d, want 405 for %s", rec.Code, m)
			}
			if allow := rec.Header().Get("Allow"); allow != "POST" {
				t.Errorf("Allow header = %q, want POST", allow)
			}
		})
	}
}

// --- AC6: Per-IP Limit ---

func TestTunnelHandler_PerIPLimit(t *testing.T) {
	pub, priv, _ := lecrypto.GenerateKeyPair()
	clientIP := "203.0.113.42"
	cfv := NewCloudflareIPValidator(true, nil)
	fwd := newTestEchoForwarder()
	ipLimiter := NewIPLimiter(1) // max 1 per IP
	handler := NewTunnelHandler(pub, cfv, ipLimiter, fwd, nil)

	token, _ := CreateSessionToken(priv, clientIP)

	// Occupy the single slot.
	ipLimiter.Acquire(clientIP)
	defer ipLimiter.Release(clientIP)

	req := httptest.NewRequest(http.MethodPost, "/tunnel", bytes.NewReader(makeFrame([]byte{0x01})))
	req.Header.Set("Authorization", "Bearer "+token)
	req.RemoteAddr = clientIP + ":12345"
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusTooManyRequests {
		t.Errorf("status = %d, want 429", rec.Code)
	}
}

// --- AC7: Context cancellation / goroutine cleanup ---

func TestTunnelHandler_ContextCancellation(t *testing.T) {
	env := newTunnelTestEnv(t)

	// Use a pipe so we control when the reader ends.
	pr, pw := io.Pipe()

	req := env.newRequest(pr)
	// Create a cancellable context.
	ctx, cancel := context.WithCancel(req.Context())
	req = req.WithContext(ctx)

	rec := httptest.NewRecorder()

	goroutinesBefore := runtime.NumGoroutine()

	done := make(chan struct{})
	go func() {
		defer close(done)
		env.handler.ServeHTTP(rec, req)
	}()

	// Give goroutines time to start.
	time.Sleep(50 * time.Millisecond)

	// Cancel the context.
	cancel()
	pw.Close()

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("handler did not terminate within 2s after cancel")
	}

	// Allow goroutines to wind down.
	time.Sleep(100 * time.Millisecond)
	goroutinesAfter := runtime.NumGoroutine()

	// Allow margin of ±2 for runtime goroutines.
	if goroutinesAfter > goroutinesBefore+2 {
		t.Errorf("goroutine leak: before=%d, after=%d", goroutinesBefore, goroutinesAfter)
	}
}

// --- AC4 + NFR20: No IP in logs ---

func TestTunnelHandler_NoIPInLogs(t *testing.T) {
	pub, priv, _ := lecrypto.GenerateKeyPair()
	clientIP := "203.0.113.42"
	cfv := NewCloudflareIPValidator(true, nil)
	fwd := newTestEchoForwarder()
	logBuf := &safeLogBuf{}
	handler := NewTunnelHandler(pub, cfv, NewIPLimiter(IPLimiterMaxPerIP), fwd, logBuf.Write)

	validToken, _ := CreateSessionToken(priv, clientIP)
	expiredToken := forgeExpiredToken(t, priv, clientIP)
	wrongIPToken, _ := CreateSessionToken(priv, "198.51.100.99")

	// Run various failure cases.
	failCases := []struct {
		name  string
		token string
	}{
		{"no_bearer", ""},
		{"expired", expiredToken},
		{"wrong_ip", wrongIPToken},
	}

	for _, tt := range failCases {
		req := httptest.NewRequest(http.MethodPost, "/tunnel", bytes.NewReader(makeFrame([]byte{0x01})))
		req.RemoteAddr = clientIP + ":12345"
		if tt.token != "" {
			req.Header.Set("Authorization", "Bearer "+tt.token)
		}
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
	}

	// Also run a frame-size-error case with valid auth.
	var badFrameBody bytes.Buffer
	hdr := make([]byte, TunnelFrameHeaderSize)
	binary.BigEndian.PutUint16(hdr, 0) // N=0 → close
	badFrameBody.Write(hdr)

	req := httptest.NewRequest(http.MethodPost, "/tunnel", &badFrameBody)
	req.Header.Set("Authorization", "Bearer "+validToken)
	req.RemoteAddr = clientIP + ":12345"
	rec := httptest.NewRecorder()

	done := make(chan struct{})
	go func() {
		defer close(done)
		handler.ServeHTTP(rec, req)
	}()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("timeout")
	}

	// Check log output for IP leaks.
	logs := logBuf.String()

	forbidden := []string{clientIP, "198.51.100.99", "CF-Connecting-IP"}
	for _, s := range forbidden {
		if strings.Contains(logs, s) {
			t.Errorf("log contains forbidden string %q: %s", s, logs)
		}
	}
	// Also check that no token value appears.
	if strings.Contains(logs, validToken) {
		t.Error("log contains bearer token value")
	}
}

// --- AC6 global: 503 via LimitMiddleware ---

func TestTunnelHandler_GlobalLimit503(t *testing.T) {
	env := newTunnelTestEnv(t)

	// Create a limiter with max=1 and saturate it.
	limiter := NewLimiter(1)
	limiter.Acquire()
	defer limiter.Release()

	wrapped := LimitMiddleware(limiter, env.handler)

	req := env.newRequest(bytes.NewReader(makeFrame([]byte{0x01})))
	rec := httptest.NewRecorder()

	wrapped.ServeHTTP(rec, req)

	if rec.Code != http.StatusServiceUnavailable {
		t.Errorf("status = %d, want 503 when global limiter saturated", rec.Code)
	}
}

// --- H2: cfValidator nil → 401 ---

func TestTunnelHandler_NilCFValidator(t *testing.T) {
	pub, priv, _ := lecrypto.GenerateKeyPair()
	clientIP := "203.0.113.42"
	token, _ := CreateSessionToken(priv, clientIP)
	fwd := newTestEchoForwarder()
	// cfValidator = nil → should reject
	handler := NewTunnelHandler(pub, nil, NewIPLimiter(IPLimiterMaxPerIP), fwd, nil)

	req := httptest.NewRequest(http.MethodPost, "/tunnel", bytes.NewReader(makeFrame([]byte{0x01})))
	req.Header.Set("Authorization", "Bearer "+token)
	req.RemoteAddr = clientIP + ":12345"
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Errorf("status = %d, want 401 when cfValidator is nil", rec.Code)
	}
}

// --- Constructor panics ---

func TestNewTunnelHandler_PanicsOnNilKey(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Error("expected panic for nil pubKey")
		}
	}()
	NewTunnelHandler(nil, nil, nil, newTestEchoForwarder(), nil)
}

func TestNewTunnelHandler_PanicsOnNilForwarder(t *testing.T) {
	pub, _, _ := lecrypto.GenerateKeyPair()
	defer func() {
		if r := recover(); r == nil {
			t.Error("expected panic for nil forwarder")
		}
	}()
	NewTunnelHandler(pub, nil, nil, nil, nil)
}
