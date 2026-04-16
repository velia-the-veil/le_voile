//go:build linux

package firewall

import (
	"net"
	"strings"
	"testing"
)

func TestRenderRuleset_Nominal(t *testing.T) {
	got, err := renderRuleset(net.ParseIP("198.51.100.42"), "levoile0")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Verify key elements in the rendered ruleset
	checks := []string{
		`table inet levoile {}`,
		`flush table inet levoile`,
		`table inet levoile {`,
		`oifname "levoile0" accept`,
		`ip daddr 198.51.100.42 udp dport 443 accept`,
		`ip daddr 198.51.100.42 tcp dport 443 accept`,
		`iifname "lo" accept`,
		`ct state established,related accept`,
		`policy drop`,
		`iifname "levoile0" accept`,
	}
	for _, want := range checks {
		if !strings.Contains(got, want) {
			t.Errorf("ruleset missing %q\nGot:\n%s", want, got)
		}
	}
}

func TestRenderRuleset_PrivateIP(t *testing.T) {
	got, err := renderRuleset(net.ParseIP("10.0.0.1"), "tun0")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(got, "ip daddr 10.0.0.1 udp dport 443 accept") {
		t.Errorf("private IP not rendered correctly:\n%s", got)
	}
}

func TestRenderRuleset_NilIP(t *testing.T) {
	_, err := renderRuleset(nil, "levoile0")
	if err == nil {
		t.Fatal("expected error for nil IP")
	}
}

func TestRenderRuleset_IPv6Rejected(t *testing.T) {
	_, err := renderRuleset(net.ParseIP("2001:db8::1"), "levoile0")
	if err == nil {
		t.Fatal("expected error for IPv6")
	}
}

func TestRenderRuleset_EmptyTunName(t *testing.T) {
	_, err := renderRuleset(net.ParseIP("1.2.3.4"), "")
	if err == nil {
		t.Fatal("expected error for empty tun name")
	}
}

func TestRenderRuleset_InvalidTunName(t *testing.T) {
	bad := []string{
		`lo" accept; chain x { policy accept }; #`,
		"a b c",
		"name_with_quotes\"",
		"aaaaaaaaaaaaaaaa", // 16 chars, exceeds IFNAMSIZ-1
	}
	for _, name := range bad {
		_, err := renderRuleset(net.ParseIP("1.2.3.4"), name)
		if err == nil {
			t.Errorf("expected error for invalid tun name %q", name)
		}
	}
}

func TestRenderRuleset_ValidTunNames(t *testing.T) {
	valid := []string{"levoile0", "tun0", "wg0", "eth0.1", "my-tun_0"}
	for _, name := range valid {
		_, err := renderRuleset(net.ParseIP("1.2.3.4"), name)
		if err != nil {
			t.Errorf("valid name %q rejected: %v", name, err)
		}
	}
}
