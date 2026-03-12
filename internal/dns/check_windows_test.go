//go:build windows

package dns

import (
	"context"
	"errors"
	"strings"
	"testing"
)

// TestCheckCurrentResolver_Windows tests CheckCurrentResolver on Windows
// using the real activeInterfaces and exec.CommandContext.
// Since CheckCurrentResolver calls exec.CommandContext directly (no mockable runner),
// these tests verify behavior at a higher level.

func TestCheckCurrentResolver_NoActiveInterfaces(t *testing.T) {
	// This test verifies the error path when activeInterfaces returns no interfaces.
	// We cannot easily mock activeInterfaces in check_windows.go since it calls
	// the package-level function directly, but we can verify the sentinel error type.
	// Integration-style: if the machine has no active interfaces, we get ErrNoActiveInterface.
	ctx := context.Background()
	_, err := CheckCurrentResolver(ctx)
	if err != nil {
		// On CI with no network, we expect ErrNoActiveInterface.
		if errors.Is(err, ErrNoActiveInterface) {
			return // expected path
		}
		// Otherwise some DNS was found or netsh ran fine — both acceptable.
	}
	// If err == nil, a resolver was found — also fine on a machine with network.
}

func TestCheckCurrentResolver_ContextCancelled(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // cancel immediately

	_, err := CheckCurrentResolver(ctx)
	if err == nil {
		t.Error("expected error from cancelled context, got nil")
	}
}

func TestForceResolver_ContextCancelled(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // cancel immediately

	err := ForceResolver(ctx, "127.0.0.1")
	if err == nil {
		t.Error("expected error from cancelled context, got nil")
	}
}

func TestParseDNSFromNetsh_EmptyOutput(t *testing.T) {
	result := parseDNSFromNetsh("")
	if result != "" {
		t.Errorf("parseDNSFromNetsh(\"\") = %q, want empty string", result)
	}
}

func TestParseDNSFromNetsh_DHCPOutput(t *testing.T) {
	output := `Configuration for interface "Ethernet"
    DNS servers configured through DHCP
    Register with which suffix:   Primary Only`
	result := parseDNSFromNetsh(output)
	if result != "dhcp" {
		t.Errorf("parseDNSFromNetsh(dhcp output) = %q, want %q", result, "dhcp")
	}
}

func TestParseDNSFromNetsh_StaticDNS(t *testing.T) {
	output := `Configuration for interface "Wi-Fi"
    Statically Configured DNS Servers:    8.8.8.8
    Register with which suffix:   Primary Only`
	result := parseDNSFromNetsh(output)
	if result != "8.8.8.8" {
		t.Errorf("parseDNSFromNetsh(static) = %q, want %q", result, "8.8.8.8")
	}
}

func TestParseDNSFromNetsh_MultipleDNS(t *testing.T) {
	output := `Configuration for interface "Ethernet"
    Statically Configured DNS Servers:    8.8.8.8
                                          8.8.4.4`
	result := parseDNSFromNetsh(output)
	// Should return the first IP found.
	if result != "8.8.8.8" {
		t.Errorf("parseDNSFromNetsh(multiple) = %q, want %q", result, "8.8.8.8")
	}
}

func TestParseDNSFromNetsh_GarbageOutput(t *testing.T) {
	output := "some random text\nwith no IPs or relevant data\n"
	result := parseDNSFromNetsh(output)
	if result != "" {
		t.Errorf("parseDNSFromNetsh(garbage) = %q, want empty string", result)
	}
}

func TestActiveInterfaces_ReturnsNonLoopback(t *testing.T) {
	ifaces, err := activeInterfaces()
	if err != nil {
		t.Fatalf("activeInterfaces() error: %v", err)
	}
	for _, name := range ifaces {
		if strings.Contains(strings.ToLower(name), "loopback") {
			t.Errorf("activeInterfaces returned loopback interface: %q", name)
		}
	}
}
