// Command crxgen generates a Chrome CRX v3 file from the extension source directory
// and optionally syncs source files to the embed assets directory.
// Usage: go run ./tools/crxgen -key extension/levoile.pem -src extension -out internal/browser/extension_assets/build/levoile.crx -sync-assets internal/browser/extension_assets/src
//
// The PEM key must be an RSA private key. If it doesn't exist, crxgen generates one.
// The extension ID is derived from the public key and printed to stdout.
package main

import (
	"archive/zip"
	"bytes"
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"encoding/binary"
	"encoding/pem"
	"flag"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

func main() {
	keyPath := flag.String("key", "extension/levoile.pem", "Path to RSA PEM private key")
	srcDir := flag.String("src", "extension", "Extension source directory")
	outPath := flag.String("out", "internal/browser/extension_assets/build/levoile.crx", "Output CRX file path")
	syncAssets := flag.String("sync-assets", "", "Sync extension source files to this embed assets directory (e.g., internal/browser/extension_assets/src)")
	flag.Parse()

	// Sync source files to embed assets directory if requested.
	if *syncAssets != "" {
		if err := syncExtensionAssets(*srcDir, *syncAssets); err != nil {
			fmt.Fprintf(os.Stderr, "sync-assets: %v\n", err)
			os.Exit(1)
		}
		fmt.Printf("Assets synced: %s -> %s\n", *srcDir, *syncAssets)
	}

	key, err := loadOrGenerateKey(*keyPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "key: %v\n", err)
		os.Exit(1)
	}

	// Compute and display extension ID.
	pubDER, err := x509.MarshalPKIXPublicKey(&key.PublicKey)
	if err != nil {
		fmt.Fprintf(os.Stderr, "marshal public key: %v\n", err)
		os.Exit(1)
	}
	extID := computeExtensionID(pubDER)
	fmt.Printf("Extension ID: %s\n", extID)

	// Create ZIP of extension source (excluding non-extension files).
	zipData, err := createExtensionZip(*srcDir)
	if err != nil {
		fmt.Fprintf(os.Stderr, "zip: %v\n", err)
		os.Exit(1)
	}

	// Build CRX v3.
	crxData, err := buildCRX3(zipData, key, pubDER)
	if err != nil {
		fmt.Fprintf(os.Stderr, "crx: %v\n", err)
		os.Exit(1)
	}

	if err := os.MkdirAll(filepath.Dir(*outPath), 0755); err != nil {
		fmt.Fprintf(os.Stderr, "mkdir: %v\n", err)
		os.Exit(1)
	}
	if err := os.WriteFile(*outPath, crxData, 0644); err != nil {
		fmt.Fprintf(os.Stderr, "write: %v\n", err)
		os.Exit(1)
	}

	fmt.Printf("CRX written to %s (%d bytes)\n", *outPath, len(crxData))
}

// syncExtensionAssets copies extension source files (excluding PEM and build artifacts)
// to the embed assets directory, keeping them in sync with the canonical source.
func syncExtensionAssets(srcDir, assetsDir string) error {
	if err := os.MkdirAll(assetsDir, 0755); err != nil {
		return fmt.Errorf("mkdir assets: %w", err)
	}

	return filepath.WalkDir(srcDir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}

		rel, _ := filepath.Rel(srcDir, path)
		rel = filepath.ToSlash(rel)

		// Skip PEM key and build artifacts.
		if rel == "levoile.pem" || strings.HasPrefix(rel, "build/") {
			return nil
		}

		destPath := filepath.Join(assetsDir, rel)
		if d.IsDir() {
			return os.MkdirAll(destPath, 0755)
		}

		data, err := os.ReadFile(path)
		if err != nil {
			return fmt.Errorf("read %s: %w", path, err)
		}
		if err := os.WriteFile(destPath, data, 0644); err != nil {
			return fmt.Errorf("write %s: %w", destPath, err)
		}
		return nil
	})
}

func loadOrGenerateKey(path string) (*rsa.PrivateKey, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if !os.IsNotExist(err) {
			return nil, err
		}
		key, err := rsa.GenerateKey(rand.Reader, 2048)
		if err != nil {
			return nil, fmt.Errorf("generate key: %w", err)
		}
		pemData := pem.EncodeToMemory(&pem.Block{
			Type:  "RSA PRIVATE KEY",
			Bytes: x509.MarshalPKCS1PrivateKey(key),
		})
		if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
			return nil, err
		}
		if err := os.WriteFile(path, pemData, 0600); err != nil {
			return nil, err
		}
		fmt.Fprintf(os.Stderr, "Generated new PEM key: %s\n", path)
		return key, nil
	}

	block, _ := pem.Decode(data)
	if block == nil {
		return nil, fmt.Errorf("no PEM block found in %s", path)
	}
	return x509.ParsePKCS1PrivateKey(block.Bytes)
}

// computeExtensionID derives the Chrome extension ID from the DER public key.
// Algorithm: SHA256 of DER, take first 16 bytes, encode each half-byte as a-p.
func computeExtensionID(pubDER []byte) string {
	h := sha256.Sum256(pubDER)
	var sb strings.Builder
	for i := 0; i < 16; i++ {
		sb.WriteByte('a' + (h[i]>>4)&0x0f)
		sb.WriteByte('a' + h[i]&0x0f)
	}
	return sb.String()
}

func createExtensionZip(srcDir string) ([]byte, error) {
	var buf bytes.Buffer
	w := zip.NewWriter(&buf)

	err := filepath.WalkDir(srcDir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}

		rel, _ := filepath.Rel(srcDir, path)
		rel = filepath.ToSlash(rel)

		// Skip non-Chrome-extension files.
		if rel == "manifest_firefox.json" || rel == "levoile.pem" || strings.HasPrefix(rel, "build/") {
			return nil
		}

		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}

		fh, err := w.Create(rel)
		if err != nil {
			return err
		}
		_, err = fh.Write(data)
		return err
	})
	if err != nil {
		return nil, err
	}
	if err := w.Close(); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// buildCRX3 builds a CRX v3 file from ZIP data using RSA signing.
// CRX3 format: magic("Cr24") + version(3) + header_length + header_proto + zip_data
// Header proto fields are manually encoded (no protobuf dependency).
func buildCRX3(zipData []byte, key *rsa.PrivateKey, pubDER []byte) ([]byte, error) {
	// CRX ID = SHA256(pubDER)[:16]
	crxIDHash := sha256.Sum256(pubDER)
	crxID := crxIDHash[:16]

	// SignedData proto: field 1 (bytes) = crx_id
	signedDataBytes := protoBytes(1, crxID)

	// Build the data to sign: "CRX3 SignedData\x00" + LE32(len(signedData)) + signedData + zip
	var toBeSigned bytes.Buffer
	toBeSigned.WriteString("CRX3 SignedData\x00")
	sdLen := make([]byte, 4)
	binary.LittleEndian.PutUint32(sdLen, uint32(len(signedDataBytes)))
	toBeSigned.Write(sdLen)
	toBeSigned.Write(signedDataBytes)
	toBeSigned.Write(zipData)

	hashed := sha256.Sum256(toBeSigned.Bytes())
	sig, err := rsa.SignPKCS1v15(rand.Reader, key, crypto.SHA256, hashed[:])
	if err != nil {
		return nil, fmt.Errorf("sign: %w", err)
	}

	// AsymmetricKeyProof proto: field 1 (bytes)=public_key, field 2 (bytes)=signature
	proof := append(protoBytes(1, pubDER), protoBytes(2, sig)...)

	// CrxFileHeader proto:
	//   field 2 (repeated AsymmetricKeyProof) = sha256_with_rsa
	//   field 10000 (bytes) = signed_header_data
	header := append(protoBytes(2, proof), protoBytes(10000, signedDataBytes)...)

	// Write CRX3 binary.
	var out bytes.Buffer
	out.Write([]byte("Cr24"))
	binary.Write(&out, binary.LittleEndian, uint32(3))
	binary.Write(&out, binary.LittleEndian, uint32(len(header)))
	out.Write(header)
	out.Write(zipData)

	return out.Bytes(), nil
}

// protoBytes encodes a protobuf field with wire type 2 (length-delimited).
func protoBytes(fieldNum int, data []byte) []byte {
	tag := uint64(fieldNum)<<3 | 2 // wire type 2
	var buf bytes.Buffer
	writeVarint(&buf, tag)
	writeVarint(&buf, uint64(len(data)))
	buf.Write(data)
	return buf.Bytes()
}

func writeVarint(buf *bytes.Buffer, v uint64) {
	for v >= 0x80 {
		buf.WriteByte(byte(v) | 0x80)
		v >>= 7
	}
	buf.WriteByte(byte(v))
}
