package captive

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestProbe_AppleSuccess(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(200)
		w.Write([]byte("<HTML><HEAD><TITLE>Success</TITLE></HEAD><BODY>Success</BODY></HTML>"))
	}))
	defer srv.Close()

	detail := Probe(context.Background(), []string{srv.URL + "/hotspot-detect.html?captive.apple.com"})
	// The URL doesn't contain "captive.apple.com" in the path but in query,
	// however our detection checks strings.Contains(url, "captive.apple.com").
	// Use a URL that matches.
	detail = probeOne(context.Background(), srv.URL+"/?captive.apple.com")
	// body contains "Success" and status 200 → NoPortal
	if detail.Result != NoPortal {
		t.Errorf("expected NoPortal, got %s (status=%d)", detail.Result, detail.StatusCode)
	}
}

func TestProbe_ApplePortalRedirect(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "http://portal.local/login", http.StatusFound)
	}))
	defer srv.Close()

	detail := probeOne(context.Background(), srv.URL+"/?captive.apple.com")
	if detail.Result != PortalDetected {
		t.Errorf("expected PortalDetected on 302, got %s", detail.Result)
	}
	if detail.StatusCode != 302 {
		t.Errorf("expected status 302, got %d", detail.StatusCode)
	}
}

func TestProbe_ApplePortalHTMLBody(t *testing.T) {
	// Some portals return 200 with login HTML instead of "Success".
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(200)
		w.Write([]byte("<html><body><form>Login to Wi-Fi</form></body></html>"))
	}))
	defer srv.Close()

	detail := probeOne(context.Background(), srv.URL+"/?captive.apple.com")
	if detail.Result != PortalDetected {
		t.Errorf("expected PortalDetected on 200 with wrong body, got %s", detail.Result)
	}
}

func TestProbe_Google204NoPortal(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(204)
	}))
	defer srv.Close()

	detail := probeOne(context.Background(), srv.URL+"/?generate_204")
	if detail.Result != NoPortal {
		t.Errorf("expected NoPortal on 204, got %s", detail.Result)
	}
}

func TestProbe_Google204PortalRedirect(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "http://portal.local/login", http.StatusTemporaryRedirect)
	}))
	defer srv.Close()

	detail := probeOne(context.Background(), srv.URL+"/?generate_204")
	if detail.Result != PortalDetected {
		t.Errorf("expected PortalDetected on 307, got %s", detail.Result)
	}
}

func TestProbe_Google204Portal200(t *testing.T) {
	// Some portals return 200 instead of 204 with login page.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(200)
		w.Write([]byte("<html>Portal Login</html>"))
	}))
	defer srv.Close()

	detail := probeOne(context.Background(), srv.URL+"/?generate_204")
	if detail.Result != PortalDetected {
		t.Errorf("expected PortalDetected on 200 (expected 204), got %s", detail.Result)
	}
}

func TestProbe_Timeout(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Block longer than DefaultTimeout.
		time.Sleep(5 * time.Second)
		w.WriteHeader(200)
	}))
	defer srv.Close()

	// Use a short context to speed up the test.
	ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
	defer cancel()

	detail := Probe(ctx, []string{srv.URL})
	if detail.Result != ProbeError {
		t.Errorf("expected ProbeError on timeout, got %s", detail.Result)
	}
	if detail.Err == nil {
		t.Error("expected non-nil error on timeout")
	}
}

func TestProbe_FallbackURL(t *testing.T) {
	// First URL times out, second succeeds → should return second URL's result.
	callCount := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callCount++
		if callCount == 1 {
			// Simulate timeout by hijacking and not responding.
			hj, ok := w.(http.Hijacker)
			if ok {
				conn, _, _ := hj.Hijack()
				conn.Close()
				return
			}
		}
		// Second call: success.
		w.WriteHeader(204)
	}))
	defer srv.Close()

	detail := Probe(context.Background(), []string{
		srv.URL + "/first",   // will fail (connection closed)
		srv.URL + "/?generate_204", // will succeed
	})
	if detail.Result != NoPortal {
		t.Errorf("expected NoPortal from fallback URL, got %s (err=%v)", detail.Result, detail.Err)
	}
}

func TestProbeResult_String(t *testing.T) {
	tests := []struct {
		r    ProbeResult
		want string
	}{
		{NoPortal, "no_portal"},
		{PortalDetected, "portal_detected"},
		{ProbeError, "probe_error"},
		{ProbeResult(99), "probe_error"},
	}
	for _, tt := range tests {
		if got := tt.r.String(); got != tt.want {
			t.Errorf("ProbeResult(%d).String() = %q, want %q", tt.r, got, tt.want)
		}
	}
}
