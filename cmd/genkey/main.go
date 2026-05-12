// Command genkey generates a fresh Ed25519 keypair for release signing
// (story 7.4). The private key lives on the maintainer's offline machine
// per NFR22g; the public key is committed to the repo via
// internal/crypto/release_keys.go and docs/keys/.
//
// Usage:
//
//	genkey -out <basepath> [-pem] [-force]
//
// Writes:
//
//	<basepath>.key     — 64-byte Ed25519 private key, base64, mode 0600
//	<basepath>.pub     — 32-byte Ed25519 public key, base64
//	<basepath>.pub.pem — PKIX-wrapped PEM (when -pem) for openssl pkeyutl
//
// Exit codes: 0 success, 1 I/O or crypto error, 2 bad invocation.
package main

import (
	"flag"
	"fmt"
	"os"

	lecrypto "github.com/velia-the-veil/le_voile/internal/crypto"
)

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, err)
		if _, ok := err.(*usageError); ok {
			os.Exit(2)
		}
		os.Exit(1)
	}
}

type usageError struct{ msg string }

func (e *usageError) Error() string { return e.msg }

func run(args []string) error {
	fs := flag.NewFlagSet("genkey", flag.ContinueOnError)
	out := fs.String("out", "", "output base path (writes <base>.key, <base>.pub[, <base>.pub.pem])")
	pemOut := fs.Bool("pem", false, "also write <base>.pub.pem (PKIX PEM) for openssl pkeyutl -verify")
	force := fs.Bool("force", false, "overwrite existing files")
	if err := fs.Parse(args); err != nil {
		return &usageError{msg: "genkey: " + err.Error()}
	}
	if *out == "" {
		fs.SetOutput(os.Stderr)
		fs.Usage()
		return &usageError{msg: "genkey: -out is required"}
	}

	pub, priv, err := lecrypto.GenerateKeyPair()
	if err != nil {
		return fmt.Errorf("genkey: generate: %w", err)
	}

	keyPath := *out + ".key"
	pubPath := *out + ".pub"
	pemPath := *out + ".pub.pem"

	if !*force {
		for _, p := range []string{keyPath, pubPath, pemPath} {
			if _, err := os.Stat(p); err == nil {
				return fmt.Errorf("genkey: %s exists (use -force to overwrite)", p)
			}
		}
	}

	// Write private key first — mode 0600. On Windows, os.WriteFile honors the
	// mode argument imperfectly, so we additionally chmod after creation which
	// is a no-op on Windows but reinforces intent on Unix.
	if err := os.WriteFile(keyPath, []byte(lecrypto.ExportPrivateKeyBase64(priv)+"\n"), 0o600); err != nil {
		return fmt.Errorf("genkey: write private key: %w", err)
	}
	if err := os.Chmod(keyPath, 0o600); err != nil {
		return fmt.Errorf("genkey: chmod private key: %w", err)
	}

	if err := os.WriteFile(pubPath, []byte(lecrypto.ExportPublicKeyBase64(pub)+"\n"), 0o644); err != nil {
		return fmt.Errorf("genkey: write public key: %w", err)
	}

	if *pemOut {
		pemBytes, err := lecrypto.ExportPublicKeyPEM(pub)
		if err != nil {
			return fmt.Errorf("genkey: export pem: %w", err)
		}
		if err := os.WriteFile(pemPath, pemBytes, 0o644); err != nil {
			return fmt.Errorf("genkey: write pem: %w", err)
		}
	}

	fmt.Printf("wrote %s (private, mode 0600)\n", keyPath)
	fmt.Printf("wrote %s (public)\n", pubPath)
	if *pemOut {
		fmt.Printf("wrote %s (public PEM)\n", pemPath)
	}
	fmt.Printf("\npublic key (base64): %s\n", lecrypto.ExportPublicKeyBase64(pub))
	return nil
}
