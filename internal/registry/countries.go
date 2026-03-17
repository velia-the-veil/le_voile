package registry

import (
	"sort"
	"strings"
)

// CountryMeta holds display metadata for a relay country.
type CountryMeta struct {
	Name string // French name: "Islande"
	Flag string // Emoji flag: "🇮🇸"
}

// CountryMetaMap maps ISO 2-letter country codes to display metadata.
var CountryMetaMap = map[string]CountryMeta{
	"is": {Name: "Islande", Flag: "🇮🇸"},
	"de": {Name: "Allemagne", Flag: "🇩🇪"},
	"fi": {Name: "Finlande", Flag: "🇫🇮"},
	"us": {Name: "États-Unis", Flag: "🇺🇸"},
	"fr": {Name: "France", Flag: "🇫🇷"},
}

// ExtractCountryCode extracts the ISO 2-letter country code from a relay ID
// or domain. It tries the ID first (relay-{code}-{num}), then the domain
// ({code}.levoile.dev), and finally falls back to legacy prefixes.
func ExtractCountryCode(id, domain string) string {
	// Try ID format: relay-{code}-{num} (e.g., "relay-is-01")
	if strings.HasPrefix(id, "relay-") {
		rest := id[len("relay-"):]
		if idx := strings.Index(rest, "-"); idx >= 2 {
			code := rest[:idx]
			if _, ok := CountryMetaMap[code]; ok {
				return code
			}
		}
	}

	// Try domain format: {code}.levoile.dev (e.g., "is.levoile.dev")
	if dot := strings.Index(domain, "."); dot >= 2 {
		code := domain[:dot]
		if _, ok := CountryMetaMap[code]; ok {
			return code
		}
	}

	// Legacy fallback: domain prefixes like "relay-iceland.levoile.dev"
	legacyMap := map[string]string{
		"relay-iceland": "is",
		"relay-finland": "fi",
		"relay-germany": "de",
		"relay-france":  "fr",
		"relay-usa":     "us",
	}
	for prefix, code := range legacyMap {
		if strings.HasPrefix(domain, prefix) || strings.HasPrefix(id, prefix) {
			return code
		}
	}

	return ""
}

// RelaysByCountry returns the current relays grouped by ISO country code.
// Within each country, relays preserve the latency-sorted order from d.Relays().
// An explicit index-based sort ensures the order survives Go map iteration.
func (d *Discoverer) RelaysByCountry() map[string][]RelayEntry {
	relays := d.Relays() // globally sorted by latency
	// Track original order index for stable intra-country sorting.
	type indexed struct {
		entry RelayEntry
		order int
	}
	groups := make(map[string][]indexed)
	for i, r := range relays {
		code := ExtractCountryCode(r.ID, r.Domain)
		if code == "" {
			code = "unknown"
		}
		groups[code] = append(groups[code], indexed{entry: r, order: i})
	}
	result := make(map[string][]RelayEntry, len(groups))
	for code, items := range groups {
		sort.Slice(items, func(a, b int) bool {
			return items[a].order < items[b].order
		})
		entries := make([]RelayEntry, len(items))
		for i, it := range items {
			entries[i] = it.entry
		}
		result[code] = entries
	}
	return result
}
