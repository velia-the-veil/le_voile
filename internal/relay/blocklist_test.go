package relay

import (
	"bytes"
	"context"
	"log"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync"
	"testing"
	"time"
)

// hostsPayload is a representative StevenBlack/hosts excerpt.
const hostsPayload = `# StevenBlack/hosts — unified
# Title: StevenBlack Unified hosts
# Last updated: 2026-01-01

# loopback
127.0.0.1 localhost
127.0.0.1 localhost.localdomain

# ads
0.0.0.0 tracker.com
0.0.0.0 ads.example.net
127.0.0.1 malware.bad.org # inline comment
0.0.0.0 UPPERCASE.COM
0.0.0.0 trailing-dot.com.

# edge cases
# 0.0.0.0 commented-out.com
192.168.1.1 wrong-ip.com
0.0.0.0
just-a-word
`

func TestBlocklist_ParseStevenBlackFormat(t *testing.T) {
	m := parseHostsFile(strings.NewReader(hostsPayload))

	tests := []struct {
		domain string
		want   bool
	}{
		{"tracker.com", true},
		{"ads.example.net", true},
		{"malware.bad.org", true},    // 127.0.0.1 prefix accepted
		{"uppercase.com", true},      // lowercased
		{"trailing-dot.com", true},   // trailing dot stripped
		{"localhost", false},         // excluded
		{"localhost.localdomain", false},
		{"commented-out.com", false}, // commented line
		{"wrong-ip.com", false},      // non-0.0.0.0/127.0.0.1 ignored
		{"not-in-list.com", false},
	}
	for _, tt := range tests {
		_, present := m[tt.domain]
		if present != tt.want {
			t.Errorf("parseHostsFile[%q] present=%v, want %v", tt.domain, present, tt.want)
		}
	}
}

func TestBlocklist_IsBlocked_ExactAndSubdomain(t *testing.T) {
	b := NewBlocklist("", nil, time.Hour)
	m := map[string]struct{}{
		"tracker.com":    {},
		"specific.ad.co": {},
	}
	b.entries.Store(&m)

	tests := []struct {
		fqdn string
		want bool
	}{
		{"tracker.com", true},
		{"Tracker.COM", true},            // case insensitive
		{"tracker.com.", true},            // trailing dot
		{"ads.tracker.com", true},         // subdomain
		{"foo.bar.tracker.com", true},     // deep subdomain
		{"notrackercom", false},           // no match (no dot)
		{"xtracker.com", false},           // prefix but not subdomain
		{"specific.ad.co", true},
		{"deep.specific.ad.co", true},
		{"ad.co", false},                  // parent of blocked, not itself blocked
		{"unrelated.org", false},
	}
	for _, tt := range tests {
		if got := b.IsBlocked(tt.fqdn); got != tt.want {
			t.Errorf("IsBlocked(%q) = %v, want %v", tt.fqdn, got, tt.want)
		}
	}
}

func TestBlocklist_Load_Success(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("0.0.0.0 evil.com\n0.0.0.0 bad.org\n"))
	}))
	defer srv.Close()

	b := NewBlocklist(srv.URL, srv.Client(), time.Hour)
	if err := b.Load(context.Background()); err != nil {
		t.Fatalf("Load() error: %v", err)
	}
	if b.Len() != 2 {
		t.Errorf("Len() = %d, want 2", b.Len())
	}
	if !b.IsBlocked("evil.com") {
		t.Error("evil.com should be blocked")
	}
}

func TestBlocklist_LoadFailure_PreservesOldMap(t *testing.T) {
	calls := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		if calls == 1 {
			w.Write([]byte("0.0.0.0 first-load.com\n"))
			return
		}
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()

	b := NewBlocklist(srv.URL, srv.Client(), time.Hour)

	// First load succeeds.
	if err := b.Load(context.Background()); err != nil {
		t.Fatalf("first Load() error: %v", err)
	}
	if b.Len() != 1 {
		t.Fatalf("after first Load, Len() = %d, want 1", b.Len())
	}

	// Second load fails — old map preserved.
	err := b.Load(context.Background())
	if err == nil {
		t.Fatal("expected error on second Load()")
	}
	if b.Len() != 1 {
		t.Errorf("after failed Load, Len() = %d, want 1 (preserved)", b.Len())
	}
	if !b.IsBlocked("first-load.com") {
		t.Error("first-load.com should still be blocked after failed refresh")
	}
}

func TestBlocklist_AtomicSwap_NoRace(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("0.0.0.0 race-test.com\n"))
	}))
	defer srv.Close()

	b := NewBlocklist(srv.URL, srv.Client(), time.Hour)
	if err := b.Load(context.Background()); err != nil {
		t.Fatal(err)
	}

	var wg sync.WaitGroup
	ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
	defer cancel()

	// Concurrent readers.
	for range 4 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for ctx.Err() == nil {
				b.IsBlocked("race-test.com")
				b.IsBlocked("not-blocked.com")
			}
		}()
	}
	// Concurrent writers.
	for range 2 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for ctx.Err() == nil {
				b.Load(context.Background())
			}
		}()
	}

	wg.Wait()
}

func TestBlocklist_NoDomainInLogs(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("0.0.0.0 secret-domain.com\n0.0.0.0 private-site.org\n"))
	}))
	defer srv.Close()

	var buf bytes.Buffer
	log.SetOutput(&buf)
	t.Cleanup(func() { log.SetOutput(os.Stderr) })

	b := NewBlocklist(srv.URL, srv.Client(), time.Hour)
	_ = b.Load(context.Background())
	b.IsBlocked("secret-domain.com")
	b.IsBlocked("private-site.org")
	b.IsBlocked("another-query.io")

	domains := []string{"secret-domain.com", "private-site.org", "another-query.io"}
	logOutput := buf.String()
	for _, d := range domains {
		if strings.Contains(logOutput, d) {
			t.Errorf("log output contains domain %q — NFR22a violation", d)
		}
	}
}
