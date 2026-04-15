package tun

import (
	"strings"
	"testing"
)

func TestValidateParams(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name    string
		ifname  string
		mtu     int
		wantErr string
	}{
		{"defaults", DefaultName, DefaultMTU, ""},
		{"min mtu", "levoile0", MinMTU, ""},
		{"max mtu", "levoile0", MaxMTU, ""},
		{"short name", "a", 1500, ""},
		{"mtu too low", "levoile0", MinMTU - 1, "hors bornes"},
		{"mtu too high", "levoile0", MaxMTU + 1, "hors bornes"},
		{"empty name", "", 1500, "nom invalide"},
		{"uppercase name", "LeVoile0", 1500, "nom invalide"},
		{"name too long", "abcdefghijklmnop", 1500, "nom invalide"}, // 16 chars
		{"starts with digit", "0voile", 1500, "nom invalide"},
		{"hyphen", "le-voile", 1500, "nom invalide"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := validateParams(tc.ifname, tc.mtu)
			if tc.wantErr == "" {
				if err != nil {
					t.Fatalf("unexpected error: %v", err)
				}
				return
			}
			if err == nil {
				t.Fatalf("expected error containing %q, got nil", tc.wantErr)
			}
			if !strings.Contains(err.Error(), tc.wantErr) {
				t.Fatalf("error %q should contain %q", err, tc.wantErr)
			}
		})
	}
}

// TestConstants vérifie que les constantes publiques restent stables (ces
// valeurs sont citées dans architecture.md et config.example.toml).
func TestConstants(t *testing.T) {
	t.Parallel()
	if DefaultName != "levoile0" {
		t.Errorf("DefaultName = %q, want levoile0", DefaultName)
	}
	if DefaultMTU != 1420 {
		t.Errorf("DefaultMTU = %d, want 1420", DefaultMTU)
	}
}
