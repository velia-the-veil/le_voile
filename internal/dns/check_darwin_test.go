//go:build darwin

package dns

import (
	"context"
	"testing"
)

func TestCheckCurrentResolver_Darwin_ContextCancelled(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := CheckCurrentResolver(ctx)
	if err == nil {
		t.Error("expected error from cancelled context, got nil")
	}
}

func TestForceResolver_Darwin_ContextCancelled(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	err := ForceResolver(ctx, "127.0.0.1")
	if err == nil {
		t.Error("expected error from cancelled context, got nil")
	}
}

func TestParseNetworkServices(t *testing.T) {
	tests := []struct {
		name   string
		output string
		want   []string
	}{
		{
			name:   "typical output",
			output: "An asterisk (*) denotes that a network service is disabled.\nWi-Fi\nEthernet\nThunderbolt Bridge\n",
			want:   []string{"Wi-Fi", "Ethernet", "Thunderbolt Bridge"},
		},
		{
			name:   "with disabled service",
			output: "An asterisk (*) denotes that a network service is disabled.\n*Bluetooth PAN\nWi-Fi\n",
			want:   []string{"Wi-Fi"},
		},
		{
			name:   "empty output",
			output: "",
			want:   nil,
		},
		{
			name:   "only header",
			output: "An asterisk (*) denotes that a network service is disabled.\n",
			want:   nil,
		},
		{
			name:   "no services",
			output: "An asterisk (*) denotes that a network service is disabled.\n*All Disabled\n",
			want:   nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := parseNetworkServices(tt.output)
			if len(got) != len(tt.want) {
				t.Fatalf("parseNetworkServices() = %v (len %d), want %v (len %d)",
					got, len(got), tt.want, len(tt.want))
			}
			for i := range got {
				if got[i] != tt.want[i] {
					t.Errorf("parseNetworkServices()[%d] = %q, want %q", i, got[i], tt.want[i])
				}
			}
		})
	}
}

func TestParseDNSServers(t *testing.T) {
	tests := []struct {
		name   string
		output string
		want   []string
	}{
		{
			name:   "single server",
			output: "8.8.8.8\n",
			want:   []string{"8.8.8.8"},
		},
		{
			name:   "multiple servers",
			output: "8.8.8.8\n8.8.4.4\n",
			want:   []string{"8.8.8.8", "8.8.4.4"},
		},
		{
			name:   "dhcp no servers",
			output: "There aren't any DNS Servers set on Wi-Fi.\n",
			want:   nil,
		},
		{
			name:   "empty output",
			output: "",
			want:   nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := parseDNSServers(tt.output)
			if len(got) != len(tt.want) {
				t.Fatalf("parseDNSServers() = %v (len %d), want %v (len %d)",
					got, len(got), tt.want, len(tt.want))
			}
			for i := range got {
				if got[i] != tt.want[i] {
					t.Errorf("parseDNSServers()[%d] = %q, want %q", i, got[i], tt.want[i])
				}
			}
		})
	}
}
