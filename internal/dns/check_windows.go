//go:build windows

package dns

import (
	"context"
	"fmt"
	"os/exec"
	"strings"
)

// CheckCurrentResolver returns the current system DNS resolver address
// by querying netsh on the first active interface.
func CheckCurrentResolver(ctx context.Context) (string, error) {
	ifaces, err := activeInterfaces()
	if err != nil {
		return "", fmt.Errorf("dns: check resolver: %w", err)
	}

	if len(ifaces) == 0 {
		return "", ErrNoActiveInterface
	}

	out, err := exec.CommandContext(ctx, "netsh", "interface", "ip", "show", "dns", ifaces[0]).CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("dns: check resolver: %w", err)
	}

	return parseDNSFromNetsh(string(out)), nil
}

// ForceResolver sets the system DNS resolver on all active interfaces
// without saving the original value. Used by the watchdog for corrections.
func ForceResolver(ctx context.Context, addr string) error {
	ifaces, err := activeInterfaces()
	if err != nil {
		return fmt.Errorf("dns: force resolver: %w", err)
	}

	for _, iface := range ifaces {
		out, err := exec.CommandContext(ctx, "netsh", "interface", "ip", "set", "dns",
			fmt.Sprintf("name=%s", iface), "static", addr).CombinedOutput()
		if err != nil {
			return fmt.Errorf("dns: force resolver for %q: %s: %w", iface, strings.TrimSpace(string(out)), err)
		}
	}

	return nil
}

