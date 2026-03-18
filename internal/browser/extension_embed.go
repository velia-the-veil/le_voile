//go:generate go run ../../tools/crxgen -key ../../extension/levoile.pem -src ../../extension -out extension_assets/build/levoile.crx -sync-assets extension_assets/src

package browser

import (
	"bytes"
	"crypto/sha256"
	"crypto/x509"
	"embed"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"text/template"
)

//go:embed all:extension_assets
var extensionFS embed.FS

// extensionInitErr stores any error from CRX extraction at init time.
var extensionInitErr error

func init() {
	// Derive ExtensionID from the embedded CRX's public key at startup.
	id, err := extractExtensionIDFromCRX()
	if err != nil {
		extensionInitErr = fmt.Errorf("browser: CRX init failed (extension will not install): %w", err)
		return
	}
	ExtensionID = id
}

// extractExtensionIDFromCRX reads the CRX v3 header to extract the public key
// and derives the extension ID.
func extractExtensionIDFromCRX() (string, error) {
	data, err := extensionFS.ReadFile("extension_assets/build/levoile.crx")
	if err != nil {
		return "", fmt.Errorf("read CRX: %w", err)
	}
	if len(data) < 12 {
		return "", fmt.Errorf("CRX too small")
	}
	// CRX3: "Cr24" + version(4) + header_length(4) + header + zip
	if string(data[:4]) != "Cr24" {
		return "", fmt.Errorf("invalid CRX magic")
	}
	headerLen := binary.LittleEndian.Uint32(data[8:12])
	if uint32(len(data)) < 12+headerLen {
		return "", fmt.Errorf("CRX header truncated")
	}
	headerData := data[12 : 12+headerLen]

	// Parse protobuf manually: find field 2 (sha256_with_rsa proof), then field 1 (public_key).
	pubKey, err := extractPublicKeyFromHeader(headerData)
	if err != nil {
		return "", err
	}

	// Verify it's a valid PKIX public key.
	if _, err := x509.ParsePKIXPublicKey(pubKey); err != nil {
		return "", fmt.Errorf("invalid public key in CRX: %w", err)
	}

	h := sha256.Sum256(pubKey)
	var sb strings.Builder
	for i := 0; i < 16; i++ {
		sb.WriteByte('a' + (h[i]>>4)&0x0f)
		sb.WriteByte('a' + h[i]&0x0f)
	}
	return sb.String(), nil
}

// extractPublicKeyFromHeader parses the CRX3 header protobuf to find the public key.
func extractPublicKeyFromHeader(data []byte) ([]byte, error) {
	// CrxFileHeader: field 2 = sha256_with_rsa (AsymmetricKeyProof)
	pos := 0
	for pos < len(data) {
		tag, n := readVarint(data[pos:])
		if n == 0 {
			break
		}
		pos += n
		fieldNum := tag >> 3
		wireType := tag & 0x7

		if wireType == 2 { // length-delimited
			length, n := readVarint(data[pos:])
			if n == 0 {
				break
			}
			pos += n
			if uint64(pos)+length > uint64(len(data)) {
				break
			}
			fieldData := data[pos : pos+int(length)]
			pos += int(length)

			if fieldNum == 2 {
				// This is AsymmetricKeyProof — parse field 1 (public_key).
				return extractFieldBytes(fieldData, 1)
			}
		} else if wireType == 0 { // varint
			_, n := readVarint(data[pos:])
			pos += n
		}
	}
	return nil, fmt.Errorf("no sha256_with_rsa proof found in CRX header")
}

// extractFieldBytes extracts a bytes field from protobuf data.
func extractFieldBytes(data []byte, targetField uint64) ([]byte, error) {
	pos := 0
	for pos < len(data) {
		tag, n := readVarint(data[pos:])
		if n == 0 {
			break
		}
		pos += n
		fieldNum := tag >> 3
		wireType := tag & 0x7

		if wireType == 2 {
			length, n := readVarint(data[pos:])
			if n == 0 {
				break
			}
			pos += n
			if uint64(pos)+length > uint64(len(data)) {
				break
			}
			fieldData := data[pos : pos+int(length)]
			pos += int(length)

			if fieldNum == targetField {
				return fieldData, nil
			}
		} else if wireType == 0 {
			_, n := readVarint(data[pos:])
			pos += n
		}
	}
	return nil, fmt.Errorf("field %d not found", targetField)
}

func readVarint(data []byte) (uint64, int) {
	var v uint64
	for i, b := range data {
		if i >= 10 {
			return 0, 0
		}
		v |= uint64(b&0x7f) << (7 * i)
		if b < 0x80 {
			return v, i + 1
		}
	}
	return 0, 0
}

// updatesXMLTemplate is the Chrome Update Manifest template.
// The version and codebase are filled in at deploy time from the embedded manifest.json.
var updatesXMLTemplate = template.Must(template.New("updates").Parse(
	`<?xml version='1.0' encoding='UTF-8'?>
<gupdate xmlns='http://www.google.com/update2/response' protocol='2.0'>
  <app appid='{{.AppID}}'>
    <updatecheck codebase='{{.Codebase}}' version='{{.Version}}' />
  </app>
</gupdate>
`))

// deployExtensionFiles extracts embedded extension files to the deploy directory
// and generates the updates.xml (Chrome) and levoile.xpi (Firefox) dynamically.
// Must be called before writing browser policies that reference these files.
//
// Cohabitation SysProxy + Extension:
// The extension has priority in browsers (enterprise policy > SysProxy) while the
// SysProxy handles apps outside browsers (Electron, Windows native apps, curl, etc.).
// Both route traffic to the same local proxy at 127.0.0.1:50113 — no conflict.
func deployExtensionFiles() error {
	if extensionInitErr != nil {
		return extensionInitErr
	}
	deployDir := extensionDeployDir()

	// Create directory structure.
	chromeDir := filepath.Join(deployDir, "chrome")
	if err := os.MkdirAll(chromeDir, 0755); err != nil {
		return fmt.Errorf("browser: deploy extension: mkdir chrome: %w", err)
	}

	// Deploy CRX for Chrome (pre-signed, embedded at build time).
	crxData, err := extensionFS.ReadFile("extension_assets/build/levoile.crx")
	if err != nil {
		return fmt.Errorf("browser: deploy extension: read embedded CRX: %w", err)
	}
	crxPath := filepath.Join(chromeDir, "levoile.crx")
	if err := os.WriteFile(crxPath, crxData, 0644); err != nil {
		return fmt.Errorf("browser: deploy extension: write CRX: %w", err)
	}

	// Read version from embedded manifest.json.
	version, err := readEmbeddedManifestVersion()
	if err != nil {
		return fmt.Errorf("browser: deploy extension: %w", err)
	}

	// Generate updates.xml with OS-specific absolute path.
	crxAbsPath := filepath.ToSlash(crxPath)
	codebase := "file:///" + strings.TrimLeft(crxAbsPath, "/")
	if err := generateUpdatesXML(chromeDir, version, codebase); err != nil {
		return fmt.Errorf("browser: deploy extension: %w", err)
	}

	// Deploy signed Firefox XPI (pre-signed via AMO, embedded at build time).
	xpiData, err := extensionFS.ReadFile("extension_assets/build/levoile.xpi")
	if err != nil {
		return fmt.Errorf("browser: deploy extension: read embedded XPI: %w", err)
	}
	xpiPath := filepath.Join(deployDir, "levoile.xpi")
	if err := os.WriteFile(xpiPath, xpiData, 0644); err != nil {
		return fmt.Errorf("browser: deploy extension: write XPI: %w", err)
	}

	return nil
}

// readEmbeddedManifestVersion reads the version field from the embedded manifest.json.
func readEmbeddedManifestVersion() (string, error) {
	data, err := extensionFS.ReadFile("extension_assets/src/manifest.json")
	if err != nil {
		return "", fmt.Errorf("read manifest.json: %w", err)
	}
	var manifest struct {
		Version string `json:"version"`
	}
	if err := json.Unmarshal(data, &manifest); err != nil {
		return "", fmt.Errorf("parse manifest.json: %w", err)
	}
	if manifest.Version == "" {
		return "", fmt.Errorf("manifest.json has empty version")
	}
	return manifest.Version, nil
}

// generateUpdatesXML writes the Chrome Update Manifest XML to the chrome dir.
func generateUpdatesXML(chromeDir, version, codebase string) error {
	if ExtensionID == "" {
		return fmt.Errorf("generate updates.xml: no extension ID configured")
	}

	var buf bytes.Buffer
	err := updatesXMLTemplate.Execute(&buf, struct {
		AppID    string
		Codebase string
		Version  string
	}{
		AppID:    ExtensionID,
		Codebase: codebase,
		Version:  version,
	})
	if err != nil {
		return fmt.Errorf("generate updates.xml: template: %w", err)
	}

	xmlPath := filepath.Join(chromeDir, "updates.xml")
	if err := os.WriteFile(xmlPath, buf.Bytes(), 0644); err != nil {
		return fmt.Errorf("generate updates.xml: write: %w", err)
	}
	return nil
}

