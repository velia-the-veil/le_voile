// Build script Story 11.7-bis : génère des fixtures dev pour le bundle APK.
//
// Génère une paire Ed25519 (master) et signe un registre fixture avec 4 relais
// MVP (DE/ES/GB/US). Écrit 2 fichiers dans res/raw/ :
//   - registry_master_pubkey         (base64 standard de la pubkey, 1 ligne)
//   - registry_bootstrap_relays      (registry JSON signed)
//
// **PLACEHOLDERS DEV** — à remplacer en release par les vraies clés publiques
// signées par le master signing process du projet (workflow distinct, hors
// scope Story 11.7-bis).
//
// Usage : `go run android/scripts/gen-registry-fixtures.go`
//
// build constraint pour exclure du build standard :
//go:build ignore

package main

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
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

const signaturePrefix = "relay-key-v1:"

// 4 pays MVP × 2 relais (cohérent Story 3.8 distribution relais).
var bootstrap = []struct {
	id     string
	domain string
}{
	{"relay-de-001", "de-001.relay.levoile.dev"},
	{"relay-de-002", "de-002.relay.levoile.dev"},
	{"relay-es-001", "es-001.relay.levoile.dev"},
	{"relay-es-002", "es-002.relay.levoile.dev"},
	{"relay-gb-001", "gb-001.relay.levoile.dev"},
	{"relay-gb-002", "gb-002.relay.levoile.dev"},
	{"relay-us-001", "us-001.relay.levoile.dev"},
	{"relay-us-002", "us-002.relay.levoile.dev"},
}

func main() {
	rawDir := filepath.Join("android", "app", "src", "main", "res", "raw")
	if err := os.MkdirAll(rawDir, 0o755); err != nil {
		fmt.Fprintf(os.Stderr, "MkdirAll %s: %v\n", rawDir, err)
		os.Exit(1)
	}

	// 1. Master keypair.
	masterPub, masterPriv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		fmt.Fprintf(os.Stderr, "GenerateKey master: %v\n", err)
		os.Exit(1)
	}
	masterPubB64 := base64.StdEncoding.EncodeToString(masterPub)

	// 2. Sign each relay key.
	now := time.Now().UTC()
	relays := make([]relayEntry, 0, len(bootstrap))
	for _, b := range bootstrap {
		relayPub, _, err := ed25519.GenerateKey(rand.Reader)
		if err != nil {
			fmt.Fprintf(os.Stderr, "GenerateKey relay %s: %v\n", b.id, err)
			os.Exit(1)
		}
		msg := append([]byte(signaturePrefix), relayPub...)
		sig := ed25519.Sign(masterPriv, msg)
		relays = append(relays, relayEntry{
			ID:        b.id,
			Domain:    b.domain,
			PublicKey: base64.StdEncoding.EncodeToString(relayPub),
			Signature: base64.StdEncoding.EncodeToString(sig),
			Added:     now,
		})
	}

	// 3. Build registry.
	reg := registry{
		Version:         1,
		MasterPublicKey: masterPubB64,
		Relays:          relays,
		Updated:         now,
	}
	regJSON, err := json.MarshalIndent(reg, "", "  ")
	if err != nil {
		fmt.Fprintf(os.Stderr, "Marshal registry: %v\n", err)
		os.Exit(1)
	}

	// 4. Écrire les fichiers res/raw/.
	pubKeyPath := filepath.Join(rawDir, "registry_master_pubkey")
	if err := os.WriteFile(pubKeyPath, []byte(masterPubB64+"\n"), 0o644); err != nil {
		fmt.Fprintf(os.Stderr, "WriteFile %s: %v\n", pubKeyPath, err)
		os.Exit(1)
	}
	regPath := filepath.Join(rawDir, "registry_bootstrap_relays")
	if err := os.WriteFile(regPath, regJSON, 0o644); err != nil {
		fmt.Fprintf(os.Stderr, "WriteFile %s: %v\n", regPath, err)
		os.Exit(1)
	}

	fmt.Printf("✓ Master pubkey      : %s (%d bytes)\n", pubKeyPath, len(masterPubB64))
	fmt.Printf("✓ Bootstrap registry : %s (%d bytes, %d relais)\n", regPath, len(regJSON), len(relays))
	fmt.Printf("\n⚠️  PLACEHOLDERS DEV — en release, remplacer par les vraies fixtures\n")
	fmt.Printf("    signées par le master signing process du projet.\n")

	// Code-review post-11.7-bis (L-5) : privkey master sur STDERR + avertissement
	// renforcé pour éviter que `go run ... > log.txt` la capture dans un fichier
	// persisté. Si stderr est aussi redirigé (`go run ... &> all.log`) le risque
	// reste — d'où la recommandation explicite ci-dessous.
	//
	// Si tu n'as pas besoin de la privkey (test local pré-existant), tu peux
	// ignorer cette ligne en pipant 2>/dev/null.
	fmt.Fprintf(os.Stderr, "\n⚠️  ATTENTION — privkey master générée localement (USAGE DEV UNIQUEMENT) :\n")
	fmt.Fprintf(os.Stderr, "    %s\n", base64.StdEncoding.EncodeToString(masterPriv))
	fmt.Fprintf(os.Stderr, "    NE PAS commiter, NE PAS publier, NE PAS persister dans un log CI.\n")
	fmt.Fprintf(os.Stderr, "    Pour silencer cette ligne : `go run ... 2>/dev/null`.\n")
}
