package updater

import (
	"crypto/sha256"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/velia-the-veil/le_voile/internal/crypto"
)

func testVerifier(t *testing.T) (*Verifier, func(data []byte) string) {
	t.Helper()
	pub, priv, err := crypto.GenerateKeyPair()
	if err != nil {
		t.Fatalf("generate key pair: %v", err)
	}
	pubBase64 := crypto.ExportPublicKeyBase64(pub)

	v, err := NewVerifier(pubBase64)
	if err != nil {
		t.Fatalf("new verifier: %v", err)
	}

	signFn := func(data []byte) string {
		sig, err := crypto.Sign(priv, data)
		if err != nil {
			t.Fatalf("sign: %v", err)
		}
		// Raw bytes, matching the on-wire format produced by cmd/signpkg and
		// accepted by cmd/verifypkg. VerifySignature reads the .sig sidecar
		// as raw 64 bytes (no base64).
		return string(sig)
	}

	return v, signFn
}

func writeTestFile(t *testing.T, dir, name, content string) string {
	t.Helper()
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("write test file: %v", err)
	}
	return path
}

func TestVerifier_VerifyChecksum_Valid(t *testing.T) {
	v, _ := testVerifier(t)
	dir := t.TempDir()

	binaryContent := "hello world binary"
	binaryPath := writeTestFile(t, dir, "binary.exe", binaryContent)

	hash := fmt.Sprintf("%x", sha256.Sum256([]byte(binaryContent)))
	checksumContent := fmt.Sprintf("%s  binary.exe\n", hash)
	checksumPath := writeTestFile(t, dir, "checksums.txt", checksumContent)

	if err := v.VerifyChecksum(binaryPath, checksumPath); err != nil {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestVerifier_VerifyChecksum_Invalid(t *testing.T) {
	v, _ := testVerifier(t)
	dir := t.TempDir()

	binaryPath := writeTestFile(t, dir, "binary.exe", "actual content")
	checksumPath := writeTestFile(t, dir, "checksums.txt", "0000000000000000000000000000000000000000000000000000000000000000  binary.exe\n")

	err := v.VerifyChecksum(binaryPath, checksumPath)
	if !errors.Is(err, ErrChecksumMismatch) {
		t.Errorf("expected ErrChecksumMismatch, got %v", err)
	}
}

func TestVerifier_VerifyChecksum_MissingFile(t *testing.T) {
	v, _ := testVerifier(t)
	dir := t.TempDir()

	binaryPath := writeTestFile(t, dir, "binary.exe", "content")

	err := v.VerifyChecksum(binaryPath, filepath.Join(dir, "nonexistent.txt"))
	if err == nil {
		t.Error("expected error for missing checksum file")
	}
}

// TestVerifier_VerifyChecksum_ConstantTime is a regression test ensuring that
// VerifyChecksum rejects any single-character divergence between the computed and
// expected hash. It does NOT measure timing (unreliable in Go tests); it guards
// the NFR9c switch to subtle.ConstantTimeCompare (verify.go) from accidental
// reversion to `!=`.
func TestVerifier_VerifyChecksum_ConstantTime(t *testing.T) {
	v, _ := testVerifier(t)
	dir := t.TempDir()

	binaryContent := "payload"
	binaryPath := writeTestFile(t, dir, "binary.exe", binaryContent)

	realHash := fmt.Sprintf("%x", sha256.Sum256([]byte(binaryContent)))
	// Flip the last character so lengths match but content differs — exercises
	// the constant-time comparison rather than an early length-mismatch bail.
	flipped := realHash[:len(realHash)-1] + flipHex(realHash[len(realHash)-1])
	if flipped == realHash {
		t.Fatal("flipped hash must differ from real hash")
	}
	checksumPath := writeTestFile(t, dir, "checksums.txt", fmt.Sprintf("%s  binary.exe\n", flipped))

	if err := v.VerifyChecksum(binaryPath, checksumPath); !errors.Is(err, ErrChecksumMismatch) {
		t.Errorf("expected ErrChecksumMismatch for tampered hash, got %v", err)
	}
}

func flipHex(c byte) string {
	if c == '0' {
		return "1"
	}
	return "0"
}

func TestVerifier_VerifyChecksum_BinaryNotInChecksums(t *testing.T) {
	v, _ := testVerifier(t)
	dir := t.TempDir()

	binaryPath := writeTestFile(t, dir, "binary.exe", "content")
	checksumPath := writeTestFile(t, dir, "checksums.txt", "abcdef1234567890  other_file.exe\n")

	err := v.VerifyChecksum(binaryPath, checksumPath)
	if !errors.Is(err, ErrNoMatchingChecksum) {
		t.Errorf("expected ErrNoMatchingChecksum, got %v", err)
	}
}

func TestVerifier_VerifySignature_Valid(t *testing.T) {
	v, signFn := testVerifier(t)
	dir := t.TempDir()

	checksumContent := "abcdef123456  binary.exe\n"
	checksumPath := writeTestFile(t, dir, "checksums.txt", checksumContent)

	sig := signFn([]byte(checksumContent))
	sigPath := writeTestFile(t, dir, "checksums.txt.sig", sig)

	if err := v.VerifySignature(checksumPath, sigPath); err != nil {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestVerifier_VerifySignature_Invalid(t *testing.T) {
	v, _ := testVerifier(t)
	dir := t.TempDir()

	checksumPath := writeTestFile(t, dir, "checksums.txt", "checksum content")
	// Invalid signature (correct 64-byte length but wrong content — all zeros)
	sigPath := writeTestFile(t, dir, "checksums.txt.sig", string(make([]byte, 64)))

	err := v.VerifySignature(checksumPath, sigPath)
	if !errors.Is(err, ErrSignatureInvalid) {
		t.Errorf("expected ErrSignatureInvalid, got %v", err)
	}
}

func TestVerifier_VerifySignature_MissingFile(t *testing.T) {
	v, _ := testVerifier(t)
	dir := t.TempDir()

	checksumPath := writeTestFile(t, dir, "checksums.txt", "content")

	err := v.VerifySignature(checksumPath, filepath.Join(dir, "nonexistent.sig"))
	if err == nil {
		t.Error("expected error for missing signature file")
	}
}

func TestVerifier_VerifyStagedUpdate_Success(t *testing.T) {
	v, signFn := testVerifier(t)
	dir := t.TempDir()

	binaryContent := "the real binary data"
	binaryPath := writeTestFile(t, dir, "binary.exe", binaryContent)

	hash := fmt.Sprintf("%x", sha256.Sum256([]byte(binaryContent)))
	checksumContent := fmt.Sprintf("%s  binary.exe\n", hash)
	checksumPath := writeTestFile(t, dir, "checksums.txt", checksumContent)

	sig := signFn([]byte(checksumContent))
	sigPath := writeTestFile(t, dir, "checksums.txt.sig", sig)

	staged := &StagedUpdate{
		BinaryPath:    binaryPath,
		ChecksumPath:  checksumPath,
		SignaturePath: sigPath,
		Version:       "1.0.0",
	}

	if err := v.VerifyStagedUpdate(staged); err != nil {
		t.Errorf("unexpected error: %v", err)
	}

	// Files should still exist
	for _, p := range []string{binaryPath, checksumPath, sigPath} {
		if _, err := os.Stat(p); err != nil {
			t.Errorf("file should still exist: %s", p)
		}
	}
}

func TestVerifier_VerifyStagedUpdate_ChecksumFail_FilesRemoved(t *testing.T) {
	v, signFn := testVerifier(t)
	dir := t.TempDir()

	binaryPath := writeTestFile(t, dir, "binary.exe", "actual content")
	checksumContent := "0000000000000000000000000000000000000000000000000000000000000000  binary.exe\n"
	checksumPath := writeTestFile(t, dir, "checksums.txt", checksumContent)
	sig := signFn([]byte(checksumContent))
	sigPath := writeTestFile(t, dir, "checksums.txt.sig", sig)

	staged := &StagedUpdate{
		BinaryPath:    binaryPath,
		ChecksumPath:  checksumPath,
		SignaturePath: sigPath,
		Version:       "1.0.0",
	}

	err := v.VerifyStagedUpdate(staged)
	if !errors.Is(err, ErrChecksumMismatch) {
		t.Errorf("expected ErrChecksumMismatch, got %v", err)
	}

	// All files should be removed
	for _, p := range []string{binaryPath, checksumPath, sigPath} {
		if _, err := os.Stat(p); !os.IsNotExist(err) {
			t.Errorf("file should be removed after failed verification: %s", p)
		}
	}
}

func TestVerifier_VerifyStagedUpdate_SignatureFail_FilesRemoved(t *testing.T) {
	v, _ := testVerifier(t)
	dir := t.TempDir()

	binaryContent := "binary data"
	binaryPath := writeTestFile(t, dir, "binary.exe", binaryContent)

	hash := fmt.Sprintf("%x", sha256.Sum256([]byte(binaryContent)))
	checksumContent := fmt.Sprintf("%s  binary.exe\n", hash)
	checksumPath := writeTestFile(t, dir, "checksums.txt", checksumContent)

	// Wrong signature
	sigPath := writeTestFile(t, dir, "checksums.txt.sig", string(make([]byte, 64)))

	staged := &StagedUpdate{
		BinaryPath:    binaryPath,
		ChecksumPath:  checksumPath,
		SignaturePath: sigPath,
		Version:       "1.0.0",
	}

	err := v.VerifyStagedUpdate(staged)
	if !errors.Is(err, ErrSignatureInvalid) {
		t.Errorf("expected ErrSignatureInvalid, got %v", err)
	}

	// All files should be removed
	for _, p := range []string{binaryPath, checksumPath, sigPath} {
		if _, err := os.Stat(p); !os.IsNotExist(err) {
			t.Errorf("file should be removed after failed verification: %s", p)
		}
	}
}
