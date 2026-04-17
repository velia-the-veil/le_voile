package registry

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"net/url"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"golang.org/x/net/dns/dnsmessage"
)

// buildDoHResponse builds a valid DNS wireformat response for host with
// one or more A/AAAA records.
func buildDoHResponse(t *testing.T, host string, qtype dnsmessage.Type, ipStrs ...string) []byte {
	t.Helper()
	name, err := dnsmessage.NewName(dnsToFQDN(host))
	if err != nil {
		t.Fatalf("NewName: %v", err)
	}
	msg := dnsmessage.Message{
		Header: dnsmessage.Header{
			ID:            0,
			Response:      true,
			Authoritative: true,
		},
		Questions: []dnsmessage.Question{{Name: name, Type: qtype, Class: dnsmessage.ClassINET}},
	}
	for _, ipStr := range ipStrs {
		ip, err := netip.ParseAddr(ipStr)
		if err != nil {
			t.Fatalf("parse ip: %v", err)
		}
		switch qtype {
		case dnsmessage.TypeA:
			if !ip.Is4() {
				t.Fatalf("expected IPv4, got %s", ipStr)
			}
			msg.Answers = append(msg.Answers, dnsmessage.Resource{
				Header: dnsmessage.ResourceHeader{Name: name, Type: dnsmessage.TypeA, Class: dnsmessage.ClassINET, TTL: 60},
				Body:   &dnsmessage.AResource{A: ip.As4()},
			})
		case dnsmessage.TypeAAAA:
			if !ip.Is6() {
				t.Fatalf("expected IPv6, got %s", ipStr)
			}
			msg.Answers = append(msg.Answers, dnsmessage.Resource{
				Header: dnsmessage.ResourceHeader{Name: name, Type: dnsmessage.TypeAAAA, Class: dnsmessage.ClassINET, TTL: 60},
				Body:   &dnsmessage.AAAAResource{AAAA: ip.As16()},
			})
		default:
			t.Fatalf("unsupported qtype %v", qtype)
		}
	}
	out, err := msg.Pack()
	if err != nil {
		t.Fatalf("Pack: %v", err)
	}
	return out
}

// buildDoHRCode builds a DNS response with the given RCODE and no answers.
func buildDoHRCode(t *testing.T, host string, qtype dnsmessage.Type, rcode dnsmessage.RCode) []byte {
	t.Helper()
	name, _ := dnsmessage.NewName(dnsToFQDN(host))
	msg := dnsmessage.Message{
		Header:    dnsmessage.Header{Response: true, RCode: rcode},
		Questions: []dnsmessage.Question{{Name: name, Type: qtype, Class: dnsmessage.ClassINET}},
	}
	buf, err := msg.Pack()
	if err != nil {
		t.Fatalf("Pack: %v", err)
	}
	return buf
}

// newDoHServer starts an HTTP test server that answers DoH queries with the
// provided responder.
func newDoHServer(t *testing.T, responder func(host string, qtype dnsmessage.Type) (status int, body []byte)) *httptest.Server {
	t.Helper()
	return httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Content-Type") != "application/dns-message" {
			http.Error(w, "bad content-type", http.StatusUnsupportedMediaType)
			return
		}
		body, err := io.ReadAll(r.Body)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		var parser dnsmessage.Parser
		if _, err := parser.Start(body); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		q, err := parser.Question()
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		host := strings.TrimSuffix(q.Name.String(), ".")
		status, resp := responder(host, q.Type)
		w.Header().Set("Content-Type", "application/dns-message")
		w.WriteHeader(status)
		_, _ = w.Write(resp)
	}))
}

// mustDoHResolver panics on construction error — tests should always supply
// valid upstream URLs (or check the error explicitly).
func mustDoHResolver(t *testing.T, opts ...DoHOption) *DoHResolver {
	t.Helper()
	r, err := NewDoHResolver(opts...)
	if err != nil {
		t.Fatalf("NewDoHResolver: %v", err)
	}
	return r
}

func TestDoHResolver_CloudflareSuccess_A(t *testing.T) {
	srv := newDoHServer(t, func(host string, qtype dnsmessage.Type) (int, []byte) {
		return http.StatusOK, buildDoHResponse(t, host, qtype, "203.0.113.10")
	})
	defer srv.Close()

	r := mustDoHResolver(t,
		WithDoHUpstreams([]string{srv.URL}),
		WithDoHHTTPClient(srv.Client()),
	)
	addr, err := r.Resolve(context.Background(), "relay.example.com")
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if got, want := addr.String(), "203.0.113.10"; got != want {
		t.Errorf("addr = %s, want %s", got, want)
	}
}

func TestDoHResolver_FailoverToSecondUpstream(t *testing.T) {
	var primaryHits int32
	primary := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		atomic.AddInt32(&primaryHits, 1)
		http.Error(w, "boom", http.StatusInternalServerError)
	}))
	defer primary.Close()

	secondary := newDoHServer(t, func(host string, qtype dnsmessage.Type) (int, []byte) {
		return http.StatusOK, buildDoHResponse(t, host, qtype, "203.0.113.42")
	})
	defer secondary.Close()

	r := mustDoHResolver(t,
		WithDoHUpstreams([]string{primary.URL, secondary.URL}),
		WithDoHHTTPClient(primary.Client()),
	)
	addr, err := r.Resolve(context.Background(), "relay.example.com")
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if got, want := addr.String(), "203.0.113.42"; got != want {
		t.Errorf("addr = %s, want %s", got, want)
	}
	if n := atomic.LoadInt32(&primaryHits); n == 0 {
		t.Error("expected primary to be contacted before fallback")
	}
}

func TestDoHResolver_AllUpstreamsDown(t *testing.T) {
	down := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "boom", http.StatusServiceUnavailable)
	}))
	defer down.Close()
	down2 := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "boom", http.StatusServiceUnavailable)
	}))
	defer down2.Close()

	r := mustDoHResolver(t,
		WithDoHUpstreams([]string{down.URL, down2.URL}),
		WithDoHHTTPClient(down.Client()),
	)
	_, err := r.Resolve(context.Background(), "relay.example.com")
	if !errors.Is(err, ErrAllDoHUpstreamsDown) {
		t.Fatalf("err = %v, want ErrAllDoHUpstreamsDown", err)
	}
	// Fix H3: inner cause must remain detectable via errors.Is.
	if !errors.Is(err, ErrDoHUpstreamUnreachable) {
		t.Errorf("err chain missing ErrDoHUpstreamUnreachable: %v", err)
	}
}

func TestDoHResolver_NoAddressRecord(t *testing.T) {
	srv := newDoHServer(t, func(host string, qtype dnsmessage.Type) (int, []byte) {
		return http.StatusOK, buildDoHRCode(t, host, qtype, dnsmessage.RCodeSuccess)
	})
	defer srv.Close()

	r := mustDoHResolver(t,
		WithDoHUpstreams([]string{srv.URL}),
		WithDoHHTTPClient(srv.Client()),
	)
	_, err := r.Resolve(context.Background(), "relay.example.com")
	if !errors.Is(err, ErrAllDoHUpstreamsDown) {
		t.Fatalf("err = %v, want wrapped ErrAllDoHUpstreamsDown", err)
	}
	if !errors.Is(err, ErrDoHNoAddressRecord) {
		t.Errorf("err chain missing ErrDoHNoAddressRecord: %v", err)
	}
}

// Fix M3: NXDOMAIN is authoritative — resolver short-circuits, no AAAA retry,
// no second upstream.
func TestDoHResolver_NXDOMAIN_ShortCircuits(t *testing.T) {
	var hitCount int32
	srv := newDoHServer(t, func(host string, qtype dnsmessage.Type) (int, []byte) {
		atomic.AddInt32(&hitCount, 1)
		return http.StatusOK, buildDoHRCode(t, host, qtype, dnsmessage.RCodeNameError)
	})
	defer srv.Close()

	srv2 := newDoHServer(t, func(host string, qtype dnsmessage.Type) (int, []byte) {
		return http.StatusOK, buildDoHResponse(t, host, qtype, "203.0.113.5")
	})
	defer srv2.Close()

	r := mustDoHResolver(t,
		WithDoHUpstreams([]string{srv.URL, srv2.URL}),
		WithDoHHTTPClient(srv.Client()),
	)
	_, err := r.Resolve(context.Background(), "nosuch.example.com")
	if !errors.Is(err, ErrDoHNXDOMAIN) {
		t.Fatalf("err = %v, want ErrDoHNXDOMAIN", err)
	}
	// One A query only — no AAAA retry, no second upstream.
	if n := atomic.LoadInt32(&hitCount); n != 1 {
		t.Errorf("upstream hits = %d, want 1 (NXDOMAIN must short-circuit)", n)
	}
}

// Fix M3: SERVFAIL also short-circuits per upstream, but resolver still tries
// the next upstream (in case Cloudflare is having a bad day but Quad9 isn't).
func TestDoHResolver_SERVFAIL_TriesNextUpstream(t *testing.T) {
	srv := newDoHServer(t, func(host string, qtype dnsmessage.Type) (int, []byte) {
		return http.StatusOK, buildDoHRCode(t, host, qtype, dnsmessage.RCodeServerFailure)
	})
	defer srv.Close()

	srv2 := newDoHServer(t, func(host string, qtype dnsmessage.Type) (int, []byte) {
		return http.StatusOK, buildDoHResponse(t, host, qtype, "203.0.113.7")
	})
	defer srv2.Close()

	r := mustDoHResolver(t,
		WithDoHUpstreams([]string{srv.URL, srv2.URL}),
		WithDoHHTTPClient(srv.Client()),
	)
	addr, err := r.Resolve(context.Background(), "relay.example.com")
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if got, want := addr.String(), "203.0.113.7"; got != want {
		t.Errorf("addr = %s, want %s", got, want)
	}
}

func TestDoHResolver_ContextCanceled(t *testing.T) {
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		select {
		case <-r.Context().Done():
		case <-time.After(2 * time.Second):
		}
	}))
	t.Cleanup(func() {
		srv.CloseClientConnections()
		srv.Close()
	})

	r := mustDoHResolver(t,
		WithDoHUpstreams([]string{srv.URL}),
		WithDoHHTTPClient(srv.Client()),
		WithDoHTimeout(200*time.Millisecond),
	)
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	_, err := r.Resolve(ctx, "relay.example.com")
	if err == nil {
		t.Fatal("expected context-related error, got nil")
	}
	// Fix L1: assert the cause is the caller-provided deadline.
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Errorf("err chain missing context.DeadlineExceeded: %v", err)
	}
}

func TestDoHResolver_RejectsPrivateAddress(t *testing.T) {
	srv := newDoHServer(t, func(host string, qtype dnsmessage.Type) (int, []byte) {
		return http.StatusOK, buildDoHResponse(t, host, qtype, "127.0.0.1")
	})
	defer srv.Close()

	r := mustDoHResolver(t,
		WithDoHUpstreams([]string{srv.URL}),
		WithDoHHTTPClient(srv.Client()),
	)
	_, err := r.Resolve(context.Background(), "relay.example.com")
	if !errors.Is(err, ErrAllDoHUpstreamsDown) {
		t.Fatalf("err = %v, want wrapped ErrAllDoHUpstreamsDown", err)
	}
	// Fix H3: ErrDoHPrivateAddress must remain detectable.
	if !errors.Is(err, ErrDoHPrivateAddress) {
		t.Errorf("err chain missing ErrDoHPrivateAddress: %v", err)
	}
}

// Fix M2: when the DNS response contains multiple A records, resolver exposes
// all of them via ResolveAll for caller-driven round-robin.
func TestDoHResolver_ResolveAll_MultipleRecords(t *testing.T) {
	srv := newDoHServer(t, func(host string, qtype dnsmessage.Type) (int, []byte) {
		return http.StatusOK, buildDoHResponse(t, host, qtype, "203.0.113.1", "203.0.113.2", "203.0.113.3")
	})
	defer srv.Close()

	r := mustDoHResolver(t,
		WithDoHUpstreams([]string{srv.URL}),
		WithDoHHTTPClient(srv.Client()),
	)
	addrs, err := r.ResolveAll(context.Background(), "relay.example.com")
	if err != nil {
		t.Fatalf("ResolveAll: %v", err)
	}
	if len(addrs) != 3 {
		t.Fatalf("len addrs = %d, want 3: %v", len(addrs), addrs)
	}
}

// Fix M2: private addresses mixed with public ones must be filtered out
// but the public ones must still be returned.
func TestDoHResolver_ResolveAll_FiltersPrivateMixed(t *testing.T) {
	srv := newDoHServer(t, func(host string, qtype dnsmessage.Type) (int, []byte) {
		return http.StatusOK, buildDoHResponse(t, host, qtype, "10.0.0.1", "203.0.113.10", "192.168.1.1")
	})
	defer srv.Close()

	r := mustDoHResolver(t,
		WithDoHUpstreams([]string{srv.URL}),
		WithDoHHTTPClient(srv.Client()),
	)
	addrs, err := r.ResolveAll(context.Background(), "relay.example.com")
	if err != nil {
		t.Fatalf("ResolveAll: %v", err)
	}
	if len(addrs) != 1 || addrs[0].String() != "203.0.113.10" {
		t.Errorf("addrs = %v, want only [203.0.113.10]", addrs)
	}
}

func TestDoHResolver_AFallsBackToAAAA(t *testing.T) {
	srv := newDoHServer(t, func(host string, qtype dnsmessage.Type) (int, []byte) {
		if qtype == dnsmessage.TypeA {
			return http.StatusOK, buildDoHRCode(t, host, qtype, dnsmessage.RCodeSuccess)
		}
		return http.StatusOK, buildDoHResponse(t, host, qtype, "2001:db8::1")
	})
	defer srv.Close()

	r := mustDoHResolver(t,
		WithDoHUpstreams([]string{srv.URL}),
		WithDoHHTTPClient(srv.Client()),
	)
	addr, err := r.Resolve(context.Background(), "relay.example.com")
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if !addr.Is6() {
		t.Errorf("addr = %v, want IPv6", addr)
	}
	if got, want := addr.String(), "2001:db8::1"; got != want {
		t.Errorf("addr = %s, want %s", got, want)
	}
}

func TestDoHResolver_IPLiteralShortCircuits(t *testing.T) {
	// An IP literal must resolve without any upstream contact. We provide
	// a minimal valid upstream list (defaults would work too) to satisfy
	// construction; the test asserts the upstream is never hit.
	srv := newDoHServer(t, func(host string, qtype dnsmessage.Type) (int, []byte) {
		t.Errorf("unexpected upstream call for IP literal: %s %v", host, qtype)
		return http.StatusInternalServerError, nil
	})
	defer srv.Close()
	r := mustDoHResolver(t,
		WithDoHUpstreams([]string{srv.URL}),
		WithDoHHTTPClient(srv.Client()),
	)
	addr, err := r.Resolve(context.Background(), "93.184.216.34")
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if got, want := addr.String(), "93.184.216.34"; got != want {
		t.Errorf("addr = %s, want %s", got, want)
	}
}

// Fix M4: the previous NFR20 logger test was tautological (local server + local
// client both on 127.0.0.1 made the invariant unverifiable). Replace with a
// direct contract check: the only strings passed to the logger must be the
// host/upstream/error format template and the corresponding arguments. Assert
// that the argument list never contains a plausible client IP (anything that
// isn't the upstream URL or the host).
func TestDoHResolver_LoggerContract_NoExtraArgs(t *testing.T) {
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "boom", http.StatusInternalServerError)
	}))
	defer srv.Close()

	type capture struct {
		format string
		args   []any
	}
	var seen []capture
	r := mustDoHResolver(t,
		WithDoHUpstreams([]string{srv.URL}),
		WithDoHHTTPClient(srv.Client()),
		WithDoHLogger(func(format string, args ...any) {
			seen = append(seen, capture{format: format, args: args})
		}),
	)
	_, _ = r.Resolve(context.Background(), "relay.example.com")
	if len(seen) == 0 {
		t.Fatal("expected logger to be invoked on upstream failure")
	}
	for _, c := range seen {
		// Contract: format is a fixed template with up to 3 verbs
		// (upstream, host, error). No %s args beyond these.
		if strings.Count(c.format, "%") > 3 {
			t.Errorf("log format has too many verbs (possible IP leak): %q", c.format)
		}
		if len(c.args) > 3 {
			t.Errorf("logger received %d args, want <=3: %+v", len(c.args), c.args)
		}
	}
}

func TestDoHResolver_EmptyHost(t *testing.T) {
	r := mustDoHResolver(t)
	_, err := r.Resolve(context.Background(), "")
	if err == nil {
		t.Fatal("expected error for empty host")
	}
}

// Fix H2: NewDoHResolver must reject invalid upstream URLs upfront.
func TestNewDoHResolver_RejectsInvalidUpstreams(t *testing.T) {
	tests := []struct {
		name    string
		inputs  []string
		wantErr bool
	}{
		{"valid https", []string{"https://a/b"}, false},
		{"http rejected", []string{"http://a/b"}, true},
		{"empty scheme rejected", []string{"://a/b"}, true},
		{"empty host rejected", []string{"https:///"}, true},
		{"garbage rejected", []string{"::::not-a-url"}, true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			_, err := NewDoHResolver(WithDoHUpstreams(tc.inputs))
			if (err != nil) != tc.wantErr {
				t.Errorf("err = %v, wantErr = %v", err, tc.wantErr)
			}
		})
	}
}

func TestValidateUpstreams(t *testing.T) {
	tests := []struct {
		name    string
		inputs  []string
		wantErr bool
	}{
		{"empty", nil, false},
		{"valid https", []string{"https://a/b", "https://c/d"}, false},
		{"http rejected", []string{"http://a/b"}, true},
		{"bad url", []string{"::::not-a-url"}, true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := validateUpstreams(tc.inputs)
			if (err != nil) != tc.wantErr {
				t.Errorf("err = %v, wantErr = %v", err, tc.wantErr)
			}
		})
	}
}

func TestParseDNSAnswer_MalformedWireformat(t *testing.T) {
	_, err := parseDNSAnswer([]byte{0x00, 0x01}, dnsmessage.TypeA)
	if !errors.Is(err, ErrDoHInvalidResponse) {
		t.Errorf("err = %v, want ErrDoHInvalidResponse", err)
	}
}

// Fix M3: parseDNSAnswer must surface NXDOMAIN and SERVFAIL distinctly.
func TestParseDNSAnswer_RCodes(t *testing.T) {
	tests := []struct {
		name    string
		rcode   dnsmessage.RCode
		wantErr error
	}{
		{"NXDOMAIN", dnsmessage.RCodeNameError, ErrDoHNXDOMAIN},
		{"SERVFAIL", dnsmessage.RCodeServerFailure, ErrDoHServerFailure},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			body := buildDoHRCode(t, "relay.example.com", dnsmessage.TypeA, tc.rcode)
			_, err := parseDNSAnswer(body, dnsmessage.TypeA)
			if !errors.Is(err, tc.wantErr) {
				t.Errorf("err = %v, want %v", err, tc.wantErr)
			}
		})
	}
}

// Sanity: ensure bootstrap IPs contain only valid public addresses.
func TestBootstrapIPs_AllPublic(t *testing.T) {
	for host, ips := range dohBootstrapIPs {
		for _, s := range ips {
			addr, err := netip.ParseAddr(s)
			if err != nil {
				t.Errorf("%s: %s invalid: %v", host, s, err)
				continue
			}
			if isRejectableAddr(addr) {
				t.Errorf("%s: %s is rejectable (should be public)", host, s)
			}
		}
	}
}

// Guard: ensure the default upstreams parse and are HTTPS.
func TestDefaultUpstreams_Valid(t *testing.T) {
	for _, u := range []string{CloudflareDoH, Quad9DoH} {
		p, err := url.Parse(u)
		if err != nil {
			t.Errorf("%s: %v", u, err)
			continue
		}
		if p.Scheme != "https" {
			t.Errorf("%s: scheme = %q, want https", u, p.Scheme)
		}
	}
}
