package main

import (
	"crypto/ed25519"
	"encoding/base64"
	"testing"

	"github.com/velia-the-veil/le_voile/internal/registry"
)

func TestMultiRelaySignAndVerify(t *testing.T) {
	_, priv, err := ed25519.GenerateKey(nil)
	if err != nil {
		t.Fatal(err)
	}
	masterPub := priv.Public().(ed25519.PublicKey)
	masterPubB64 := base64.StdEncoding.EncodeToString(masterPub)

	// Generate 8 relay keys (4 countries × 2).
	countries := []struct{ code, c1id, c1dom, c2id, c2dom string }{
		{"de", "relay-de-001", "de-001.levoile.dev", "relay-de-002", "de-002.levoile.dev"},
		{"es", "relay-es-001", "es-001.levoile.dev", "relay-es-002", "es-002.levoile.dev"},
		{"gb", "relay-gb-001", "gb-001.levoile.dev", "relay-gb-002", "gb-002.levoile.dev"},
		{"us", "relay-us-001", "us-001.levoile.dev", "relay-us-002", "us-002.levoile.dev"},
	}

	var entries []registry.RelayEntry
	for _, c := range countries {
		for _, r := range []struct{ id, dom string }{{c.c1id, c.c1dom}, {c.c2id, c.c2dom}} {
			pub, _, err := ed25519.GenerateKey(nil)
			if err != nil {
				t.Fatal(err)
			}
			pubB64 := base64.StdEncoding.EncodeToString(pub)

			msg := append([]byte(registry.SignaturePrefix), pub...)
			sig := ed25519.Sign(priv, msg)

			entries = append(entries, registry.RelayEntry{
				ID:        r.id,
				Domain:    r.dom,
				PublicKey: pubB64,
				Signature: base64.StdEncoding.EncodeToString(sig),
			})
		}
	}

	// Build registry and verify via VerifyAll (AC2).
	reg := &registry.Registry{
		Version:         1,
		MasterPublicKey: masterPubB64,
		Relays:          entries,
	}

	verified, err := reg.VerifyAll()
	if err != nil {
		t.Fatalf("VerifyAll failed: %v", err)
	}
	if len(verified) != 8 {
		t.Errorf("verified relay count = %d, want 8", len(verified))
	}

	// Verify country extraction works for all entries.
	countryCounts := make(map[string]int)
	for _, e := range verified {
		code := registry.ExtractCountryCode(e.ID, e.Domain)
		if code == "" {
			t.Errorf("ExtractCountryCode(%q, %q) returned empty", e.ID, e.Domain)
		}
		countryCounts[code]++
	}

	for _, c := range countries {
		if countryCounts[c.code] != 2 {
			t.Errorf("country %q: got %d relays, want 2", c.code, countryCounts[c.code])
		}
	}
}

func TestDomainOnlyExtraction(t *testing.T) {
	entries := []registry.RelayEntry{
		{ID: "", Domain: "de-001.levoile.dev"},
		{ID: "", Domain: "de-002.levoile.dev"},
		{ID: "", Domain: "us-001.levoile.dev"},
		{ID: "", Domain: "us-002.levoile.dev"},
	}

	countryCounts := make(map[string]int)
	for _, e := range entries {
		code := registry.ExtractCountryCode(e.ID, e.Domain)
		countryCounts[code]++
	}

	if countryCounts["de"] != 2 {
		t.Errorf("de count = %d, want 2", countryCounts["de"])
	}
	if countryCounts["us"] != 2 {
		t.Errorf("us count = %d, want 2", countryCounts["us"])
	}
}

func TestSignatureDeterminism(t *testing.T) {
	_, priv, _ := ed25519.GenerateKey(nil)
	pub, _, _ := ed25519.GenerateKey(nil)

	msg := append([]byte(registry.SignaturePrefix), pub...)
	sig1 := ed25519.Sign(priv, msg)
	sig2 := ed25519.Sign(priv, msg)

	if base64.StdEncoding.EncodeToString(sig1) != base64.StdEncoding.EncodeToString(sig2) {
		t.Error("Ed25519 signatures are not deterministic")
	}
}

func TestPriorityCountryCheck(t *testing.T) {
	expected := map[string]bool{"de": true, "es": true, "gb": true, "us": true}
	for _, c := range priorityCountries {
		if !expected[c] {
			t.Errorf("unexpected priority country: %q", c)
		}
	}
	if len(priorityCountries) != 4 {
		t.Errorf("priorityCountries length = %d, want 4", len(priorityCountries))
	}
}

// TestStrictPriorityDetectsUnderserved verifies the priority country counting
// logic that drives -strict-priority (AC5).
func TestStrictPriorityDetectsUnderserved(t *testing.T) {
	// Only 1 relay per country → all 4 priority countries underserved.
	inputs := []struct{ id, domain string }{
		{"relay-de-001", "de-001.levoile.dev"},
		{"relay-es-001", "es-001.levoile.dev"},
		{"relay-gb-001", "gb-001.levoile.dev"},
		{"relay-us-001", "us-001.levoile.dev"},
	}

	countryCounts := make(map[string]int)
	for _, inp := range inputs {
		code := registry.ExtractCountryCode(inp.id, inp.domain)
		if code != "" {
			countryCounts[code]++
		}
	}

	var underserved []string
	for _, c := range priorityCountries {
		if countryCounts[c] < 2 {
			underserved = append(underserved, c)
		}
	}

	if len(underserved) != 4 {
		t.Errorf("expected 4 underserved countries, got %d: %v", len(underserved), underserved)
	}

	// Now with 2 relays each → 0 underserved.
	inputs2 := []struct{ id, domain string }{
		{"relay-de-001", "de-001.levoile.dev"}, {"relay-de-002", "de-002.levoile.dev"},
		{"relay-es-001", "es-001.levoile.dev"}, {"relay-es-002", "es-002.levoile.dev"},
		{"relay-gb-001", "gb-001.levoile.dev"}, {"relay-gb-002", "gb-002.levoile.dev"},
		{"relay-us-001", "us-001.levoile.dev"}, {"relay-us-002", "us-002.levoile.dev"},
	}

	countryCounts2 := make(map[string]int)
	for _, inp := range inputs2 {
		code := registry.ExtractCountryCode(inp.id, inp.domain)
		if code != "" {
			countryCounts2[code]++
		}
	}

	var underserved2 []string
	for _, c := range priorityCountries {
		if countryCounts2[c] < 2 {
			underserved2 = append(underserved2, c)
		}
	}

	if len(underserved2) != 0 {
		t.Errorf("expected 0 underserved with full set, got %d: %v", len(underserved2), underserved2)
	}
}
