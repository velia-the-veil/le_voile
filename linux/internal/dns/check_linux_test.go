//go:build linux

package dns

import (
	"context"
	"testing"
)

func TestCheckCurrentResolver_Linux_ContextCancelled(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := CheckCurrentResolver(ctx)
	if err == nil {
		t.Error("expected error from cancelled context, got nil")
	}
}

func TestForceResolver_Linux_ContextCancelled(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	err := ForceResolver(ctx, "127.0.0.1")
	if err == nil {
		t.Error("expected error from cancelled context, got nil")
	}
}

func TestDiscoverInterfaces_FallbackToEth0(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // cancelled context will cause resolvectl to fail

	interfaces := discoverInterfaces(ctx)
	if len(interfaces) == 0 {
		t.Fatal("discoverInterfaces should never return empty slice")
	}
	// When resolvectl fails, it falls back to ["eth0"].
	// On a real system with resolvectl, it may return real interfaces.
	// Either way, the slice should be non-empty.
}

func TestIsIPAddr_Valid(t *testing.T) {
	tests := []struct {
		input string
		want  bool
	}{
		// IPv4
		{"192.168.1.1", true},
		{"8.8.8.8", true},
		{"0.0.0.0", true},
		{"255.255.255.255", true},
		{"127.0.0.1", true},
		{"1.2.3.4", true},
		// IPv6 — isIPAddr is family-agnostic, DNS over IPv6 is valid.
		{"::1", true},
		{"2001:db8::1", true},
	}
	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			if got := isIPAddr(tt.input); got != tt.want {
				t.Errorf("isIPAddr(%q) = %v, want %v", tt.input, got, tt.want)
			}
		})
	}
}

func TestIsIPAddr_Invalid(t *testing.T) {
	tests := []struct {
		input string
	}{
		{""},
		{"not-an-ip"},
		{"1.2.3"},
		{"1.2.3.4.5"},
		{"a.b.c.d"},
		{"256.1.1.1"},
		{"1.2.3."},
		{".1.2.3"},
		{"1..2.3"},
		{"1.2.3.4a"},
		{"1234.1.1.1"},
		{"1.2.3.-1"},
	}
	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			if got := isIPAddr(tt.input); got {
				t.Errorf("isIPAddr(%q) = true, want false", tt.input)
			}
		})
	}
}

func TestParseAllResolvectlInterfaces(t *testing.T) {
	tests := []struct {
		name   string
		output string
		want   []string
	}{
		{
			name:   "single interface",
			output: "Link 2 (eth0): 192.168.1.1",
			want:   []string{"eth0"},
		},
		{
			name:   "multiple interfaces",
			output: "Link 2 (eth0): 192.168.1.1\nLink 3 (wlan0): 8.8.8.8",
			want:   []string{"eth0", "wlan0"},
		},
		{
			name:   "empty output",
			output: "",
			want:   nil,
		},
		{
			name:   "no link lines",
			output: "Global: 8.8.8.8\n",
			want:   nil,
		},
		{
			name:   "link without parentheses",
			output: "Link 2 eth0: 192.168.1.1",
			want:   nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := parseAllResolvectlInterfaces(tt.output)
			if len(got) != len(tt.want) {
				t.Fatalf("parseAllResolvectlInterfaces() = %v (len %d), want %v (len %d)",
					got, len(got), tt.want, len(tt.want))
			}
			for i := range got {
				if got[i] != tt.want[i] {
					t.Errorf("parseAllResolvectlInterfaces()[%d] = %q, want %q", i, got[i], tt.want[i])
				}
			}
		})
	}
}
