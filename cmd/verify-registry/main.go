// Command verify-registry loads a relay-registry.json file, overrides its
// master public key with a trusted value, and reports how many entries pass
// Ed25519 signature verification.
//
// Usage:
//
//	verify-registry <path-to-registry.json> <master-public-key-base64>
//
// Exit 0: at least one relay verified.
// Exit 1: parse error, master key invalid, or no relays verified.
// Exit 2: bad invocation.
//
// Intended for operator smoke tests (deploy/smoke_registry.sh --verify), not
// for the running client which uses internal/registry.Client directly.
package main

import (
	"fmt"
	"os"

	"github.com/velia-the-veil/le_voile/internal/registry"
)

func main() {
	if len(os.Args) != 3 {
		fmt.Fprintln(os.Stderr, "usage: verify-registry <registry.json> <master-pub-key-base64>")
		os.Exit(2)
	}

	data, err := os.ReadFile(os.Args[1])
	if err != nil {
		fmt.Fprintln(os.Stderr, "read:", err)
		os.Exit(1)
	}

	reg, err := registry.Parse(data)
	if err != nil {
		fmt.Fprintln(os.Stderr, "parse:", err)
		os.Exit(1)
	}

	// Override with the operator-supplied trust anchor — same pattern as
	// registry.Client.Fetch uses against the embedded master key.
	reg.MasterPublicKey = os.Args[2]

	rejects := 0
	logger := func(id, domain, reason string) {
		fmt.Fprintf(os.Stderr, "  rejected id=%s domain=%s reason=%s\n", id, domain, reason)
		rejects++
	}

	verified, err := reg.VerifyAllWithLogger(logger)
	if err != nil {
		fmt.Fprintln(os.Stderr, "verify:", err)
		os.Exit(1)
	}

	fmt.Printf("ok: %d verified, %d rejected (total %d)\n", len(verified), rejects, len(reg.Relays))
	for _, r := range verified {
		fmt.Printf("  %-20s %s\n", r.ID, r.Domain)
	}
}
