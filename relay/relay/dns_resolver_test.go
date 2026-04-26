package relay

import (
	"bytes"
	"context"
	"log"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/miekg/dns"
)

// buildQuery creates a DNS wire-format A query for the given name.
func buildQuery(t *testing.T, name string) []byte {
	t.Helper()
	m := new(dns.Msg)
	m.SetQuestion(dns.Fqdn(name), dns.TypeA)
	m.RecursionDesired = true
	out, err := m.Pack()
	if err != nil {
		t.Fatalf("pack query: %v", err)
	}
	return out
}

// buildResponse creates a DNS wire-format response with the given rcode
// and AD flag. The response mirrors the question from req.
func buildResponse(t *testing.T, req []byte, rcode int, ad bool) []byte {
	t.Helper()
	qmsg := new(dns.Msg)
	if err := qmsg.Unpack(req); err != nil {
		t.Fatalf("unpack query: %v", err)
	}
	resp := new(dns.Msg)
	resp.SetRcode(qmsg, rcode)
	resp.AuthenticatedData = ad
	resp.RecursionAvailable = true
	if rcode == dns.RcodeSuccess && len(qmsg.Question) > 0 {
		resp.Answer = append(resp.Answer, &dns.A{
			Hdr: dns.RR_Header{
				Name:   qmsg.Question[0].Name,
				Rrtype: dns.TypeA,
				Class:  dns.ClassINET,
				Ttl:    300,
			},
			A: []byte{93, 184, 216, 34},
		})
	}
	out, err := resp.Pack()
	if err != nil {
		t.Fatalf("pack response: %v", err)
	}
	return out
}

// dohHandler returns an http.HandlerFunc that responds with DNS wire-format.
func dohHandler(t *testing.T, rcode int, ad bool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		body := make([]byte, 65535)
		n, _ := r.Body.Read(body)
		resp := buildResponse(t, body[:n], rcode, ad)
		w.Header().Set("Content-Type", "application/dns-message")
		w.Write(resp)
	}
}

func TestDNSResolver_NominalCloudflare(t *testing.T) {
	srv := httptest.NewServer(dohHandler(t, dns.RcodeSuccess, true))
	defer srv.Close()

	r := NewDNSResolver([]string{srv.URL}, nil, srv.Client())
	query := buildQuery(t, "example.com")
	resp, err := r.Resolve(context.Background(), query)
	if err != nil {
		t.Fatalf("Resolve() error: %v", err)
	}

	msg := new(dns.Msg)
	if err := msg.Unpack(resp); err != nil {
		t.Fatalf("unpack response: %v", err)
	}
	if msg.Rcode != dns.RcodeSuccess {
		t.Errorf("Rcode = %d, want %d", msg.Rcode, dns.RcodeSuccess)
	}

	m := r.Metrics()
	if m.QueriesTotal != 1 {
		t.Errorf("QueriesTotal = %d, want 1", m.QueriesTotal)
	}
	if m.UpstreamFailuresTotal != 0 {
		t.Errorf("UpstreamFailuresTotal = %d, want 0", m.UpstreamFailuresTotal)
	}
}

func TestDNSResolver_FailoverToQuad9_OnTimeout(t *testing.T) {
	// First server: delays beyond upstream timeout.
	slowSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(3 * time.Second) // exceeds dnsUpstreamTimeout (2s)
		w.WriteHeader(http.StatusGatewayTimeout)
	}))
	defer slowSrv.Close()

	// Second server: responds normally.
	fastSrv := httptest.NewServer(dohHandler(t, dns.RcodeSuccess, true))
	defer fastSrv.Close()

	r := NewDNSResolver([]string{slowSrv.URL, fastSrv.URL}, nil, &http.Client{Timeout: 10 * time.Second})
	query := buildQuery(t, "failover-test.com")
	resp, err := r.Resolve(context.Background(), query)
	if err != nil {
		t.Fatalf("Resolve() error: %v", err)
	}

	msg := new(dns.Msg)
	if err := msg.Unpack(resp); err != nil {
		t.Fatalf("unpack response: %v", err)
	}
	if msg.Rcode != dns.RcodeSuccess {
		t.Errorf("Rcode = %d, want %d (SUCCESS)", msg.Rcode, dns.RcodeSuccess)
	}

	m := r.Metrics()
	if m.UpstreamFailuresTotal < 1 {
		t.Errorf("UpstreamFailuresTotal = %d, want >= 1", m.UpstreamFailuresTotal)
	}
}

func TestDNSResolver_AD0_AcceptedButTracked(t *testing.T) {
	// AD=0 is normal for non-DNSSEC zones — must resolve successfully,
	// not SERVFAIL. The metric tracks it but doesn't block.
	noADSrv := httptest.NewServer(dohHandler(t, dns.RcodeSuccess, false))
	defer noADSrv.Close()

	r := NewDNSResolver([]string{noADSrv.URL}, nil, noADSrv.Client())
	query := buildQuery(t, "unsigned-zone.com")
	resp, err := r.Resolve(context.Background(), query)
	if err != nil {
		t.Fatalf("Resolve() error: %v", err)
	}

	msg := new(dns.Msg)
	if err := msg.Unpack(resp); err != nil {
		t.Fatalf("unpack response: %v", err)
	}
	if msg.Rcode != dns.RcodeSuccess {
		t.Errorf("Rcode = %d, want SUCCESS — AD=0 must NOT cause SERVFAIL", msg.Rcode)
	}

	m := r.Metrics()
	if m.DNSSECFailuresTotal != 1 {
		t.Errorf("DNSSECFailuresTotal = %d, want 1 (tracked but not blocking)", m.DNSSECFailuresTotal)
	}
}

func TestDNSResolver_SERVFAIL_BothFail(t *testing.T) {
	failSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer failSrv.Close()

	r := NewDNSResolver([]string{failSrv.URL, failSrv.URL}, nil, failSrv.Client())
	query := buildQuery(t, "both-fail.com")
	resp, err := r.Resolve(context.Background(), query)
	if err != nil {
		t.Fatalf("Resolve() error: %v", err)
	}

	msg := new(dns.Msg)
	if err := msg.Unpack(resp); err != nil {
		t.Fatalf("unpack response: %v", err)
	}
	if msg.Rcode != dns.RcodeServerFailure {
		t.Errorf("Rcode = %d, want %d (SERVFAIL)", msg.Rcode, dns.RcodeServerFailure)
	}
}

func TestDNSResolver_BlocklistNXDOMAIN(t *testing.T) {
	var called atomic.Bool
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called.Store(true)
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	bl := NewBlocklist("", nil, time.Hour)
	m := map[string]struct{}{"evil.com": {}}
	bl.entries.Store(&m)

	r := NewDNSResolver([]string{srv.URL}, bl, srv.Client())
	query := buildQuery(t, "evil.com")
	resp, err := r.Resolve(context.Background(), query)
	if err != nil {
		t.Fatalf("Resolve() error: %v", err)
	}

	msg := new(dns.Msg)
	if err := msg.Unpack(resp); err != nil {
		t.Fatalf("unpack response: %v", err)
	}
	if msg.Rcode != dns.RcodeNameError {
		t.Errorf("Rcode = %d, want %d (NXDOMAIN)", msg.Rcode, dns.RcodeNameError)
	}
	if called.Load() {
		t.Error("upstream was called for blocked domain — should not happen (AC4)")
	}

	metrics := r.Metrics()
	if metrics.BlockedTotal != 1 {
		t.Errorf("BlockedTotal = %d, want 1", metrics.BlockedTotal)
	}
}

func TestDNSResolver_BlocklistSubdomain(t *testing.T) {
	bl := NewBlocklist("", nil, time.Hour)
	m := map[string]struct{}{"evil.com": {}}
	bl.entries.Store(&m)

	failSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Error("upstream called for blocked subdomain")
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer failSrv.Close()

	r := NewDNSResolver([]string{failSrv.URL}, bl, failSrv.Client())
	query := buildQuery(t, "ads.evil.com")
	resp, err := r.Resolve(context.Background(), query)
	if err != nil {
		t.Fatalf("Resolve() error: %v", err)
	}

	msg := new(dns.Msg)
	if err := msg.Unpack(resp); err != nil {
		t.Fatalf("unpack: %v", err)
	}
	if msg.Rcode != dns.RcodeNameError {
		t.Errorf("Rcode = %d, want NXDOMAIN for subdomain of blocked domain", msg.Rcode)
	}
}

func TestDNSResolver_NoDomainInLogs(t *testing.T) {
	srv := httptest.NewServer(dohHandler(t, dns.RcodeSuccess, true))
	defer srv.Close()

	bl := NewBlocklist("", nil, time.Hour)
	m := map[string]struct{}{"blocked-secret.com": {}}
	bl.entries.Store(&m)

	var buf bytes.Buffer
	log.SetOutput(&buf)
	t.Cleanup(func() { log.SetOutput(os.Stderr) })

	r := NewDNSResolver([]string{srv.URL}, bl, srv.Client())

	domains := []string{"secret-query.com", "blocked-secret.com", "another-domain.org"}
	for _, d := range domains {
		r.Resolve(context.Background(), buildQuery(t, d))
	}

	logOutput := buf.String()
	for _, d := range domains {
		if strings.Contains(logOutput, d) {
			t.Errorf("log output contains domain %q — NFR22a violation", d)
		}
	}
}

func TestDNSResolver_InvalidQuery(t *testing.T) {
	srv := httptest.NewServer(dohHandler(t, dns.RcodeSuccess, true))
	defer srv.Close()

	r := NewDNSResolver([]string{srv.URL}, nil, srv.Client())

	// Garbage input.
	resp, err := r.Resolve(context.Background(), []byte{0xDE, 0xAD})
	if err != nil {
		t.Fatalf("expected nil error for garbage, got: %v", err)
	}

	msg := new(dns.Msg)
	if err := msg.Unpack(resp); err != nil {
		t.Fatalf("unpack SERVFAIL: %v", err)
	}
	if msg.Rcode != dns.RcodeServerFailure {
		t.Errorf("Rcode = %d, want SERVFAIL for garbage input", msg.Rcode)
	}
}
