// Command verifypkg verifies a detached Ed25519 signature of a Le Voile
// distribution artifact (story 7.4). It is the user-facing counterpart of
// tools/signpkg and is bundled into every release archive as levoile-verify.
//
// Usage:
//
//	verifypkg <artifact> <artifact.sig>
//	verifypkg -pubkey <base64> <artifact> <artifact.sig>
//	verifypkg -try-next <artifact> <artifact.sig>
//
// Default mode (no -pubkey): verifies against the public key embedded at
// build time via internal/crypto/release_keys.go. This is the safest path
// for end users — the trust anchor ships with the binary, not over the
// network.
//
// With -pubkey: verifies against an explicitly supplied base64 Ed25519
// public key (32 raw bytes post-decode). Use this for auditing or for
// verifying artifacts signed under a non-current key.
//
// With -try-next: after the current key fails, also tries the rotation
// key if ReleasePublicKeyNextBase64 is configured (NFR22h).
//
// Exit codes: 0 signature valid, 1 signature invalid or I/O error,
// 2 bad invocation.
package main

import (
	"crypto/ed25519"
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
	fs := flag.NewFlagSet("verifypkg", flag.ContinueOnError)
	pubFlag := fs.String("pubkey", "", "explicit base64 Ed25519 public key (overrides embedded key)")
	tryNext := fs.Bool("try-next", false, "also try the rotation key (NFR22h dual-signature window)")
	if err := fs.Parse(args); err != nil {
		return &usageError{msg: "verifypkg: " + err.Error()}
	}

	positional := fs.Args()
	if len(positional) != 2 {
		return &usageError{msg: "verifypkg: expected <artifact> <artifact.sig>"}
	}
	artifactPath, sigPath := positional[0], positional[1]

	artifact, err := os.ReadFile(artifactPath)
	if err != nil {
		return fmt.Errorf("verifypkg: read artifact: %w", err)
	}
	sig, err := os.ReadFile(sigPath)
	if err != nil {
		return fmt.Errorf("verifypkg: read signature: %w", err)
	}
	// An Ed25519 detached signature is exactly 64 bytes per RFC 8032. Reject
	// any other size early with a clear diagnostic — this catches corrupted
	// transfers, CRLF-mangled downloads, and accidental base64-encoded sigs
	// long before Verify() returns a generic "signature mismatch".
	if len(sig) != ed25519.SignatureSize {
		return fmt.Errorf("fail: %s (signature file is not a valid Ed25519 signature: expected %d bytes, got %d)",
			sigPath, ed25519.SignatureSize, len(sig))
	}

	keys, err := resolveKeys(*pubFlag, *tryNext)
	if err != nil {
		return err
	}

	for _, k := range keys {
		if lecrypto.Verify(k.pub, artifact, sig) {
			// Success goes to stdout (Unix convention) so callers can
			// `verifypkg f f.sig | grep ok` without redirecting stderr.
			fmt.Fprintf(os.Stdout, "ok: %s (verified with %s)\n", artifactPath, k.label)
			return nil
		}
	}
	return fmt.Errorf("fail: %s (signature did not verify under any trusted key)", artifactPath)
}

type namedKey struct {
	pub   ed25519.PublicKey
	label string
}

func resolveKeys(pubFlag string, tryNext bool) ([]namedKey, error) {
	if pubFlag != "" {
		pub, err := lecrypto.ImportPublicKeyBase64(pubFlag)
		if err != nil {
			return nil, fmt.Errorf("verifypkg: parse -pubkey: %w", err)
		}
		return []namedKey{{pub: pub, label: "-pubkey"}}, nil
	}

	var keys []namedKey
	cur, err := lecrypto.ReleasePublicKeyCurrent()
	if err != nil {
		// If the current key is a placeholder and the user didn't override,
		// we cannot verify anything.
		return nil, fmt.Errorf("verifypkg: embedded release key not available: %w", err)
	}
	keys = append(keys, namedKey{pub: cur, label: "embedded current"})

	if tryNext {
		nxt, hasNext, nextErr := lecrypto.ReleasePublicKeyNext()
		if nextErr != nil {
			return nil, fmt.Errorf("verifypkg: parse rotation key: %w", nextErr)
		}
		if hasNext {
			keys = append(keys, namedKey{pub: nxt, label: "embedded next"})
		} else {
			fmt.Fprintln(os.Stderr, "note: -try-next set but no rotation key configured")
		}
	}
	return keys, nil
}
