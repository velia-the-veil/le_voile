//go:build linux

package dns

import (
	"context"
	"fmt"
	"net"
	"os"
	"strings"
	"sync"
)

const resolvConfPath = "/etc/resolv.conf"
const resolvConfBackup = "/etc/resolv.conf.levoile.bak"

// linuxManager implements DNSManager using resolvectl or resolv.conf on Linux.
type linuxManager struct {
	originalDNS   string // original resolv.conf content or resolvectl DNS output
	useResolvectl bool
	mu            sync.Mutex
	run           commandRunner
}

// NewManager creates a Linux-specific DNS manager.
func NewManager() DNSManager {
	return newManagerWithRunner(defaultRunner)
}

// newManagerWithRunner creates a Linux DNS manager with a custom command runner (for testing).
func newManagerWithRunner(run commandRunner) DNSManager {
	m := &linuxManager{run: run}
	m.useResolvectl = m.hasResolvectl()
	return m
}

// hasResolvectl checks if resolvectl is available on the system.
func (m *linuxManager) hasResolvectl() bool {
	_, err := m.run(context.Background(), "which", "resolvectl")
	return err == nil
}

// SetResolver modifies the DNS resolver to point to addr.
func (m *linuxManager) SetResolver(ctx context.Context, addr string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.useResolvectl {
		return m.setResolverResolvectl(ctx, addr)
	}
	return m.setResolverResolvConf(ctx, addr)
}

// RestoreResolver restores the DNS resolver to its original value.
func (m *linuxManager) RestoreResolver(ctx context.Context) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.originalDNS == "" {
		return nil
	}

	if m.useResolvectl {
		return m.restoreResolvectl(ctx)
	}
	return m.restoreResolvConf()
}

// OriginalResolver returns the saved original DNS configuration.
func (m *linuxManager) OriginalResolver() string {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.originalDNS
}

// --- resolvectl implementation ---

func (m *linuxManager) setResolverResolvectl(ctx context.Context, addr string) error {
	// Save current DNS for all interfaces
	out, err := m.run(ctx, "resolvectl", "dns")
	if err != nil {
		return fmt.Errorf("dns: set resolver: get current dns: %w", err)
	}
	m.originalDNS = strings.TrimSpace(string(out))

	// Parse ALL interfaces from resolvectl output
	interfaces := parseAllResolvectlInterfaces(string(out))
	if len(interfaces) == 0 {
		// Audit fix D3 (2026-05-04) — fallback enumerates the kernel's
		// real interfaces (UP, non-loopback, non-tunnel) instead of
		// hard-coding "eth0", which doesn't exist on Wi-Fi laptops, VMs
		// with predictable names (enpXsY), or Fedora/Arch hosts.
		interfaces = enumerateActiveInterfaces()
	}

	// Modify DNS on ALL interfaces
	for _, iface := range interfaces {
		out, err = m.run(ctx, "resolvectl", "dns", iface, addr)
		if err != nil {
			return fmt.Errorf("dns: set resolver: resolvectl for %q: %s: %w", iface, string(out), ErrSetResolverFailed)
		}
		// Audit fix D4 (2026-05-04) — disable mDNS and LLMNR on the
		// physical interfaces while the tunnel is up. The kill-switch
		// already drops outbound multicast at the firewall layer, but
		// systemd-resolved still emits its own mDNS / LLMNR queries on
		// behalf of getaddrinfo callers; turning the resolver-side
		// switch off prevents hostnames being broadcast to the LAN at
		// all (a meaningful fingerprint signal in repressive networks).
		// Best-effort: failures are logged via stderr but do not abort
		// SetResolver — DNS via the proxy still works.
		_, _ = m.run(ctx, "resolvectl", "mdns", iface, "off")
		_, _ = m.run(ctx, "resolvectl", "llmnr", iface, "off")
	}

	return nil
}

func (m *linuxManager) restoreResolvectl(ctx context.Context) error {
	// Revert ALL interfaces from saved state
	interfaces := parseAllResolvectlInterfaces(m.originalDNS)
	if len(interfaces) == 0 {
		interfaces = enumerateActiveInterfaces()
	}

	var lastErr error
	for _, iface := range interfaces {
		out, err := m.run(ctx, "resolvectl", "revert", iface)
		if err != nil {
			lastErr = fmt.Errorf("dns: restore resolver: resolvectl for %q: %s: %w", iface, string(out), ErrRestoreFailed)
		}
	}

	// `resolvectl revert` vide les scopes DNS de l'interface sans notifier
	// NetworkManager. Sur les distros desktop (Mint/Ubuntu/Fedora), NM gère
	// le DHCP et c'est lui qui doit ré-appliquer les serveurs DNS originaux.
	// Sans ce nudge, l'interface reste en "Current Scopes: none" → tout
	// futur lookup retourne SERVFAIL, internet est cassé après chaque
	// shutdown du service. `nmcli device reapply <iface>` force NM à
	// ré-appliquer la config (DHCP DNS inclus). Best-effort : si nmcli est
	// absent (systemd-networkd pur, ou container), on skip silencieusement
	// — l'utilisateur peut toujours `systemctl restart NetworkManager` ou
	// rebooter pour récupérer le DNS.
	for _, iface := range interfaces {
		// Erreur ignorée par design : nmcli peut être absent (pas un échec
		// du restore stricto sensu), ou l'iface peut ne pas être managed
		// par NM (e.g. levoile0 elle-même n'a pas de connexion NM).
		_, _ = m.run(ctx, "nmcli", "device", "reapply", iface)
	}

	m.originalDNS = ""
	return lastErr
}

// enumerateActiveInterfaces returns the list of physical interfaces
// currently UP and not loopback / not a Le Voile tunnel device. Used as
// the D3 fallback when resolvectl returns no parseable Link lines (audit
// fix 2026-05-04). Skips loopback (lo), the tunnel itself (levoile*) and
// classic VPN/virtual prefixes that have no business hosting an upstream
// resolver. Returns empty slice if nothing matches — the caller will then
// fail loudly in the resolvectl step rather than silently misconfigure
// the wrong device, which is the safer outcome on an exotic host.
func enumerateActiveInterfaces() []string {
	ifs, err := net.Interfaces()
	if err != nil {
		return nil
	}
	var out []string
	for _, ifi := range ifs {
		if ifi.Flags&net.FlagUp == 0 || ifi.Flags&net.FlagLoopback != 0 {
			continue
		}
		name := ifi.Name
		if strings.HasPrefix(name, "levoile") ||
			strings.HasPrefix(name, "tun") ||
			strings.HasPrefix(name, "tap") ||
			strings.HasPrefix(name, "wg") ||
			strings.HasPrefix(name, "docker") ||
			strings.HasPrefix(name, "veth") ||
			strings.HasPrefix(name, "br-") {
			continue
		}
		out = append(out, name)
	}
	return out
}

// parseAllResolvectlInterfaces extracts ALL interface names from resolvectl dns output.
// Format: "Link 2 (eth0): 192.168.1.1\nLink 3 (wlan0): 8.8.8.8"
func parseAllResolvectlInterfaces(output string) []string {
	var interfaces []string
	lines := strings.Split(output, "\n")
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "Link") {
			start := strings.Index(trimmed, "(")
			end := strings.Index(trimmed, ")")
			if start >= 0 && end > start {
				interfaces = append(interfaces, trimmed[start+1:end])
			}
		}
	}
	return interfaces
}

// --- resolv.conf implementation ---

func (m *linuxManager) setResolverResolvConf(ctx context.Context, addr string) error {
	// Read and save current resolv.conf
	content, err := os.ReadFile(resolvConfPath)
	if err != nil {
		return fmt.Errorf("dns: set resolver: read resolv.conf: %w", err)
	}
	m.originalDNS = string(content)

	// Preserve original file permissions
	perm := os.FileMode(0644)
	if info, err := os.Stat(resolvConfPath); err == nil {
		perm = info.Mode().Perm()
	}

	// Write backup with same permissions
	if err := os.WriteFile(resolvConfBackup, content, perm); err != nil {
		return fmt.Errorf("dns: set resolver: backup resolv.conf: %w", err)
	}

	// Validate addr is a valid IP address to prevent injection via newlines.
	if net.ParseIP(addr) == nil {
		return fmt.Errorf("dns: set resolver: invalid IP address %q", addr)
	}

	// Write new resolv.conf with same permissions
	newContent := fmt.Sprintf("# Generated by Le Voile VPN\nnameserver %s\n", addr)
	if err := os.WriteFile(resolvConfPath, []byte(newContent), perm); err != nil {
		return fmt.Errorf("dns: set resolver: write resolv.conf: %w", ErrSetResolverFailed)
	}

	return nil
}

func (m *linuxManager) restoreResolvConf() error {
	// Preserve original file permissions
	perm := os.FileMode(0644)
	if info, err := os.Stat(resolvConfPath); err == nil {
		perm = info.Mode().Perm()
	}

	backup, err := os.ReadFile(resolvConfBackup)
	if err != nil {
		// Try to use saved original content
		if m.originalDNS != "" {
			if err := os.WriteFile(resolvConfPath, []byte(m.originalDNS), perm); err != nil {
				return fmt.Errorf("dns: restore resolver: write resolv.conf: %w", ErrRestoreFailed)
			}
			m.originalDNS = ""
			return nil
		}
		return fmt.Errorf("dns: restore resolver: read backup: %w", ErrRestoreFailed)
	}

	if err := os.WriteFile(resolvConfPath, backup, perm); err != nil {
		return fmt.Errorf("dns: restore resolver: write resolv.conf: %w", ErrRestoreFailed)
	}

	os.Remove(resolvConfBackup)
	m.originalDNS = ""
	return nil
}

// RecoverOrphanDNS restores DNS from backup resolv.conf if a previous session crashed.
func RecoverOrphanDNS(_ context.Context) error {
	if _, err := os.Stat(resolvConfBackup); err != nil {
		return nil // no backup → nothing to recover
	}
	backup, err := os.ReadFile(resolvConfBackup)
	if err != nil {
		return nil
	}
	perm := os.FileMode(0644)
	if info, err := os.Stat(resolvConfPath); err == nil {
		perm = info.Mode().Perm()
	}
	if err := os.WriteFile(resolvConfPath, backup, perm); err != nil {
		return fmt.Errorf("dns: recover orphan: %w", err)
	}
	os.Remove(resolvConfBackup)
	return nil
}
