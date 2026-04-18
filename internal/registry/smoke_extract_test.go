package registry

import (
	"fmt"
	"testing"
)

// TestProductionRelayShape_SmokeExtract validates ExtractCountryCode + CountryMetaMap
// against the ACTUAL production registry entries observed on
// https://relay.levoile.dev/.well-known/relay-registry.json (2026-04-17).
// Guards Story 5.2 against the 3-digit suffix used in production.
func TestProductionRelayShape_SmokeExtract(t *testing.T) {
	cases := []struct {
		id, domain  string
		wantCode    string
		wantName    string
		wantFlag    string
	}{
		{"relay-de-001", "de-001.levoile.dev", "de", "Allemagne", "🇩🇪"},
		{"relay-de-002", "de-002.levoile.dev", "de", "Allemagne", "🇩🇪"},
		{"relay-us-001", "us-001.levoile.dev", "us", "États-Unis", "🇺🇸"},
		{"relay-us-002", "us-002.levoile.dev", "us", "États-Unis", "🇺🇸"},
		{"relay-es-001", "es-001.levoile.dev", "es", "Espagne", "🇪🇸"},
		{"relay-es-002", "es-002.levoile.dev", "es", "Espagne", "🇪🇸"},
		{"relay-gb-001", "gb-001.levoile.dev", "gb", "Royaume-Uni", "🇬🇧"},
		{"relay-gb-002", "gb-002.levoile.dev", "gb", "Royaume-Uni", "🇬🇧"},
	}
	for _, c := range cases {
		t.Run(fmt.Sprintf("%s/%s", c.id, c.domain), func(t *testing.T) {
			code := ExtractCountryCode(c.id, c.domain)
			if code != c.wantCode {
				t.Errorf("ExtractCountryCode = %q, want %q", code, c.wantCode)
			}
			meta, ok := CountryMetaMap[code]
			if !ok {
				t.Fatalf("CountryMetaMap missing code %q", code)
			}
			if meta.Name != c.wantName {
				t.Errorf("Name = %q, want %q", meta.Name, c.wantName)
			}
			if meta.Flag != c.wantFlag {
				t.Errorf("Flag = %q, want %q", meta.Flag, c.wantFlag)
			}
		})
	}
}
