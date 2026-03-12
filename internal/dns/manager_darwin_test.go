//go:build darwin

package dns

import (
	"context"
	"errors"
	"strings"
	"testing"
)

func TestDarwinManager_SetResolver(t *testing.T) {
	var setCalls []string

	runner := func(ctx context.Context, name string, args ...string) ([]byte, error) {
		key := name + " " + strings.Join(args, " ")
		if strings.Contains(key, "-listallnetworkservices") {
			return []byte("An asterisk (*) denotes that a network service is disabled.\nWi-Fi\nEthernet\n"), nil
		}
		if strings.Contains(key, "-getdnsservers") {
			return []byte("192.168.1.1\n"), nil
		}
		if strings.Contains(key, "-setdnsservers") {
			setCalls = append(setCalls, key)
			return []byte(""), nil
		}
		return nil, errors.New("unexpected command: " + key)
	}

	mgr := newManagerWithRunner(runner).(*darwinManager)
	ctx := context.Background()

	if err := mgr.SetResolver(ctx, "127.0.0.1"); err != nil {
		t.Fatalf("SetResolver: %v", err)
	}

	if len(setCalls) != 2 {
		t.Fatalf("expected 2 set calls (Wi-Fi + Ethernet), got %d", len(setCalls))
	}

	for _, call := range setCalls {
		if !strings.Contains(call, "127.0.0.1") {
			t.Errorf("set call missing 127.0.0.1: %s", call)
		}
	}
}

func TestDarwinManager_RestoreResolver_DHCP(t *testing.T) {
	var restoreCalls []string

	runner := func(ctx context.Context, name string, args ...string) ([]byte, error) {
		key := name + " " + strings.Join(args, " ")
		if strings.Contains(key, "-setdnsservers") {
			restoreCalls = append(restoreCalls, key)
			return []byte(""), nil
		}
		return nil, errors.New("unexpected")
	}

	mgr := newManagerWithRunner(runner).(*darwinManager)
	mgr.originalDNS = map[string][]string{
		"Wi-Fi": {}, // DHCP — no DNS was set
	}

	ctx := context.Background()
	if err := mgr.RestoreResolver(ctx); err != nil {
		t.Fatalf("RestoreResolver: %v", err)
	}

	if len(restoreCalls) != 1 {
		t.Fatalf("expected 1 restore call, got %d", len(restoreCalls))
	}

	if !strings.Contains(restoreCalls[0], "Empty") {
		t.Errorf("expected Empty for DHCP restore, got: %s", restoreCalls[0])
	}
}

func TestDarwinManager_RestoreResolver_Static(t *testing.T) {
	var restoreCalls []string

	runner := func(ctx context.Context, name string, args ...string) ([]byte, error) {
		key := name + " " + strings.Join(args, " ")
		if strings.Contains(key, "-setdnsservers") {
			restoreCalls = append(restoreCalls, key)
			return []byte(""), nil
		}
		return nil, errors.New("unexpected")
	}

	mgr := newManagerWithRunner(runner).(*darwinManager)
	mgr.originalDNS = map[string][]string{
		"Wi-Fi": {"8.8.8.8", "8.8.4.4"},
	}

	ctx := context.Background()
	if err := mgr.RestoreResolver(ctx); err != nil {
		t.Fatalf("RestoreResolver: %v", err)
	}

	if len(restoreCalls) != 1 {
		t.Fatalf("expected 1 restore call, got %d", len(restoreCalls))
	}

	if !strings.Contains(restoreCalls[0], "8.8.8.8") {
		t.Errorf("expected 8.8.8.8 in restore, got: %s", restoreCalls[0])
	}
}

func TestDarwinManager_RestoreResolver_NoOp(t *testing.T) {
	mgr := newManagerWithRunner(nil).(*darwinManager)
	ctx := context.Background()

	if err := mgr.RestoreResolver(ctx); err != nil {
		t.Fatalf("RestoreResolver (no-op): %v", err)
	}
}

func TestDarwinManager_OriginalResolver(t *testing.T) {
	mgr := newManagerWithRunner(nil).(*darwinManager)

	if got := mgr.OriginalResolver(); got != "" {
		t.Errorf("OriginalResolver before set = %q, want empty", got)
	}

	mgr.originalDNS = map[string][]string{
		"Wi-Fi": {"8.8.8.8"},
	}

	if got := mgr.OriginalResolver(); got != "8.8.8.8" {
		t.Errorf("OriginalResolver = %q, want %q", got, "8.8.8.8")
	}
}

func TestParseNetworkServices(t *testing.T) {
	tests := []struct {
		name   string
		input  string
		expect []string
	}{
		{
			name:   "standard",
			input:  "An asterisk (*) denotes that a network service is disabled.\nWi-Fi\nEthernet\n",
			expect: []string{"Wi-Fi", "Ethernet"},
		},
		{
			name:   "with_disabled",
			input:  "An asterisk (*) denotes that a network service is disabled.\n*Bluetooth PAN\nWi-Fi\n",
			expect: []string{"Wi-Fi"},
		},
		{
			name:   "empty",
			input:  "",
			expect: nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := parseNetworkServices(tt.input)
			if len(got) != len(tt.expect) {
				t.Errorf("parseNetworkServices got %d services, want %d", len(got), len(tt.expect))
				return
			}
			for i := range got {
				if got[i] != tt.expect[i] {
					t.Errorf("service[%d] = %q, want %q", i, got[i], tt.expect[i])
				}
			}
		})
	}
}

func TestParseDNSServers(t *testing.T) {
	tests := []struct {
		name   string
		input  string
		expect []string
	}{
		{
			name:   "static",
			input:  "8.8.8.8\n8.8.4.4\n",
			expect: []string{"8.8.8.8", "8.8.4.4"},
		},
		{
			name:   "dhcp",
			input:  "There aren't any DNS Servers set on Wi-Fi.\n",
			expect: nil,
		},
		{
			name:   "single",
			input:  "192.168.1.1\n",
			expect: []string{"192.168.1.1"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := parseDNSServers(tt.input)
			if len(got) != len(tt.expect) {
				t.Errorf("parseDNSServers got %d, want %d", len(got), len(tt.expect))
				return
			}
			for i := range got {
				if got[i] != tt.expect[i] {
					t.Errorf("server[%d] = %q, want %q", i, got[i], tt.expect[i])
				}
			}
		})
	}
}
