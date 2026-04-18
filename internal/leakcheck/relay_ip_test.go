package leakcheck

import (
	"context"
	"errors"
	"net"
	"net/netip"
	"sync/atomic"
	"testing"
	"time"
)

// stubDoH is a test double implementing DoHResolver.
type stubDoH struct {
	calls     atomic.Int64
	addr      netip.Addr
	err       error
	sequence  []netip.Addr // optional: per-call return values (overrides addr when non-nil)
	sequenceI atomic.Int64
}

func (s *stubDoH) Resolve(_ context.Context, _ string) (netip.Addr, error) {
	s.calls.Add(1)
	if len(s.sequence) > 0 {
		i := s.sequenceI.Add(1) - 1
		if int(i) < len(s.sequence) {
			return s.sequence[i], nil
		}
	}
	return s.addr, s.err
}

func TestNewRelayIPResolver_EmptyDomain(t *testing.T) {
	if _, err := NewRelayIPResolver("", nil); !errors.Is(err, ErrRelayDomainEmpty) {
		t.Fatalf("expected ErrRelayDomainEmpty, got %v", err)
	}
}

func TestRelayIPResolver_FreshLookup(t *testing.T) {
	doh := &stubDoH{addr: netip.MustParseAddr("198.51.100.7")}
	r, err := NewRelayIPResolver("relay.example.com", doh)
	if err != nil {
		t.Fatalf("NewRelayIPResolver: %v", err)
	}

	ip, err := r.ExpectedIP(context.Background())
	if err != nil {
		t.Fatalf("ExpectedIP: %v", err)
	}
	if got, want := ip.String(), "198.51.100.7"; got != want {
		t.Errorf("ip = %q, want %q", got, want)
	}
	if doh.calls.Load() != 1 {
		t.Errorf("doh calls = %d, want 1", doh.calls.Load())
	}
}

func TestRelayIPResolver_CacheHit(t *testing.T) {
	doh := &stubDoH{addr: netip.MustParseAddr("198.51.100.7")}
	r, err := NewRelayIPResolver("relay.example.com", doh)
	if err != nil {
		t.Fatalf("NewRelayIPResolver: %v", err)
	}

	if _, err := r.ExpectedIP(context.Background()); err != nil {
		t.Fatalf("first ExpectedIP: %v", err)
	}
	if _, err := r.ExpectedIP(context.Background()); err != nil {
		t.Fatalf("second ExpectedIP: %v", err)
	}

	if got := doh.calls.Load(); got != 1 {
		t.Errorf("doh calls = %d, want 1 (cache hit on second call)", got)
	}
}

func TestRelayIPResolver_CacheExpiry(t *testing.T) {
	doh := &stubDoH{
		sequence: []netip.Addr{
			netip.MustParseAddr("198.51.100.1"),
			netip.MustParseAddr("198.51.100.2"),
		},
	}
	r, err := NewRelayIPResolver("relay.example.com", doh)
	if err != nil {
		t.Fatalf("NewRelayIPResolver: %v", err)
	}
	r.WithTTL(1 * time.Minute)

	now := time.Unix(1_000_000, 0)
	r.WithNowFunc(func() time.Time { return now })

	ip, err := r.ExpectedIP(context.Background())
	if err != nil || ip.String() != "198.51.100.1" {
		t.Fatalf("first = %v, %v; want 198.51.100.1, nil", ip, err)
	}

	// Advance past TTL.
	now = now.Add(61 * time.Second)

	ip, err = r.ExpectedIP(context.Background())
	if err != nil || ip.String() != "198.51.100.2" {
		t.Fatalf("second (post-expiry) = %v, %v; want 198.51.100.2, nil", ip, err)
	}
	if got := doh.calls.Load(); got != 2 {
		t.Errorf("doh calls = %d, want 2", got)
	}
}

func TestRelayIPResolver_ResolverError_DoesNotPolluteCache(t *testing.T) {
	doh := &stubDoH{err: errors.New("upstream down")}
	r, err := NewRelayIPResolver("relay.example.com", doh)
	if err != nil {
		t.Fatalf("NewRelayIPResolver: %v", err)
	}

	if _, err := r.ExpectedIP(context.Background()); err == nil {
		t.Fatal("expected error on DoH failure, got nil")
	}

	// Second call must ALSO hit DoH (no poisoned cache).
	if _, err := r.ExpectedIP(context.Background()); err == nil {
		t.Fatal("expected error on second DoH failure, got nil")
	}
	if got := doh.calls.Load(); got != 2 {
		t.Errorf("doh calls = %d, want 2 (each failure re-queries)", got)
	}
}

func TestRelayIPResolver_EmptyResult(t *testing.T) {
	doh := &stubDoH{addr: netip.Addr{}} // invalid
	r, err := NewRelayIPResolver("relay.example.com", doh)
	if err != nil {
		t.Fatalf("NewRelayIPResolver: %v", err)
	}
	_, err = r.ExpectedIP(context.Background())
	if err == nil {
		t.Fatal("expected error on empty address, got nil")
	}
}

func TestRelayIPResolver_NilResolver(t *testing.T) {
	r, err := NewRelayIPResolver("relay.example.com", nil)
	if err != nil {
		t.Fatalf("NewRelayIPResolver: %v", err)
	}
	_, err = r.ExpectedIP(context.Background())
	if err == nil {
		t.Fatal("expected error when resolver is nil, got nil")
	}
}

func TestRelayIPResolver_Invalidate(t *testing.T) {
	doh := &stubDoH{
		sequence: []netip.Addr{
			netip.MustParseAddr("198.51.100.1"),
			netip.MustParseAddr("198.51.100.2"),
		},
	}
	r, err := NewRelayIPResolver("relay.example.com", doh)
	if err != nil {
		t.Fatalf("NewRelayIPResolver: %v", err)
	}

	if _, err := r.ExpectedIP(context.Background()); err != nil {
		t.Fatalf("first ExpectedIP: %v", err)
	}
	r.Invalidate()
	ip, err := r.ExpectedIP(context.Background())
	if err != nil || ip.String() != "198.51.100.2" {
		t.Fatalf("after Invalidate: got %v, %v; want 198.51.100.2, nil", ip, err)
	}
	if got := doh.calls.Load(); got != 2 {
		t.Errorf("doh calls = %d, want 2", got)
	}
}

func TestClassifyLeak_PrivateIP_TunDown(t *testing.T) {
	cases := []string{"192.168.1.5", "10.0.0.1", "172.16.5.5"}
	for _, s := range cases {
		if got := ClassifyLeak(net.ParseIP(s)); got != LeakReasonTUNDown {
			t.Errorf("%s classified as %q, want %q", s, got, LeakReasonTUNDown)
		}
	}
}

func TestClassifyLeak_PublicIP_DiffersFromRelay(t *testing.T) {
	cases := []string{"8.8.8.8", "203.0.113.42"}
	for _, s := range cases {
		if got := ClassifyLeak(net.ParseIP(s)); got != LeakReasonStunIPDiffers {
			t.Errorf("%s classified as %q, want %q", s, got, LeakReasonStunIPDiffers)
		}
	}
}

func TestClassifyLeak_Loopback(t *testing.T) {
	if got := ClassifyLeak(net.ParseIP("127.0.0.1")); got != LeakReasonTUNDown {
		t.Errorf("127.0.0.1 classified as %q, want %q", got, LeakReasonTUNDown)
	}
}

func TestClassifyLeak_IPv6Private(t *testing.T) {
	cases := []string{"fe80::1", "fc00::1"}
	for _, s := range cases {
		if got := ClassifyLeak(net.ParseIP(s)); got != LeakReasonTUNDown {
			t.Errorf("%s classified as %q, want %q", s, got, LeakReasonTUNDown)
		}
	}
}

func TestClassifyLeak_Nil(t *testing.T) {
	if got := ClassifyLeak(nil); got != LeakReasonStunIPDiffers {
		t.Errorf("nil classified as %q, want %q", got, LeakReasonStunIPDiffers)
	}
}
