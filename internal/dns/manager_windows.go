//go:build windows

package dns

import (
	"context"
	"fmt"
	"net"
	"strings"
	"sync"
)

// interfaceLister abstracts network interface discovery for testing.
type interfaceLister func() ([]string, error)

// windowsManager implements DNSManager using netsh on Windows.
type windowsManager struct {
	originalDNS   map[string]string // interface name → original IPv4 DNS server
	originalDNSv6 map[string]string // interface name → original IPv6 DNS server
	mu            sync.Mutex
	run           commandRunner
	listInterfaces interfaceLister
}

// NewManager creates a Windows-specific DNS manager.
func NewManager() DNSManager {
	return &windowsManager{
		run:            defaultRunner,
		listInterfaces: activeInterfaces,
	}
}

// newManagerWithRunner creates a Windows DNS manager with a custom command runner (for testing).
func newManagerWithRunner(run commandRunner, lister interfaceLister) DNSManager {
	if lister == nil {
		lister = activeInterfaces
	}
	return &windowsManager{
		run:            run,
		listInterfaces: lister,
	}
}

// SetResolver modifies the DNS resolver on all active network interfaces to addr.
// On partial failure, already-modified interfaces are rolled back to their original DNS.
func (m *windowsManager) SetResolver(ctx context.Context, addr string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	interfaces, err := m.listInterfaces()
	if err != nil {
		return fmt.Errorf("dns: set resolver: %w", err)
	}

	if len(interfaces) == 0 {
		return ErrNoActiveInterface
	}

	m.originalDNS = make(map[string]string, len(interfaces))
	m.originalDNSv6 = make(map[string]string, len(interfaces))
	var modified []string

	for _, iface := range interfaces {
		original, err := m.getCurrentDNS(ctx, iface)
		if err != nil {
			m.rollback(ctx, modified)
			m.originalDNS = nil
			m.originalDNSv6 = nil
			return fmt.Errorf("dns: set resolver: get current dns for %q: %w", iface, err)
		}
		m.originalDNS[iface] = original

		originalV6, _ := m.getCurrentDNSv6(ctx, iface)
		m.originalDNSv6[iface] = originalV6

		// Set IPv4 DNS
		out, err := m.run(ctx, "netsh", "interface", "ip", "set", "dns",
			fmt.Sprintf(`name="%s"`, iface), "static", addr)
		if err != nil {
			m.rollback(ctx, modified)
			m.originalDNS = nil
			m.originalDNSv6 = nil
			return fmt.Errorf("dns: set resolver: netsh set dns for %q: %s: %w", iface, string(out), ErrSetResolverFailed)
		}

		// Set IPv6 DNS to ::1 to prevent IPv6 DNS bypass
		m.run(ctx, "netsh", "interface", "ipv6", "set", "dns",
			fmt.Sprintf(`name="%s"`, iface), "static", "::1")

		modified = append(modified, iface)
	}

	return nil
}

// rollback restores already-modified interfaces to their original DNS on SetResolver failure.
func (m *windowsManager) rollback(ctx context.Context, modified []string) {
	for _, iface := range modified {
		nameArg := fmt.Sprintf(`name="%s"`, iface)

		// Restore IPv4
		original := m.originalDNS[iface]
		if original == "" || strings.EqualFold(original, "dhcp") {
			m.run(ctx, "netsh", "interface", "ip", "set", "dns", nameArg, "dhcp")
		} else {
			m.run(ctx, "netsh", "interface", "ip", "set", "dns", nameArg, "static", original)
		}

		// Restore IPv6
		originalV6 := m.originalDNSv6[iface]
		if originalV6 == "" || strings.EqualFold(originalV6, "dhcp") {
			m.run(ctx, "netsh", "interface", "ipv6", "set", "dns", nameArg, "dhcp")
		} else {
			m.run(ctx, "netsh", "interface", "ipv6", "set", "dns", nameArg, "static", originalV6)
		}
	}
}

// RestoreResolver restores the DNS resolver to its original value on all modified interfaces.
func (m *windowsManager) RestoreResolver(ctx context.Context) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.originalDNS == nil {
		return nil
	}

	var lastErr error
	for iface, original := range m.originalDNS {
		nameArg := fmt.Sprintf(`name="%s"`, iface)

		// Restore IPv4
		var out []byte
		var err error
		if original == "" || strings.EqualFold(original, "dhcp") {
			out, err = m.run(ctx, "netsh", "interface", "ip", "set", "dns", nameArg, "dhcp")
		} else {
			out, err = m.run(ctx, "netsh", "interface", "ip", "set", "dns", nameArg, "static", original)
		}
		if err != nil {
			lastErr = fmt.Errorf("dns: restore resolver for %q: %s: %w", iface, string(out), ErrRestoreFailed)
		}

		// Restore IPv6
		originalV6 := m.originalDNSv6[iface]
		if originalV6 == "" || strings.EqualFold(originalV6, "dhcp") {
			out, err = m.run(ctx, "netsh", "interface", "ipv6", "set", "dns", nameArg, "dhcp")
		} else {
			out, err = m.run(ctx, "netsh", "interface", "ipv6", "set", "dns", nameArg, "static", originalV6)
		}
		if err != nil {
			lastErr = fmt.Errorf("dns: restore ipv6 resolver for %q: %s: %w", iface, string(out), ErrRestoreFailed)
		}
	}

	m.originalDNS = nil
	m.originalDNSv6 = nil
	return lastErr
}

// OriginalResolver returns the first saved original DNS address, or empty string.
func (m *windowsManager) OriginalResolver() string {
	m.mu.Lock()
	defer m.mu.Unlock()

	for _, dns := range m.originalDNS {
		if dns != "" {
			return dns
		}
	}
	return ""
}

// getCurrentDNS retrieves the current IPv4 DNS server for a given interface using netsh.
func (m *windowsManager) getCurrentDNS(ctx context.Context, ifaceName string) (string, error) {
	out, err := m.run(ctx, "netsh", "interface", "ip", "show", "dns", ifaceName)
	if err != nil {
		return "", fmt.Errorf("dns: get current dns: %w", err)
	}

	return parseDNSFromNetsh(string(out)), nil
}

// getCurrentDNSv6 retrieves the current IPv6 DNS server for a given interface using netsh.
func (m *windowsManager) getCurrentDNSv6(ctx context.Context, ifaceName string) (string, error) {
	out, err := m.run(ctx, "netsh", "interface", "ipv6", "show", "dns", ifaceName)
	if err != nil {
		return "", fmt.Errorf("dns: get current ipv6 dns: %w", err)
	}

	return parseDNSFromNetsh(string(out)), nil
}

// parseDNSFromNetsh extracts the DNS server address from netsh output.
func parseDNSFromNetsh(output string) string {
	lines := strings.Split(output, "\n")
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		parts := strings.Fields(trimmed)
		for _, part := range parts {
			if net.ParseIP(part) != nil {
				return part
			}
		}
	}

	// No IP found — check if DHCP is configured
	lower := strings.ToLower(output)
	if strings.Contains(lower, "dhcp") {
		return "dhcp"
	}

	return ""
}

// activeInterfaces returns the names of all active, non-loopback network interfaces.
func activeInterfaces() ([]string, error) {
	ifaces, err := net.Interfaces()
	if err != nil {
		return nil, fmt.Errorf("dns: list interfaces: %w", err)
	}

	var names []string
	for _, iface := range ifaces {
		if iface.Flags&net.FlagUp == 0 {
			continue
		}
		if iface.Flags&net.FlagLoopback != 0 {
			continue
		}
		names = append(names, iface.Name)
	}

	return names, nil
}
