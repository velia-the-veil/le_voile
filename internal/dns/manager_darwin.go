//go:build darwin

package dns

import (
	"context"
	"fmt"
	"strings"
	"sync"
)

// darwinManager implements DNSManager using networksetup on macOS.
type darwinManager struct {
	originalDNS map[string][]string // service name → original DNS servers
	mu          sync.Mutex
	run         commandRunner
}

// NewManager creates a macOS-specific DNS manager.
func NewManager() DNSManager {
	return newManagerWithRunner(defaultRunner)
}

// newManagerWithRunner creates a macOS DNS manager with a custom command runner (for testing).
func newManagerWithRunner(run commandRunner) DNSManager {
	return &darwinManager{run: run}
}

// SetResolver modifies the DNS resolver on all network services to addr.
func (m *darwinManager) SetResolver(ctx context.Context, addr string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	services, err := m.listNetworkServices(ctx)
	if err != nil {
		return fmt.Errorf("dns: set resolver: %w", err)
	}

	if len(services) == 0 {
		return ErrNoActiveInterface
	}

	m.originalDNS = make(map[string][]string, len(services))

	for _, svc := range services {
		servers, err := m.getDNSServers(ctx, svc)
		if err != nil {
			return fmt.Errorf("dns: set resolver: get dns for %q: %w", svc, err)
		}
		m.originalDNS[svc] = servers

		out, err := m.run(ctx, "networksetup", "-setdnsservers", svc, addr)
		if err != nil {
			return fmt.Errorf("dns: set resolver: networksetup for %q: %s: %w", svc, string(out), ErrSetResolverFailed)
		}
	}

	return nil
}

// RestoreResolver restores the DNS resolver to its original value on all modified services.
func (m *darwinManager) RestoreResolver(ctx context.Context) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.originalDNS == nil {
		return nil
	}

	var lastErr error
	for svc, servers := range m.originalDNS {
		var out []byte
		var err error

		if len(servers) == 0 {
			// No DNS was set → restore to DHCP (Empty)
			out, err = m.run(ctx, "networksetup", "-setdnsservers", svc, "Empty")
		} else {
			args := append([]string{"-setdnsservers", svc}, servers...)
			out, err = m.run(ctx, "networksetup", args...)
		}

		if err != nil {
			lastErr = fmt.Errorf("dns: restore resolver for %q: %s: %w", svc, string(out), ErrRestoreFailed)
		}
	}

	m.originalDNS = nil
	return lastErr
}

// OriginalResolver returns the first saved original DNS address, or empty string.
func (m *darwinManager) OriginalResolver() string {
	m.mu.Lock()
	defer m.mu.Unlock()

	for _, servers := range m.originalDNS {
		if len(servers) > 0 {
			return servers[0]
		}
	}
	return ""
}

// listNetworkServices returns the list of network services (e.g., "Wi-Fi", "Ethernet").
func (m *darwinManager) listNetworkServices(ctx context.Context) ([]string, error) {
	out, err := m.run(ctx, "networksetup", "-listallnetworkservices")
	if err != nil {
		return nil, fmt.Errorf("dns: list network services: %w", err)
	}

	return parseNetworkServices(string(out)), nil
}

// parseNetworkServices parses the output of networksetup -listallnetworkservices.
func parseNetworkServices(output string) []string {
	var services []string
	lines := strings.Split(output, "\n")
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" {
			continue
		}
		// Skip the header line and disabled services (marked with *)
		if strings.HasPrefix(trimmed, "An asterisk") || strings.HasPrefix(trimmed, "*") {
			continue
		}
		services = append(services, trimmed)
	}
	return services
}

// getDNSServers returns the current DNS servers for a given service.
func (m *darwinManager) getDNSServers(ctx context.Context, service string) ([]string, error) {
	out, err := m.run(ctx, "networksetup", "-getdnsservers", service)
	if err != nil {
		return nil, fmt.Errorf("dns: get dns servers: %w", err)
	}

	return parseDNSServers(string(out)), nil
}

// parseDNSServers parses the output of networksetup -getdnsservers.
func parseDNSServers(output string) []string {
	trimmed := strings.TrimSpace(output)

	// "There aren't any DNS Servers set on ..." means DHCP
	if strings.Contains(trimmed, "aren't any") {
		return nil
	}

	var servers []string
	for _, line := range strings.Split(trimmed, "\n") {
		s := strings.TrimSpace(line)
		if s != "" {
			servers = append(servers, s)
		}
	}
	return servers
}
