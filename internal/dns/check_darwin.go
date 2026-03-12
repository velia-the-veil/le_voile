//go:build darwin

package dns

import (
	"context"
	"fmt"
	"os/exec"
	"strings"
)

// CheckCurrentResolver returns the current system DNS resolver address
// by querying networksetup on macOS.
func CheckCurrentResolver(ctx context.Context) (string, error) {
	out, err := exec.CommandContext(ctx, "networksetup", "-listallnetworkservices").CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("dns: check resolver: %w", err)
	}

	services := parseNetworkServices(string(out))
	for _, svc := range services {
		out, err := exec.CommandContext(ctx, "networksetup", "-getdnsservers", svc).CombinedOutput()
		if err != nil {
			continue
		}
		servers := parseDNSServers(string(out))
		if len(servers) > 0 {
			return servers[0], nil
		}
	}

	return "", fmt.Errorf("dns: check resolver: no DNS servers found")
}

// ForceResolver sets the system DNS resolver on all services without
// saving the original value. Used by the watchdog for corrections.
func ForceResolver(ctx context.Context, addr string) error {
	out, err := exec.CommandContext(ctx, "networksetup", "-listallnetworkservices").CombinedOutput()
	if err != nil {
		return fmt.Errorf("dns: force resolver: %w", err)
	}

	services := parseNetworkServices(string(out))
	for _, svc := range services {
		if out, err := exec.CommandContext(ctx, "networksetup", "-setdnsservers", svc, addr).CombinedOutput(); err != nil {
			return fmt.Errorf("dns: force resolver for %q: %s: %w", svc, strings.TrimSpace(string(out)), err)
		}
	}

	return nil
}
