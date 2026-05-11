// Command genregistry generates a signed relay-registry.json file.
//
// Multi-relay mode (recommended):
//
//	genregistry -signing-key master.key -relays relays.json [-strict-priority] [-out registry.json]
//
// Legacy single-relay mode:
//
//	genregistry -signing-key master.key -relay-id relay-fr-01 -relay-domain relay.levoile.dev [-out registry.json]
package main

import (
	"crypto/ed25519"
	"encoding/base64"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/velia-the-veil/le_voile/internal/registry"
)

// relayInput is the schema for each entry in the -relays JSON file.
type relayInput struct {
	ID        string `json:"id"`
	Domain    string `json:"domain"`
	PublicKey string `json:"public_key"` // base64-encoded Ed25519 public key
}

type relayEntry struct {
	ID        string    `json:"id"`
	Domain    string    `json:"domain"`
	PublicKey string    `json:"public_key"`
	Signature string    `json:"signature"`
	Added     time.Time `json:"added"`
}

type registryDoc struct {
	Version         int          `json:"version"`
	MasterPublicKey string       `json:"master_public_key"`
	Relays          []relayEntry `json:"relays"`
	Updated         time.Time    `json:"updated"`
}

// priorityCountries lists the countries that require ≥ 2 relays (FR19b).
var priorityCountries = []string{"de", "es", "gb", "us"}

func main() {
	signingKeyPath := flag.String("signing-key", "", "path to Ed25519 private key file (base64)")
	relaysPath := flag.String("relays", "", "path to JSON file listing relays [{id, domain, public_key}, ...]")
	relayID := flag.String("relay-id", "", "(legacy) single relay ID")
	relayDomain := flag.String("relay-domain", "", "(legacy) single relay domain")
	strictPriority := flag.Bool("strict-priority", false, "fail if any priority country (DE/ES/GB/US) has < 2 relays")
	outPath := flag.String("out", "relay-registry.json", "output file path")
	flag.Parse()

	if *signingKeyPath == "" {
		fmt.Fprintln(os.Stderr, "usage: genregistry -signing-key KEY (-relays FILE | -relay-id ID -relay-domain DOMAIN) [-strict-priority] [-out FILE]")
		os.Exit(1)
	}

	// Load signing key (Ed25519 private key, base64-encoded).
	keyData, err := os.ReadFile(*signingKeyPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "genregistry: read key: %v\n", err)
		os.Exit(1)
	}
	privKey, err := base64.StdEncoding.DecodeString(strings.TrimSpace(string(keyData)))
	if err != nil || len(privKey) != ed25519.PrivateKeySize {
		fmt.Fprintf(os.Stderr, "genregistry: invalid key (got %d bytes, want %d)\n", len(privKey), ed25519.PrivateKeySize)
		os.Exit(1)
	}

	masterPriv := ed25519.PrivateKey(privKey)
	masterPub := masterPriv.Public().(ed25519.PublicKey)
	masterPubB64 := base64.StdEncoding.EncodeToString(masterPub)

	// Build relay list from either -relays file or legacy single-relay flags.
	var inputs []relayInput
	switch {
	case *relaysPath != "":
		data, err := os.ReadFile(*relaysPath)
		if err != nil {
			fmt.Fprintf(os.Stderr, "genregistry: read relays file: %v\n", err)
			os.Exit(1)
		}
		if err := json.Unmarshal(data, &inputs); err != nil {
			fmt.Fprintf(os.Stderr, "genregistry: parse relays file: %v\n", err)
			os.Exit(1)
		}
		if len(inputs) == 0 {
			fmt.Fprintln(os.Stderr, "genregistry: relays file is empty")
			os.Exit(1)
		}
	case *relayID != "" && *relayDomain != "":
		// Legacy single-relay mode: relay key = master key.
		inputs = []relayInput{{
			ID:        *relayID,
			Domain:    *relayDomain,
			PublicKey: masterPubB64,
		}}
	default:
		fmt.Fprintln(os.Stderr, "usage: genregistry -signing-key KEY (-relays FILE | -relay-id ID -relay-domain DOMAIN) [-strict-priority] [-out FILE]")
		os.Exit(1)
	}

	// Validate inputs.
	for i, inp := range inputs {
		if inp.ID == "" || inp.Domain == "" || inp.PublicKey == "" {
			fmt.Fprintf(os.Stderr, "genregistry: relay %d has empty required field (id=%q, domain=%q, public_key=%q)\n", i, inp.ID, inp.Domain, inp.PublicKey)
			os.Exit(1)
		}
	}

	// Priority country check.
	countryCounts := make(map[string]int)
	for _, inp := range inputs {
		code := registry.ExtractCountryCode(inp.ID, inp.Domain)
		if code != "" {
			countryCounts[code]++
		}
	}

	var underserved []string
	for _, c := range priorityCountries {
		if countryCounts[c] < 2 {
			underserved = append(underserved, fmt.Sprintf("%s (%d/2)", c, countryCounts[c]))
		}
	}
	if len(underserved) > 0 {
		msg := fmt.Sprintf("genregistry: priority countries with < 2 relays: %s", strings.Join(underserved, ", "))
		if *strictPriority {
			fmt.Fprintln(os.Stderr, msg)
			os.Exit(1)
		}
		fmt.Fprintf(os.Stderr, "WARNING: %s\n", msg)
	}

	// Sign each relay and build entries.
	now := time.Now().UTC().Truncate(time.Second)
	entries := make([]relayEntry, len(inputs))
	for i, inp := range inputs {
		pubBytes, err := base64.StdEncoding.DecodeString(inp.PublicKey)
		if err != nil {
			fmt.Fprintf(os.Stderr, "genregistry: relay %q: decode public key: %v\n", inp.ID, err)
			os.Exit(1)
		}
		if len(pubBytes) != ed25519.PublicKeySize {
			fmt.Fprintf(os.Stderr, "genregistry: relay %q: public key has %d bytes, want %d\n", inp.ID, len(pubBytes), ed25519.PublicKeySize)
			os.Exit(1)
		}

		msg := append([]byte(registry.SignaturePrefix), pubBytes...)
		sig := ed25519.Sign(masterPriv, msg)

		entries[i] = relayEntry{
			ID:        inp.ID,
			Domain:    inp.Domain,
			PublicKey: inp.PublicKey,
			Signature: base64.StdEncoding.EncodeToString(sig),
			Added:     now,
		}
	}

	reg := registryDoc{
		Version:         1,
		MasterPublicKey: masterPubB64,
		Relays:          entries,
		Updated:         now,
	}

	data, err := json.MarshalIndent(reg, "", "  ")
	if err != nil {
		fmt.Fprintf(os.Stderr, "genregistry: marshal: %v\n", err)
		os.Exit(1)
	}

	// #nosec G306 -- relay-registry.json est distribué publiquement (servi
	// par le master registry sur HTTPS pour tous les clients Le Voile). Son
	// intégrité est garantie par signature Ed25519 embarquée dans le JSON
	// lui-même (champ "signature"). 0644 = standard pour fichier public.
	if err := os.WriteFile(*outPath, data, 0o644); err != nil {
		fmt.Fprintf(os.Stderr, "genregistry: write: %v\n", err)
		os.Exit(1)
	}

	fmt.Fprintf(os.Stderr, "genregistry: wrote %s (%d relays, %d bytes)\n", *outPath, len(entries), len(data))
	for _, e := range entries {
		code := registry.ExtractCountryCode(e.ID, e.Domain)
		fmt.Fprintf(os.Stderr, "  %s (%s) [%s]\n", e.ID, e.Domain, code)
	}
}
