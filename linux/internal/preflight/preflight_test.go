//go:build linux

package preflight

import (
	"errors"
	"testing"
)

func TestMatchLinux(t *testing.T) {
	cases := []struct {
		name    string
		iface   string
		wantPat string
		wantOK  bool
	}{
		{"wg", "wg0", "wg", true},
		{"wg-mullvad", "wg-mullvad", "wg", true},
		{"wireguard", "wireguard0", "wireguard", true},
		{"tun", "tun5", "tun", true},
		{"tap", "tap0", "tap", true},
		{"utun", "utun3", "utun", true},
		{"ppp", "ppp0", "ppp", true},
		{"ipsec", "ipsec0", "ipsec", true},
		{"gpd", "gpd0", "gpd", true},
		{"case-insensitive", "TUN0", "tun", true},

		{"eth0 ignored", "eth0", "", false},
		{"wlan0 ignored", "wlan0", "", false},
		{"docker0 ignored", "docker0", "", false},
		{"vmnet0 ignored", "vmnet0", "", false},
		{"own interface ignored", "levoile0", "", false},
		{"loopback ignored", "lo", "", false},
		{"empty ignored", "", "", false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			pat, ok := matchLinux(c.iface)
			if ok != c.wantOK || pat != c.wantPat {
				t.Errorf("matchLinux(%q) = (%q, %v), want (%q, %v)", c.iface, pat, ok, c.wantPat, c.wantOK)
			}
		})
	}
}

func TestMatchWindows(t *testing.T) {
	cases := []struct {
		name    string
		ifName  string
		desc    string
		wantPat string
		wantOK  bool
	}{
		{"wireguard", "Local Area Connection", "WireGuard Tunnel #3", "WireGuard Tunnel", true},
		{"tap-windows", "TAP", "TAP-Windows Adapter V9", "TAP-Windows", true},
		{"openvpn-plain", "x", "OpenVPN Data Channel Offload", "OpenVPN", true},
		{"nordvpn", "x", "NordLynx Virtual Adapter by NordVPN", "NordVPN", true},
		{"wintun third-party", "Mullvad", "Wintun Userspace Tunnel", "Wintun", true},
		{"case-insensitive", "X", "wireguard tunnel", "WireGuard Tunnel", true},

		{"ethernet ignored", "Ethernet", "Intel(R) Ethernet Connection I219-V", "", false},
		{"wifi ignored", "Wi-Fi", "Intel(R) Wi-Fi 6 AX201 160MHz", "", false},
		{"empty description ignored", "foo", "", "", false},
		{"own levoile0 excluded even with wintun desc", OwnInterfaceName, "Wintun Userspace Tunnel", "", false},

		// Fallback: no Description → match by name (Linux-style prefixes)
		{"fallback wg name no desc", "wg0", "", "wg", true},
		{"fallback tun name no desc", "tun5", "", "tun", true},
		{"fallback eth name no desc", "eth0", "", "", false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			pat, ok := matchWindows(c.ifName, c.desc)
			if ok != c.wantOK || pat != c.wantPat {
				t.Errorf("matchWindows(%q,%q) = (%q,%v), want (%q,%v)", c.ifName, c.desc, pat, ok, c.wantPat, c.wantOK)
			}
		})
	}
}

func TestErrConcurrentVPN_Message(t *testing.T) {
	err := &ErrConcurrentVPN{InterfaceName: "wg0", MatchedPattern: "wg"}
	got := err.Error()
	want := "VPN concurrent détecté (wg0). Déconnectez-le pour utiliser Le Voile."
	if got != want {
		t.Errorf("Error() = %q, want %q", got, want)
	}
	var e *ErrConcurrentVPN
	if !errors.As(err, &e) {
		t.Error("errors.As failed")
	}
}

func TestDetect_DownInterfacesIgnored(t *testing.T) {
	ifs := []Interface{
		{Name: "wg0", IsUp: false}, // DOWN → ignored even if VPN-like
		{Name: "eth0", IsUp: true},
	}
	scanned, err := detect(ifs, func(i Interface) (string, bool) { return matchLinux(i.Name) })
	if err != nil {
		t.Fatalf("detect() err=%v, want nil", err)
	}
	if len(scanned) != 1 || scanned[0] != "eth0" {
		t.Errorf("scanned=%v, want [eth0]", scanned)
	}
}

func TestDetect_OwnInterfaceIgnored(t *testing.T) {
	ifs := []Interface{
		{Name: OwnInterfaceName, IsUp: true}, // crash-recovery case
		{Name: "eth0", IsUp: true},
	}
	_, err := detect(ifs, func(i Interface) (string, bool) { return matchLinux(i.Name) })
	if err != nil {
		t.Fatalf("detect() err=%v, want nil", err)
	}
}

func TestDetect_FirstMatchWins(t *testing.T) {
	ifs := []Interface{
		{Name: "eth0", IsUp: true},
		{Name: "wg0", IsUp: true},
		{Name: "tun3", IsUp: true},
	}
	_, err := detect(ifs, func(i Interface) (string, bool) { return matchLinux(i.Name) })
	var e *ErrConcurrentVPN
	if !errors.As(err, &e) {
		t.Fatalf("err=%v, want *ErrConcurrentVPN", err)
	}
	if e.InterfaceName != "wg0" {
		t.Errorf("got %q, want wg0", e.InterfaceName)
	}
}
