package updater

import (
	"bufio"
	"crypto/ed25519"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/velia-the-veil/le_voile/internal/crypto"
)

var (
	// ErrChecksumMismatch indicates the SHA256 checksum does not match.
	ErrChecksumMismatch = errors.New("updater: checksum mismatch")
	// ErrSignatureInvalid indicates the Ed25519 signature is invalid.
	ErrSignatureInvalid = errors.New("updater: signature invalid")
	// ErrNoMatchingChecksum indicates no checksum entry was found for the binary.
	ErrNoMatchingChecksum = errors.New("updater: no matching checksum for binary")
)

// Verifier verifies the integrity and authenticity of staged updates.
type Verifier struct {
	relayPubKey ed25519.PublicKey
}

// NewVerifier creates a Verifier using the given base64-encoded Ed25519 public key.
func NewVerifier(pubKeyBase64 string) (*Verifier, error) {
	pubKey, err := crypto.ImportPublicKeyBase64(pubKeyBase64)
	if err != nil {
		return nil, fmt.Errorf("updater: verifier: %w", err)
	}
	return &Verifier{relayPubKey: pubKey}, nil
}

// VerifyChecksum verifies that the SHA256 checksum of the binary matches
// the expected checksum from the checksums file.
// The checksums file uses the GoReleaser format: "hash  filename" per line.
func (v *Verifier) VerifyChecksum(binaryPath, checksumPath string) error {
	expectedHash, err := findChecksum(checksumPath, filepath.Base(binaryPath))
	if err != nil {
		return err
	}

	actualHash, err := hashFile(binaryPath)
	if err != nil {
		return fmt.Errorf("updater: verify checksum: %w", err)
	}

	// NFR9c: constant-time comparison to prevent timing attacks on checksum verification.
	if subtle.ConstantTimeCompare([]byte(actualHash), []byte(expectedHash)) != 1 {
		return ErrChecksumMismatch
	}
	return nil
}

// VerifySignature verifies that checksums.txt is signed with the relay's Ed25519 key.
// The signature file contains the base64-encoded Ed25519 signature.
// NFR9c: the underlying ed25519.Verify (Go stdlib) is constant-time by construction.
func (v *Verifier) VerifySignature(checksumPath, signaturePath string) error {
	checksumData, err := os.ReadFile(checksumPath)
	if err != nil {
		return fmt.Errorf("updater: verify signature: read checksums: %w", err)
	}

	sigData, err := os.ReadFile(signaturePath)
	if err != nil {
		return fmt.Errorf("updater: verify signature: read signature: %w", err)
	}

	sigBytes, err := base64.StdEncoding.DecodeString(strings.TrimSpace(string(sigData)))
	if err != nil {
		return fmt.Errorf("updater: verify signature: decode: %w", err)
	}

	if !crypto.Verify(v.relayPubKey, checksumData, sigBytes) {
		return ErrSignatureInvalid
	}
	return nil
}

// VerifyStagedUpdate verifies checksum and signature of a staged update.
// If verification fails, all staged files are removed.
func (v *Verifier) VerifyStagedUpdate(staged *StagedUpdate) error {
	if err := v.VerifyChecksum(staged.BinaryPath, staged.ChecksumPath); err != nil {
		removeStagedFiles(staged)
		return err
	}

	if err := v.VerifySignature(staged.ChecksumPath, staged.SignaturePath); err != nil {
		removeStagedFiles(staged)
		return err
	}

	return nil
}

// findChecksum parses a checksums file and returns the hash for the given filename.
func findChecksum(checksumPath, filename string) (string, error) {
	f, err := os.Open(checksumPath)
	if err != nil {
		return "", fmt.Errorf("updater: verify checksum: %w", err)
	}
	defer f.Close()

	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		// GoReleaser format: "hash  filename" (double space)
		parts := strings.SplitN(line, "  ", 2)
		if len(parts) != 2 {
			continue
		}
		if strings.TrimSpace(parts[1]) == filename {
			return strings.TrimSpace(parts[0]), nil
		}
	}

	return "", ErrNoMatchingChecksum
}

// hashFile computes the SHA256 hex digest of a file.
func hashFile(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()

	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", err
	}
	return fmt.Sprintf("%x", h.Sum(nil)), nil
}

// removeStagedFiles removes all files in a staged update.
func removeStagedFiles(staged *StagedUpdate) {
	os.Remove(staged.BinaryPath)
	os.Remove(staged.ChecksumPath)
	os.Remove(staged.SignaturePath)
}
