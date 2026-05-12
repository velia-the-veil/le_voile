package relay

import "testing"

// TestSubnetPrefix covers the helper that groups client IPs into /24 (IPv4)
// or /64 (IPv6) buckets for the aggregate subnet quota (fix H4). If this
// function mis-classifies two addresses in the same /24 as "different",
// an attacker rotating IPs inside a block trivially bypasses the quota
// cap — the whole point of the H4 fix.
func TestSubnetPrefix(t *testing.T) {
	cases := []struct {
		in   string
		want string
	}{
		{"203.0.113.5", "203.0.113.0/24"},
		{"203.0.113.250", "203.0.113.0/24"},
		{"198.51.100.1", "198.51.100.0/24"},
		{"2001:db8::1", "2001:db8::/64"},
		{"2001:db8::feed:beef", "2001:db8::/64"},
		{"[::1]", "::/64"},
		{"fe80::1%eth0", "fe80::/64"},
		{"", ""},
		{"not-an-ip", ""},
	}
	for _, tc := range cases {
		got := subnetPrefix(tc.in)
		if got != tc.want {
			t.Errorf("subnetPrefix(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

// TestSubnetPrefix_GroupsRotation confirms that two IPs minted for a
// rotation attack inside a single /24 map to the exact same bucket key
// — meaning the quota state for one aggregates across both.
func TestSubnetPrefix_GroupsRotation(t *testing.T) {
	a := subnetPrefix("203.0.113.5")
	b := subnetPrefix("203.0.113.200")
	if a == "" || a != b {
		t.Errorf("/24 rotation not grouped: a=%q b=%q", a, b)
	}
}
