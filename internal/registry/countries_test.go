package registry

import "testing"

func TestExtractCountryCode(t *testing.T) {
	tests := []struct {
		name   string
		id     string
		domain string
		want   string
	}{
		{"ID format relay-is-01", "relay-is-01", "is.levoile.dev", "is"},
		{"ID format relay-de-02", "relay-de-02", "de.levoile.dev", "de"},
		{"ID format relay-fi-01", "relay-fi-01", "", "fi"},
		{"ID format relay-us-03", "relay-us-03", "", "us"},
		{"ID format relay-fr-01", "relay-fr-01", "", "fr"},
		{"domain format is.levoile.dev", "", "is.levoile.dev", "is"},
		{"domain format de.levoile.dev", "", "de.levoile.dev", "de"},
		{"legacy relay-iceland", "relay-iceland-01", "relay-iceland.levoile.dev", "is"},
		{"legacy relay-finland", "", "relay-finland.levoile.dev", "fi"},
		{"legacy relay-germany", "", "relay-germany.example.com", "de"},
		{"legacy relay-france", "", "relay-france.levoile.dev", "fr"},
		{"legacy relay-usa", "", "relay-usa.levoile.dev", "us"},
		{"unknown domain", "", "custom.example.com", ""},
		{"empty both", "", "", ""},
		{"short domain", "", "x.y", ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ExtractCountryCode(tt.id, tt.domain)
			if got != tt.want {
				t.Errorf("ExtractCountryCode(%q, %q) = %q, want %q", tt.id, tt.domain, got, tt.want)
			}
		})
	}
}

func TestRelaysByCountry(t *testing.T) {
	relays := []RelayEntry{
		{ID: "relay-is-01", Domain: "is.levoile.dev"},
		{ID: "relay-is-02", Domain: "is2.levoile.dev"},
		{ID: "relay-de-01", Domain: "de.levoile.dev"},
		{ID: "relay-fi-01", Domain: "fi.levoile.dev"},
	}

	d := &Discoverer{}
	d.setRelays(relays)

	byCountry := d.RelaysByCountry()

	if len(byCountry["is"]) != 2 {
		t.Errorf("Iceland relay count = %d, want 2", len(byCountry["is"]))
	}
	if len(byCountry["de"]) != 1 {
		t.Errorf("Germany relay count = %d, want 1", len(byCountry["de"]))
	}
	if len(byCountry["fi"]) != 1 {
		t.Errorf("Finland relay count = %d, want 1", len(byCountry["fi"]))
	}
}

func TestCountryMetaMap_AllCountries(t *testing.T) {
	expected := []string{"is", "de", "fi", "us", "fr"}
	for _, code := range expected {
		meta, ok := CountryMetaMap[code]
		if !ok {
			t.Errorf("CountryMetaMap missing code %q", code)
			continue
		}
		if meta.Name == "" {
			t.Errorf("CountryMetaMap[%q].Name is empty", code)
		}
		if meta.Flag == "" {
			t.Errorf("CountryMetaMap[%q].Flag is empty", code)
		}
	}
}
