// Command genregistry generates a signed relay-registry.json file.
// Usage: genregistry -signing-key /path/to/key -relay-id relay-fr-01 -relay-domain relay.levoile.dev -out /path/to/relay-registry.json
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
)

type relayEntry struct {
	ID        string    `json:"id"`
	Domain    string    `json:"domain"`
	PublicKey string    `json:"public_key"`
	Signature string    `json:"signature"`
	Added     time.Time `json:"added"`
}

type registry struct {
	Version         int          `json:"version"`
	MasterPublicKey string       `json:"master_public_key"`
	Relays          []relayEntry `json:"relays"`
	Updated         time.Time    `json:"updated"`
}

func main() {
	signingKeyPath := flag.String("signing-key", "", "path to Ed25519 private key file (base64)")
	relayID := flag.String("relay-id", "", "relay ID (e.g., relay-fr-01)")
	relayDomain := flag.String("relay-domain", "", "relay domain (e.g., relay.levoile.dev)")
	outPath := flag.String("out", "relay-registry.json", "output file path")
	flag.Parse()

	if *signingKeyPath == "" || *relayID == "" || *relayDomain == "" {
		fmt.Fprintln(os.Stderr, "usage: genregistry -signing-key KEY -relay-id ID -relay-domain DOMAIN [-out FILE]")
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

	// The relay's public key is the same as the master key (self-signed for single-relay setup).
	relayPubB64 := masterPubB64
	relayPubBytes := []byte(masterPub)

	// Sign: ed25519.Sign(masterPrivKey, "relay-key-v1:" + relayPubKeyBytes)
	msg := append([]byte("relay-key-v1:"), relayPubBytes...)
	sig := ed25519.Sign(masterPriv, msg)
	sigB64 := base64.StdEncoding.EncodeToString(sig)

	now := time.Now().UTC().Truncate(time.Second)
	reg := registry{
		Version:         1,
		MasterPublicKey: masterPubB64,
		Relays: []relayEntry{
			{
				ID:        *relayID,
				Domain:    *relayDomain,
				PublicKey: relayPubB64,
				Signature: sigB64,
				Added:     now,
			},
		},
		Updated: now,
	}

	data, err := json.MarshalIndent(reg, "", "  ")
	if err != nil {
		fmt.Fprintf(os.Stderr, "genregistry: marshal: %v\n", err)
		os.Exit(1)
	}

	if err := os.WriteFile(*outPath, data, 0o644); err != nil {
		fmt.Fprintf(os.Stderr, "genregistry: write: %v\n", err)
		os.Exit(1)
	}

	fmt.Fprintf(os.Stderr, "genregistry: wrote %s (%d bytes)\n", *outPath, len(data))
	fmt.Fprintf(os.Stderr, "  master_public_key: %s\n", masterPubB64)
	fmt.Fprintf(os.Stderr, "  relay: %s (%s)\n", *relayID, *relayDomain)
}
