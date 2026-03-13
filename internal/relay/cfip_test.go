package relay

import (
	"net/http"
	"net/http/httptest"
	"net/netip"
	"testing"
)

func TestIsTrustedSource_KnownCFIP(t *testing.T) {
	v := NewCloudflareIPValidator(false, nil)
	// 104.16.1.1 falls within 104.16.0.0/13
	if !v.IsTrustedSource("104.16.1.1:1234") {
		t.Error("expected 104.16.1.1:1234 to be trusted (within 104.16.0.0/13)")
	}
}

func TestIsTrustedSource_NonCFIP(t *testing.T) {
	v := NewCloudflareIPValidator(false, nil)
	if v.IsTrustedSource("8.8.8.8:1234") {
		t.Error("expected 8.8.8.8:1234 to be untrusted")
	}
}

func TestIsTrustedSource_InvalidAddress(t *testing.T) {
	v := NewCloudflareIPValidator(false, nil)
	if v.IsTrustedSource("not-an-ip") {
		t.Error("expected invalid address to be untrusted")
	}
}

func TestIsTrustedSource_InsecureAlwaysTrue(t *testing.T) {
	v := NewCloudflareIPValidator(true, nil)
	tests := []string{
		"8.8.8.8:1234",
		"192.168.1.1:80",
		"not-an-ip",
		"[::1]:443",
	}
	for _, addr := range tests {
		if !v.IsTrustedSource(addr) {
			t.Errorf("insecure mode: expected %q to be trusted", addr)
		}
	}
}

func TestIsTrustedSource_Table(t *testing.T) {
	v := NewCloudflareIPValidator(false, nil)
	tests := []struct {
		name    string
		addr    string
		trusted bool
	}{
		{"CF IPv4 173.245.48.x", "173.245.48.1:443", true},
		{"CF IPv4 103.21.244.x", "103.21.244.10:80", true},
		{"CF IPv4 141.101.64.x", "141.101.64.1:8080", true},
		{"CF IPv4 162.158.0.x", "162.158.0.1:443", true},
		{"CF IPv4 172.64.0.x", "172.64.0.1:443", true},
		{"CF IPv4 131.0.72.x", "131.0.72.1:443", true},
		{"CF IPv6 2400:cb00::", "[2400:cb00::1]:443", true},
		{"CF IPv6 2606:4700::", "[2606:4700::1]:443", true},
		{"CF IPv6 2a06:98c0::", "[2a06:98c0::1]:443", true},
		{"non-CF 10.0.0.1", "10.0.0.1:80", false},
		{"non-CF 127.0.0.1", "127.0.0.1:80", false},
		{"non-CF 1.1.1.1", "1.1.1.1:80", false},
		{"bare IP no port", "104.16.1.1", true},
		{"empty string", "", false},
		{"garbage", "xyz:abc:def", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := v.IsTrustedSource(tt.addr)
			if got != tt.trusted {
				t.Errorf("IsTrustedSource(%q) = %v, want %v", tt.addr, got, tt.trusted)
			}
		})
	}
}

func TestExtractClientIP_TrustedWithCFHeader(t *testing.T) {
	v := NewCloudflareIPValidator(false, nil)
	r := httptest.NewRequest(http.MethodGet, "/", nil)
	r.RemoteAddr = "104.16.1.1:1234"
	r.Header.Set("CF-Connecting-IP", "203.0.113.50")

	ip, err := v.ExtractClientIP(r)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if ip != "203.0.113.50" {
		t.Errorf("got %q, want %q", ip, "203.0.113.50")
	}
}

func TestExtractClientIP_TrustedMissingHeader(t *testing.T) {
	// Trusted source but no CF-Connecting-IP header; strict mode should error.
	v := NewCloudflareIPValidator(false, nil)
	r := httptest.NewRequest(http.MethodGet, "/", nil)
	r.RemoteAddr = "104.16.1.1:1234"
	// No CF-Connecting-IP header set.

	_, err := v.ExtractClientIP(r)
	if err == nil {
		t.Fatal("expected error for trusted source without CF-Connecting-IP in strict mode")
	}
}

func TestExtractClientIP_UntrustedInsecureMode(t *testing.T) {
	v := NewCloudflareIPValidator(true, nil)
	r := httptest.NewRequest(http.MethodGet, "/", nil)
	r.RemoteAddr = "8.8.8.8:5678"
	// No CF-Connecting-IP header.

	ip, err := v.ExtractClientIP(r)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if ip != "8.8.8.8" {
		t.Errorf("got %q, want %q", ip, "8.8.8.8")
	}
}

func TestExtractClientIP_UntrustedStrictMode(t *testing.T) {
	v := NewCloudflareIPValidator(false, nil)
	r := httptest.NewRequest(http.MethodGet, "/", nil)
	r.RemoteAddr = "8.8.8.8:5678"

	_, err := v.ExtractClientIP(r)
	if err == nil {
		t.Fatal("expected error for untrusted source in strict mode")
	}
}

func TestExtractClientIP_InvalidCFConnectingIP(t *testing.T) {
	v := NewCloudflareIPValidator(false, nil)
	r := httptest.NewRequest(http.MethodGet, "/", nil)
	r.RemoteAddr = "104.16.1.1:1234"
	r.Header.Set("CF-Connecting-IP", "not-a-valid-ip")

	_, err := v.ExtractClientIP(r)
	if err == nil {
		t.Fatal("expected error for invalid CF-Connecting-IP")
	}
}

func TestExtractClientIP_InsecureWithCFHeader(t *testing.T) {
	// In insecure mode, IsTrustedSource always returns true,
	// so CF-Connecting-IP should still be used when present.
	v := NewCloudflareIPValidator(true, nil)
	r := httptest.NewRequest(http.MethodGet, "/", nil)
	r.RemoteAddr = "8.8.8.8:5678"
	r.Header.Set("CF-Connecting-IP", "198.51.100.1")

	ip, err := v.ExtractClientIP(r)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if ip != "198.51.100.1" {
		t.Errorf("got %q, want %q", ip, "198.51.100.1")
	}
}

func TestExtractClientIP_IPv6CFHeader(t *testing.T) {
	v := NewCloudflareIPValidator(false, nil)
	r := httptest.NewRequest(http.MethodGet, "/", nil)
	r.RemoteAddr = "[2606:4700::1]:443"
	r.Header.Set("CF-Connecting-IP", "2001:db8::1")

	ip, err := v.ExtractClientIP(r)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if ip != "2001:db8::1" {
		t.Errorf("got %q, want %q", ip, "2001:db8::1")
	}
}

func TestExtractClientIP_InsecureRemoteAddrNoPort(t *testing.T) {
	// When RemoteAddr has no port, SplitHostPort fails and raw RemoteAddr is returned.
	v := NewCloudflareIPValidator(true, nil)
	r := httptest.NewRequest(http.MethodGet, "/", nil)
	r.RemoteAddr = "192.168.1.1"

	ip, err := v.ExtractClientIP(r)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if ip != "192.168.1.1" {
		t.Errorf("got %q, want %q", ip, "192.168.1.1")
	}
}

func TestParsePrefixes_ValidCIDRs(t *testing.T) {
	cidrs := []string{
		"10.0.0.0/8",
		"192.168.0.0/16",
		"2001:db8::/32",
	}
	prefixes := parsePrefixes(cidrs)
	if len(prefixes) != 3 {
		t.Fatalf("expected 3 prefixes, got %d", len(prefixes))
	}
	// Verify each prefix parses to the expected value.
	expected := []string{"10.0.0.0/8", "192.168.0.0/16", "2001:db8::/32"}
	for i, p := range prefixes {
		want, _ := netip.ParsePrefix(expected[i])
		if p != want {
			t.Errorf("prefix[%d] = %v, want %v", i, p, want)
		}
	}
}

func TestParsePrefixes_InvalidCIDRsSkipped(t *testing.T) {
	cidrs := []string{
		"10.0.0.0/8",
		"not-a-cidr",
		"also/invalid",
		"192.168.0.0/16",
		"",
		"999.999.999.999/99",
	}
	prefixes := parsePrefixes(cidrs)
	if len(prefixes) != 2 {
		t.Fatalf("expected 2 valid prefixes, got %d", len(prefixes))
	}
}

func TestParsePrefixes_EmptyInput(t *testing.T) {
	prefixes := parsePrefixes(nil)
	if len(prefixes) != 0 {
		t.Fatalf("expected 0 prefixes for nil input, got %d", len(prefixes))
	}
	prefixes = parsePrefixes([]string{})
	if len(prefixes) != 0 {
		t.Fatalf("expected 0 prefixes for empty input, got %d", len(prefixes))
	}
}

func TestNewCloudflareIPValidator_FallbackRangesLoaded(t *testing.T) {
	v := NewCloudflareIPValidator(false, nil)
	ranges := v.ranges.Load()
	if ranges == nil {
		t.Fatal("expected non-nil ranges after construction")
	}
	// Should have 15 IPv4 + 6 IPv6 = 21 ranges.
	if len(*ranges) != 21 {
		t.Errorf("expected 21 fallback ranges, got %d", len(*ranges))
	}
}

func TestNewCloudflareIPValidator_LastRefreshSet(t *testing.T) {
	v := NewCloudflareIPValidator(false, nil)
	ts := v.lastRefresh.Load()
	if ts == 0 {
		t.Error("expected lastRefresh to be set after construction")
	}
}

func TestIsTrustedSource_NilRanges(t *testing.T) {
	// Construct validator then clear ranges to test nil-safety.
	v := NewCloudflareIPValidator(false, nil)
	v.ranges.Store(nil)
	if v.IsTrustedSource("104.16.1.1:1234") {
		t.Error("expected false when ranges are nil")
	}
}
