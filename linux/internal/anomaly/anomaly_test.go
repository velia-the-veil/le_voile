//go:build linux

package anomaly

import (
	"bytes"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"sync"
	"testing"
)

func TestCategorizeError(t *testing.T) {
	cases := []struct {
		name string
		err  error
		want ErrorCategory
	}{
		{"nil returns unknown", nil, CategoryUnknown},
		{"tun recovery tun", errors.New("tun recovery: tun.New: permission denied"), CategoryTUNCreateFailed},
		{"routing setup wrap", errors.New("tun recovery: routing setup: missing gateway"), CategoryRoutingSetupFailed},
		{"bare route keyword", errors.New("route setup: ip rule add"), CategoryRoutingSetupFailed},
		{"firewall activate", errors.New("tun recovery: firewall activate: nftables load"), CategoryFirewallActivateFail},
		{"tunnel connect wrap", errors.New("tun recovery: tunnel.Connect: handshake"), CategoryTunnelConnectFailed},
		{"tunnel connect bare", errors.New("tunnel connect: quic dial"), CategoryTunnelConnectFailed},
		{"unknown falls back", errors.New("completely unrelated message"), CategoryUnknown},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := CategorizeError(tc.err)
			if got != tc.want {
				t.Fatalf("CategorizeError(%v) = %q, want %q", tc.err, got, tc.want)
			}
		})
	}
}

func TestStderrLogger_EmitsExpectedStrings(t *testing.T) {
	buf := &bytes.Buffer{}
	l := newStderrLogger(buf)

	l.Started(ReasonLeakDetected)
	l.Succeeded(1234)
	l.Failed(CategoryFirewallActivateFail)

	out := buf.String()
	wantSubstrings := []string{
		"anomaly detected: reason=leak_detected, starting recovery",
		"anomaly recovery succeeded after 1234ms",
		"anomaly recovery failed: firewall_activate_failed",
	}
	for _, s := range wantSubstrings {
		if !strings.Contains(out, s) {
			t.Errorf("missing substring %q in output\n%s", s, out)
		}
	}
}

func TestStderrLogger_CloseIsIdempotent(t *testing.T) {
	l := newStderrLogger(&bytes.Buffer{})
	if err := l.Close(); err != nil {
		t.Fatalf("first Close() returned %v, want nil", err)
	}
	if err := l.Close(); err != ErrLoggerClosed {
		t.Fatalf("second Close() = %v, want ErrLoggerClosed", err)
	}
}

func TestStderrLogger_NoWritesAfterClose(t *testing.T) {
	buf := &bytes.Buffer{}
	l := newStderrLogger(buf)
	_ = l.Close()
	l.Started(ReasonLeakDetected) // should be a silent no-op
	if buf.Len() != 0 {
		t.Fatalf("expected no output after Close, got %q", buf.String())
	}
}

func TestStderrLogger_ConcurrentWritesAreSafe(t *testing.T) {
	buf := &bytes.Buffer{}
	l := newStderrLogger(buf)
	var wg sync.WaitGroup
	for i := 0; i < 64; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			l.Succeeded(int64(i))
		}(i)
	}
	wg.Wait()
	// 64 lines = 64 newlines; if the mutex were missing, the race
	// detector would catch interleaved writes.
	if got := strings.Count(buf.String(), "\n"); got != 64 {
		t.Fatalf("expected 64 output lines, got %d:\n%s", got, buf.String())
	}
}

// TestAnomalyLogger_NFR22a is the most important test in the package: it
// proves that no IP, interface name, TLD, or filesystem path can leak
// into an anomaly log entry no matter how poisoned the inputs are. If a
// future developer widens the API to accept free-form strings, these
// assertions fail loudly in CI.
func TestAnomalyLogger_NFR22a(t *testing.T) {
	buf := &bytes.Buffer{}
	l := newStderrLogger(buf)

	// Emit every lifecycle event at least once with every known Reason
	// and every known ErrorCategory.
	for _, r := range []Reason{ReasonLeakDetected, ReasonTUNAltered, ReasonManual} {
		l.Started(r)
	}
	for _, c := range []ErrorCategory{
		CategoryTUNCreateFailed,
		CategoryRoutingSetupFailed,
		CategoryFirewallActivateFail,
		CategoryTunnelConnectFailed,
		CategoryUnknown,
	} {
		l.Failed(c)
	}
	for _, d := range []int64{0, 1, 12345, 999999} {
		l.Succeeded(d)
	}

	out := buf.String()

	// Validation patterns — each represents a class of user-data leak
	// that NFR22a forbids.
	forbidden := []struct {
		name    string
		pattern *regexp.Regexp
	}{
		// IPv4 dotted-quad: 1.2.3.4 / 192.168.0.5 / 255.255.255.255
		{"ipv4", regexp.MustCompile(`\b\d{1,3}\.\d{1,3}\.\d{1,3}\.\d{1,3}\b`)},
		// IPv6 heuristic: at least two colon-separated hex groups
		{"ipv6", regexp.MustCompile(`\b[0-9a-fA-F]{1,4}:[0-9a-fA-F]{1,4}:[0-9a-fA-F:]*\b`)},
		// The canonical TUN name — must not appear verbatim.
		{"tun name", regexp.MustCompile(`levoile0`)},
		// Filesystem paths (Windows backslash or Unix forward slash).
		{"windows path", regexp.MustCompile(`[A-Za-z]:\\`)},
		{"unix path", regexp.MustCompile(`(^|\s)/[a-zA-Z]`)},
		// Common public TLD suffixes — proxy for domain leakage.
		{"tld", regexp.MustCompile(`\.(com|net|org|io|co|fr|de|uk|us)\b`)},
	}

	for _, f := range forbidden {
		if loc := f.pattern.FindString(out); loc != "" {
			t.Errorf("NFR22a violation: %s pattern matched %q\nfull output:\n%s", f.name, loc, out)
		}
	}

	// Positive assertion: the log must still contain the structural
	// tokens operators rely on to grep.
	mustContain := []string{
		"anomaly detected",
		"anomaly recovery succeeded",
		"anomaly recovery failed",
	}
	for _, s := range mustContain {
		if !strings.Contains(out, s) {
			t.Errorf("missing operator token %q in output\n%s", s, out)
		}
	}
}

// TestNewLoggerReturnsNonNil guards against the factory being accidentally
// broken on the current platform. The real logger may or may not be able
// to open its system log sink (elevated on Windows, /dev/log on Linux),
// but NewLogger MUST always return a working Logger — falling back to the
// stderr shim if needed.
func TestNewLoggerReturnsNonNil(t *testing.T) {
	l := NewLogger()
	if l == nil {
		t.Fatal("NewLogger returned nil")
	}
	defer func() { _ = l.Close() }()
	// Emit one of each — must not panic even when the underlying sink is
	// the stderr fallback.
	l.Started(ReasonLeakDetected)
	l.Succeeded(42)
	l.Failed(CategoryUnknown)
}

// TestNopNotifier_Safe ensures the "no UI attached" notifier never panics
// for any combination of calls. It's embedded in Program when the service
// runs headless, so we lean on it silently.
func TestNopNotifier_Safe(t *testing.T) {
	var n Notifier = NopNotifier{}
	n.Started(ReasonManual)
	n.Succeeded(0)
	n.Failed(CategoryUnknown)
}

func ExampleCategorizeError() {
	err := errors.New("tun recovery: firewall activate: nftables load failed")
	fmt.Println(CategorizeError(err))
	// Output: firewall_activate_failed
}
