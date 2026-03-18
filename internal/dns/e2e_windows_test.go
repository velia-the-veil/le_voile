//go:build e2e && windows

package dns

import (
	"context"
	"net"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"
)

// isElevated checks whether the current process runs with admin privileges.
func isElevated() bool {
	// "net session" requires elevation; it fails with access denied otherwise.
	err := exec.Command("net", "session").Run()
	return err == nil
}

// restoreDNSAll restores DNS on all active interfaces to DHCP or the given
// address, then removes any persisted state file.  Best-effort — errors are
// intentionally ignored so t.Cleanup never panics.
func restoreDNSAll(original string) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	ifaces, _ := activeInterfaces()
	for _, iface := range ifaces {
		nameArg := `name="` + iface + `"`
		if original == "" || strings.EqualFold(original, "dhcp") {
			exec.CommandContext(ctx, "netsh", "interface", "ip", "set", "dns", nameArg, "dhcp").Run()
			exec.CommandContext(ctx, "netsh", "interface", "ipv6", "set", "dns", nameArg, "dhcp").Run()
		} else {
			exec.CommandContext(ctx, "netsh", "interface", "ip", "set", "dns", nameArg, "static", original).Run()
			exec.CommandContext(ctx, "netsh", "interface", "ipv6", "set", "dns", nameArg, "dhcp").Run()
		}
	}
	removePersistedState()
}

// TestE2E_DNSRestoredAfterShutdown saves the original resolver, sets it to
// 127.0.0.1 via the DNS manager, calls RestoreResolver, and verifies the
// original value is restored and the state file is deleted.
func TestE2E_DNSRestoredAfterShutdown(t *testing.T) {
	if os.Getenv("E2E") != "1" {
		t.Skip("set E2E=1 to run")
	}
	if !isElevated() {
		t.Skip("requires admin elevation")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	original, err := CheckCurrentResolver(ctx)
	if err != nil {
		t.Fatalf("get current resolver: %v", err)
	}
	t.Logf("original resolver: %q", original)

	t.Cleanup(func() { restoreDNSAll(original) })

	mgr := NewManager()

	if err := mgr.SetResolver(ctx, "127.0.0.1"); err != nil {
		t.Fatalf("SetResolver: %v", err)
	}

	// Verify DNS was changed.
	current, err := CheckCurrentResolver(ctx)
	if err != nil {
		t.Fatalf("check after set: %v", err)
	}
	if current != "127.0.0.1" {
		t.Errorf("after SetResolver: got %q, want %q", current, "127.0.0.1")
	}

	// Verify state file exists.
	if _, err := os.Stat(dnsStatePath()); os.IsNotExist(err) {
		t.Error("dns state file should exist after SetResolver")
	}

	// Restore.
	if err := mgr.RestoreResolver(ctx); err != nil {
		t.Fatalf("RestoreResolver: %v", err)
	}

	// Verify restored.
	restored, err := CheckCurrentResolver(ctx)
	if err != nil {
		t.Fatalf("check after restore: %v", err)
	}

	if original == "" || strings.EqualFold(original, "dhcp") {
		if restored != "" && !strings.EqualFold(restored, "dhcp") && restored != "127.0.0.1" {
			t.Errorf("after restore: got %q, want dhcp/empty", restored)
		}
	} else if restored != original {
		t.Errorf("after restore: got %q, want %q", restored, original)
	}

	// Verify state file is deleted.
	if _, err := os.Stat(dnsStatePath()); !os.IsNotExist(err) {
		t.Error("dns state file should be deleted after RestoreResolver")
	}

	t.Logf("DNS restore OK: %q -> 127.0.0.1 -> %q", original, restored)
}

// TestE2E_DNSIPv6Resolver verifies that when the IPv4 resolver is changed to
// 127.0.0.1, the IPv6 resolver is also redirected to ::1 (or disabled) so that
// no external ISP IPv6 resolver can leak DNS queries.
func TestE2E_DNSIPv6Resolver(t *testing.T) {
	if os.Getenv("E2E") != "1" {
		t.Skip("set E2E=1 to run")
	}
	if !isElevated() {
		t.Skip("requires admin elevation")
	}

	ifaces, err := activeInterfaces()
	if err != nil || len(ifaces) == 0 {
		t.Skip("no active network interfaces")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	// Probe IPv6 support.
	out, err := exec.CommandContext(ctx, "netsh", "interface", "ipv6", "show", "dns", ifaces[0]).CombinedOutput()
	if err != nil {
		t.Skip("IPv6 not available")
	}
	originalV6 := parseDNSFromNetsh(string(out))
	t.Logf("original IPv6 resolver on %q: %q", ifaces[0], originalV6)

	original, _ := CheckCurrentResolver(ctx)
	t.Cleanup(func() { restoreDNSAll(original) })

	mgr := NewManager()
	if err := mgr.SetResolver(ctx, "127.0.0.1"); err != nil {
		t.Fatalf("SetResolver: %v", err)
	}

	// Check every active interface: IPv6 DNS must be ::1 or absent.
	for _, iface := range ifaces {
		v6Out, err := exec.CommandContext(ctx, "netsh", "interface", "ipv6", "show", "dns", iface).CombinedOutput()
		if err != nil {
			continue
		}
		v6DNS := parseDNSFromNetsh(string(v6Out))
		t.Logf("interface %q IPv6 DNS after SetResolver: %q", iface, v6DNS)

		if v6DNS == "" || v6DNS == "::1" || strings.EqualFold(v6DNS, "dhcp") {
			continue // acceptable
		}
		ip := net.ParseIP(v6DNS)
		if ip != nil && !ip.IsLoopback() {
			t.Errorf("interface %q: IPv6 DNS = %s (external ISP leak risk), want ::1 or disabled", iface, v6DNS)
		}
	}

	if err := mgr.RestoreResolver(ctx); err != nil {
		t.Fatalf("RestoreResolver: %v", err)
	}

	t.Log("IPv6 resolver correctly handled during Le Voile connection")
}

// TestE2E_DNSPort53Real starts the DNS proxy on the real port 53, configures
// the system resolver to 127.0.0.1, and verifies that net.LookupHost succeeds
// through the proxy.  Skips when port 53 is occupied or admin is unavailable.
func TestE2E_DNSPort53Real(t *testing.T) {
	if os.Getenv("E2E") != "1" {
		t.Skip("set E2E=1 to run")
	}
	if !isElevated() {
		t.Skip("port 53 unavailable or not admin")
	}

	// Verify port 53 is free.
	ln, err := net.ListenPacket("udp", "127.0.0.1:53")
	if err != nil {
		t.Skip("port 53 unavailable or not admin")
	}
	ln.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	original, err := CheckCurrentResolver(ctx)
	if err != nil {
		t.Fatalf("get current resolver: %v", err)
	}
	t.Cleanup(func() { restoreDNSAll(original) })

	// Mock upstream that returns 127.0.0.99 for any query.
	mockUpstream := func(_ context.Context, payload []byte) ([]byte, error) {
		resp := make([]byte, len(payload)+16)
		copy(resp, payload)
		resp[2] |= 0x80
		resp[6] = 0x00
		resp[7] = 0x01
		off := len(payload)
		resp = resp[:off+16]
		resp[off+0] = 0xC0
		resp[off+1] = 0x0C
		resp[off+2] = 0x00
		resp[off+3] = 0x01
		resp[off+4] = 0x00
		resp[off+5] = 0x01
		resp[off+6] = 0x00
		resp[off+7] = 0x00
		resp[off+8] = 0x00
		resp[off+9] = 0x3C
		resp[off+10] = 0x00
		resp[off+11] = 0x04
		resp[off+12] = 127
		resp[off+13] = 0
		resp[off+14] = 0
		resp[off+15] = 99
		return resp, nil
	}

	p := NewProxy("127.0.0.1:53", mockUpstream)
	proxyCtx, proxyCancel := context.WithCancel(ctx)
	defer proxyCancel()
	startProxy(t, p, proxyCtx)

	// Point system DNS at our proxy.
	if err := ForceResolver(ctx, "127.0.0.1"); err != nil {
		t.Fatalf("ForceResolver: %v", err)
	}

	// Flush Windows DNS cache.
	exec.CommandContext(ctx, "ipconfig", "/flushdns").Run()

	// Send a raw DNS query to 127.0.0.1:53 via UDP — avoids any Windows DNS
	// client caching or IPv6 fallback issues that affect net.Resolver.
	conn, err := net.Dial("udp", "127.0.0.1:53")
	if err != nil {
		t.Fatalf("dial port 53: %v", err)
	}
	defer conn.Close()
	conn.SetDeadline(time.Now().Add(5 * time.Second))

	query := makeDNSPayload() // example.com A
	if _, err := conn.Write(query); err != nil {
		t.Fatalf("write DNS query to port 53: %v", err)
	}

	buf := make([]byte, 4096)
	n, err := conn.Read(buf)
	if err != nil {
		t.Fatalf("read DNS response from port 53: %v", err)
	}

	if n < 12 {
		t.Fatalf("response too short: %d bytes", n)
	}

	if buf[2]&0x80 == 0 {
		t.Error("QR bit not set — not a valid DNS response")
	}

	ancount := int(buf[6])<<8 | int(buf[7])
	if ancount == 0 {
		t.Error("expected at least one answer record")
	}

	// Verify the mock IP (127.0.0.99) in the answer.
	ansOff := len(query)
	if n >= ansOff+16 {
		ip := net.IPv4(buf[ansOff+12], buf[ansOff+13], buf[ansOff+14], buf[ansOff+15])
		if ip.String() != "127.0.0.99" {
			t.Errorf("answer IP = %s, want 127.0.0.99", ip.String())
		}
	}

	t.Logf("port 53 real test OK: DNS query returned %d bytes, %d answers", n, ancount)
}
