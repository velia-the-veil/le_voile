// Command signpkg produces detached Ed25519 signatures for a list of
// distribution artifacts (story 7.4). It is the signing half of the
// signpkg <-> verifypkg pair; the latter is shipped to end users.
//
// Usage:
//
//	signpkg -signing-key <path> <artifact> [<artifact> ...]
//	signpkg -signing-key <path> -checksums <path> <artifact> [...]
//
// For each argument it reads the file, signs its contents with the Ed25519
// private key, and writes <artifact>.sig (64 raw bytes, per RFC 8032).
// When -checksums is provided, that file is also signed into
// <checksums>.sig — this is how GoReleaser post-build hooks chain.
//
// The signing key file must contain a base64 standard Ed25519 private key
// (64 bytes after decode) on a single line, matching the format produced
// by cmd/genkey and cmd/genregistry.
//
// Exit codes: 0 success, 1 I/O or crypto error, 2 bad invocation.
package main

import (
	"crypto/ed25519"
	"flag"
	"fmt"
	"os"
	"strings"

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
	fs := flag.NewFlagSet("signpkg", flag.ContinueOnError)
	keyPath := fs.String("signing-key", "", "path to Ed25519 private key file (base64)")
	checksums := fs.String("checksums", "", "optional: path to checksums.txt to sign in addition to positional args")
	if err := fs.Parse(args); err != nil {
		return &usageError{msg: "signpkg: " + err.Error()}
	}
	if *keyPath == "" {
		return &usageError{msg: "signpkg: -signing-key is required"}
	}

	artifacts := fs.Args()
	if *checksums != "" {
		artifacts = append(artifacts, *checksums)
	}
	if len(artifacts) == 0 {
		return &usageError{msg: "signpkg: no artifacts provided"}
	}

	priv, err := loadPrivateKey(*keyPath)
	if err != nil {
		return err
	}

	for _, artifact := range artifacts {
		if err := signArtifact(priv, artifact); err != nil {
			return err
		}
	}
	return nil
}

func loadPrivateKey(path string) (ed25519.PrivateKey, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("signpkg: read signing key: %w", err)
	}
	priv, err := lecrypto.ImportPrivateKeyBase64(strings.TrimSpace(string(raw)))
	if err != nil {
		return nil, fmt.Errorf("signpkg: parse signing key: %w", err)
	}
	return priv, nil
}

// maxArtifactSize caps the size of an artifact signpkg will read into RAM.
// stdlib ed25519.Sign takes []byte (not an io.Reader), so we must buffer the
// whole artifact. 500 MiB is comfortable headroom above our current max
// (~40 MiB .deb) without exposing a CI runner to OOM from an accidental
// 10 GiB file — which would indicate a build pipeline regression anyway.
const maxArtifactSize = 500 * 1024 * 1024

func signArtifact(priv ed25519.PrivateKey, path string) error {
	info, err := os.Stat(path)
	if err != nil {
		return fmt.Errorf("signpkg: stat %s: %w", path, err)
	}
	if info.Size() > maxArtifactSize {
		return fmt.Errorf("signpkg: %s exceeds max signable size (%d bytes > %d bytes = 500 MiB); investigate the build pipeline",
			path, info.Size(), maxArtifactSize)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("signpkg: read %s: %w", path, err)
	}
	sig, err := lecrypto.Sign(priv, data)
	if err != nil {
		return fmt.Errorf("signpkg: sign %s: %w", path, err)
	}
	sigPath := path + ".sig"
	if err := os.WriteFile(sigPath, sig, 0o644); err != nil {
		return fmt.Errorf("signpkg: write %s: %w", sigPath, err)
	}
	fmt.Printf("signed %s -> %s (%d bytes)\n", path, sigPath, len(sig))
	return nil
}
