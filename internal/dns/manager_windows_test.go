//go:build windows

package dns

import (
	"context"
	"errors"
	"strings"
	"testing"
)

// fakeInterfaces returns a fixed list of interface names for testing.
func fakeInterfaces(names ...string) interfaceLister {
	return func() ([]string, error) {
		return names, nil
	}
}

func TestWindowsManager_SetResolver(t *testing.T) {
	var setCalls []string

	runner := func(ctx context.Context, name string, args ...string) ([]byte, error) {
		key := strings.Join(args, " ")
		if strings.Contains(key, "show dns") {
			return []byte("Configuration for interface \"Ethernet\"\n    DNS servers configured through DHCP: 192.168.1.1\n"), nil
		}
		if strings.Contains(key, "set dns") {
			setCalls = append(setCalls, strings.Join(args, " "))
			return []byte("Ok.\n"), nil
		}
		return nil, errors.New("unexpected command")
	}

	mgr := newManagerWithRunner(runner, fakeInterfaces("Ethernet", "Wi-Fi")).(*windowsManager)
	ctx := context.Background()

	if err := mgr.SetResolver(ctx, "127.0.0.1"); err != nil {
		t.Fatalf("SetResolver: %v", err)
	}

	// 2 interfaces × (1 IPv4 set + 1 IPv6 set) = 4 set calls
	if len(setCalls) != 4 {
		t.Fatalf("expected 4 set calls (2 IPv4 + 2 IPv6), got %d: %v", len(setCalls), setCalls)
	}

	var ipv4Calls, ipv6Calls int
	for _, call := range setCalls {
		if strings.Contains(call, "interface ip set dns") {
			ipv4Calls++
			if !strings.Contains(call, "static 127.0.0.1") {
				t.Errorf("IPv4 set call missing static 127.0.0.1: %s", call)
			}
		} else if strings.Contains(call, "interface ipv6 set dns") {
			ipv6Calls++
			if !strings.Contains(call, "static ::1") {
				t.Errorf("IPv6 set call missing static ::1: %s", call)
			}
		}
	}
	if ipv4Calls != 2 {
		t.Errorf("expected 2 IPv4 set calls, got %d", ipv4Calls)
	}
	if ipv6Calls != 2 {
		t.Errorf("expected 2 IPv6 set calls, got %d", ipv6Calls)
	}
}

func TestWindowsManager_SetResolver_Rollback(t *testing.T) {
	ipv4SetCount := 0
	var rollbackCalls []string

	runner := func(ctx context.Context, name string, args ...string) ([]byte, error) {
		key := strings.Join(args, " ")
		if strings.Contains(key, "show dns") {
			return []byte("DNS servers configured through DHCP: 192.168.1.1\n"), nil
		}
		if strings.Contains(key, "set dns") {
			isIPv4 := strings.Contains(key, "interface ip set dns")
			if isIPv4 {
				ipv4SetCount++
				if ipv4SetCount == 2 {
					// Fail on IPv4 set for second interface
					return []byte("Access denied.\n"), errors.New("exit status 1")
				}
			}
			// After the failure, track rollback calls
			// Rollback happens after the 2nd IPv4 set fails, so any subsequent
			// set dns calls are rollback calls.
			if ipv4SetCount >= 2 && !isIPv4 || ipv4SetCount > 2 {
				rollbackCalls = append(rollbackCalls, key)
			}
			return []byte("Ok.\n"), nil
		}
		return nil, errors.New("unexpected")
	}

	mgr := newManagerWithRunner(runner, fakeInterfaces("Ethernet", "Wi-Fi")).(*windowsManager)
	ctx := context.Background()

	err := mgr.SetResolver(ctx, "127.0.0.1")
	if err == nil {
		t.Fatal("expected error on partial failure, got nil")
	}

	if !errors.Is(err, ErrSetResolverFailed) {
		t.Errorf("expected ErrSetResolverFailed, got: %v", err)
	}

	// Rollback restores both IPv4 and IPv6 for the first interface
	if len(rollbackCalls) != 2 {
		t.Errorf("expected 2 rollback calls (1 IPv4 + 1 IPv6), got %d: %v", len(rollbackCalls), rollbackCalls)
	}

	// originalDNS should be nil after rollback
	if mgr.originalDNS != nil {
		t.Error("originalDNS should be nil after failed SetResolver with rollback")
	}
}

func TestWindowsManager_RestoreResolver_NoOp(t *testing.T) {
	mgr := newManagerWithRunner(nil, fakeInterfaces()).(*windowsManager)
	ctx := context.Background()

	if err := mgr.RestoreResolver(ctx); err != nil {
		t.Fatalf("RestoreResolver (no-op): %v", err)
	}
}

func TestWindowsManager_RestoreResolver_DHCP(t *testing.T) {
	var restoreCalls []string

	runner := func(ctx context.Context, name string, args ...string) ([]byte, error) {
		key := strings.Join(args, " ")
		if strings.Contains(key, "set dns") {
			restoreCalls = append(restoreCalls, key)
			return []byte("Ok.\n"), nil
		}
		return []byte(""), nil
	}

	mgr := newManagerWithRunner(runner, fakeInterfaces()).(*windowsManager)
	mgr.originalDNS = map[string]string{
		"Ethernet": "dhcp",
	}
	mgr.originalDNSv6 = map[string]string{
		"Ethernet": "",
	}

	ctx := context.Background()
	if err := mgr.RestoreResolver(ctx); err != nil {
		t.Fatalf("RestoreResolver: %v", err)
	}

	// 1 IPv4 restore + 1 IPv6 restore = 2 calls
	if len(restoreCalls) != 2 {
		t.Fatalf("expected 2 restore calls (1 IPv4 + 1 IPv6), got %d: %v", len(restoreCalls), restoreCalls)
	}

	if !strings.Contains(restoreCalls[0], "dhcp") {
		t.Errorf("expected dhcp restore for IPv4, got: %s", restoreCalls[0])
	}
}

func TestWindowsManager_RestoreResolver_Static(t *testing.T) {
	var restoreCalls []string

	runner := func(ctx context.Context, name string, args ...string) ([]byte, error) {
		key := strings.Join(args, " ")
		if strings.Contains(key, "set dns") {
			restoreCalls = append(restoreCalls, key)
			return []byte("Ok.\n"), nil
		}
		return []byte(""), nil
	}

	mgr := newManagerWithRunner(runner, fakeInterfaces()).(*windowsManager)
	mgr.originalDNS = map[string]string{
		"Ethernet": "8.8.8.8",
	}
	mgr.originalDNSv6 = map[string]string{
		"Ethernet": "",
	}

	ctx := context.Background()
	if err := mgr.RestoreResolver(ctx); err != nil {
		t.Fatalf("RestoreResolver: %v", err)
	}

	// 1 IPv4 restore + 1 IPv6 restore = 2 calls
	if len(restoreCalls) != 2 {
		t.Fatalf("expected 2 restore calls (1 IPv4 + 1 IPv6), got %d: %v", len(restoreCalls), restoreCalls)
	}

	if !strings.Contains(restoreCalls[0], "static 8.8.8.8") {
		t.Errorf("expected static 8.8.8.8, got: %s", restoreCalls[0])
	}
}

func TestWindowsManager_OriginalResolver(t *testing.T) {
	mgr := newManagerWithRunner(nil, fakeInterfaces()).(*windowsManager)

	if got := mgr.OriginalResolver(); got != "" {
		t.Errorf("OriginalResolver before set = %q, want empty", got)
	}

	mgr.originalDNS = map[string]string{
		"Ethernet": "8.8.8.8",
	}

	if got := mgr.OriginalResolver(); got != "8.8.8.8" {
		t.Errorf("OriginalResolver = %q, want %q", got, "8.8.8.8")
	}
}

func TestWindowsManager_SetResolverError(t *testing.T) {
	runner := func(ctx context.Context, name string, args ...string) ([]byte, error) {
		key := strings.Join(args, " ")
		if strings.Contains(key, "show dns") {
			return []byte("DHCP configured\n"), nil
		}
		if strings.Contains(key, "set dns") {
			return []byte("The requested operation requires elevation.\n"), errors.New("exit status 1")
		}
		return nil, errors.New("unexpected")
	}

	mgr := newManagerWithRunner(runner, fakeInterfaces("Ethernet")).(*windowsManager)
	ctx := context.Background()

	err := mgr.SetResolver(ctx, "127.0.0.1")
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !errors.Is(err, ErrSetResolverFailed) {
		t.Errorf("expected ErrSetResolverFailed, got: %v", err)
	}
}

func TestWindowsManager_NoActiveInterfaces(t *testing.T) {
	mgr := newManagerWithRunner(nil, fakeInterfaces()).(*windowsManager)
	ctx := context.Background()

	err := mgr.SetResolver(ctx, "127.0.0.1")
	if !errors.Is(err, ErrNoActiveInterface) {
		t.Errorf("expected ErrNoActiveInterface, got: %v", err)
	}
}

func TestParseDNSFromNetsh(t *testing.T) {
	tests := []struct {
		name   string
		input  string
		expect string
	}{
		{
			name: "static_dns",
			input: `Configuration for interface "Ethernet"
    Statically Configured DNS Servers:    8.8.8.8
`,
			expect: "8.8.8.8",
		},
		{
			name: "dhcp_dns",
			input: `Configuration for interface "Ethernet"
    DNS servers configured through DHCP:  192.168.1.1
`,
			expect: "192.168.1.1",
		},
		{
			name: "dhcp_only",
			input: `Configuration for interface "Ethernet"
    DNS servers configured through DHCP
    Register with which suffix:           Primary Only
`,
			expect: "dhcp",
		},
		{
			name:   "empty",
			input:  "",
			expect: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := parseDNSFromNetsh(tt.input)
			if got != tt.expect {
				t.Errorf("parseDNSFromNetsh = %q, want %q", got, tt.expect)
			}
		})
	}
}
