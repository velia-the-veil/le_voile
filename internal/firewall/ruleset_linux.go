//go:build linux

package firewall

import (
	"bytes"
	_ "embed"
	"fmt"
	"net"
	"regexp"
	"text/template"
)

// validTunName matches valid Linux interface names (IFNAMSIZ=16, alphanumeric + _ - .).
var validTunName = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9_.\-]{0,14}$`)

//go:embed ruleset.nft.tmpl
var rulesetTemplate string

var parsedTemplate = template.Must(template.New("ruleset").Parse(rulesetTemplate))

// rulesetParams holds the values injected into the nftables template.
type rulesetParams struct {
	RelayIP string
	TunName string
}

// renderRuleset generates the complete nftables script from the embedded
// template. It rejects nil IP, empty tunName, and IPv6 addresses (out of
// scope — Story 2.9).
func renderRuleset(relayIP net.IP, tunName string) (string, error) {
	if relayIP == nil {
		return "", fmt.Errorf("firewall: relay IP is nil")
	}
	ip4 := relayIP.To4()
	if ip4 == nil {
		return "", fmt.Errorf("firewall: IPv6 relay not supported (got %s)", relayIP)
	}
	if tunName == "" {
		return "", fmt.Errorf("firewall: TUN interface name is empty")
	}
	if !validTunName.MatchString(tunName) {
		return "", fmt.Errorf("firewall: invalid TUN interface name %q", tunName)
	}

	var buf bytes.Buffer
	if err := parsedTemplate.Execute(&buf, rulesetParams{
		RelayIP: ip4.String(),
		TunName: tunName,
	}); err != nil {
		return "", fmt.Errorf("firewall: template render: %w", err)
	}
	return buf.String(), nil
}

// captiveRulesetTemplate is the nftables script for captive portal mode.
// Allows traffic needed for Wi-Fi captive portal authentication:
//   - DNS (UDP 53) to any server (portal DNS may not be the gateway)
//   - HTTP/HTTPS (TCP 80/443) to any server (portal login pages are often
//     hosted on a different IP than the gateway, e.g. hotel cloud portals)
//   - Gateway IP (any protocol, for local portal pages served by the AP)
//   - DHCP (UDP 67/68) for lease renewal
//   - ICMP for basic connectivity probes
//   - Loopback for IPC
// Conntrack established/related covers return traffic for all of the above.
const captiveRulesetTemplate = `table inet levoile {}
flush table inet levoile
table inet levoile {
  chain input {
    type filter hook input priority 0; policy drop;
    iifname "lo" accept
    ct state established,related accept
  }
  chain output {
    type filter hook output priority 0; policy drop;
    oifname "lo" accept
    ct state established,related accept
    ip daddr {{.GatewayIP}} accept
    udp dport 53 accept
    tcp dport { 80, 443 } accept
    ip protocol icmp accept
    udp dport 67 accept
    udp sport 68 accept
  }
}
`

var parsedCaptiveTemplate = template.Must(template.New("captive").Parse(captiveRulesetTemplate))

// captiveRulesetParams holds the values injected into the captive nftables template.
type captiveRulesetParams struct {
	GatewayIP string
}

// renderCaptiveRuleset generates the nftables script for captive portal mode.
func renderCaptiveRuleset(lanGateway net.IP) (string, error) {
	if lanGateway == nil {
		return "", fmt.Errorf("firewall: LAN gateway IP is nil")
	}
	ip4 := lanGateway.To4()
	if ip4 == nil {
		return "", fmt.Errorf("firewall: IPv6 LAN gateway not supported (got %s)", lanGateway)
	}

	var buf bytes.Buffer
	if err := parsedCaptiveTemplate.Execute(&buf, captiveRulesetParams{
		GatewayIP: ip4.String(),
	}); err != nil {
		return "", fmt.Errorf("firewall: captive template render: %w", err)
	}
	return buf.String(), nil
}
