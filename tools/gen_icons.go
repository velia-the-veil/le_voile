//go:build ignore

// gen_icons generates simple colored square ICO files for the system tray.
// Usage: go run tools/gen_icons.go
package main

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"os"
	"path/filepath"
)

// ICO file structures per https://en.wikipedia.org/wiki/ICO_(file_format)

type iconDir struct {
	Reserved uint16
	Type     uint16
	Count    uint16
}

type iconDirEntry struct {
	Width      byte
	Height     byte
	ColorCount byte
	Reserved   byte
	Planes     uint16
	BitCount   uint16
	SizeInBytes uint32
	Offset      uint32
}

type bitmapInfoHeader struct {
	Size          uint32
	Width         int32
	Height        int32 // 2x height for XOR+AND masks
	Planes        uint16
	BitCount      uint16
	Compression   uint32
	SizeImage     uint32
	XPelsPerMeter int32
	YPelsPerMeter int32
	ClrUsed       uint32
	ClrImportant  uint32
}

func generateICO(sizes []int, r, g, b byte) ([]byte, error) {
	var buf bytes.Buffer

	// Calculate offsets and image data
	headerSize := 6 + 16*len(sizes)
	type imgData struct {
		data []byte
	}
	images := make([]imgData, len(sizes))

	for i, size := range sizes {
		images[i].data = generateBMPData(size, r, g, b)
	}

	// Write ICONDIR
	dir := iconDir{Reserved: 0, Type: 1, Count: uint16(len(sizes))}
	if err := binary.Write(&buf, binary.LittleEndian, dir); err != nil {
		return nil, err
	}

	// Write ICONDIRENTRY for each size
	offset := uint32(headerSize)
	for i, size := range sizes {
		w := byte(size)
		h := byte(size)
		if size == 256 {
			w = 0 // 0 means 256 in ICO format
			h = 0
		}
		entry := iconDirEntry{
			Width:       w,
			Height:      h,
			ColorCount:  0,
			Reserved:    0,
			Planes:      1,
			BitCount:    32,
			SizeInBytes: uint32(len(images[i].data)),
			Offset:      offset,
		}
		if err := binary.Write(&buf, binary.LittleEndian, entry); err != nil {
			return nil, err
		}
		offset += uint32(len(images[i].data))
	}

	// Write image data
	for _, img := range images {
		buf.Write(img.data)
	}

	return buf.Bytes(), nil
}

func generateBMPData(size int, r, g, b byte) []byte {
	var buf bytes.Buffer

	// Row stride: each pixel is 4 bytes (BGRA), rows padded to 4-byte boundary
	rowSize := size * 4
	// AND mask: 1 bit per pixel, rows padded to 4-byte boundary
	andRowSize := ((size + 31) / 32) * 4
	pixelDataSize := rowSize * size
	andMaskSize := andRowSize * size

	hdr := bitmapInfoHeader{
		Size:     40,
		Width:    int32(size),
		Height:   int32(size * 2), // double height for XOR+AND
		Planes:   1,
		BitCount: 32,
		SizeImage: uint32(pixelDataSize + andMaskSize),
	}
	binary.Write(&buf, binary.LittleEndian, hdr)

	// XOR mask (BGRA pixel data, bottom-up)
	// Create a shield-like shape: rounded square with slight border
	center := float64(size) / 2.0
	radius := float64(size) / 2.0 * 0.85 // 85% of half-size

	for y := 0; y < size; y++ {
		for x := 0; x < size; x++ {
			// Distance from center (use Chebyshev for rounded square feel)
			dx := abs(float64(x) - center + 0.5)
			dy := abs(float64(y) - center + 0.5)
			dist := max(dx, dy)

			if dist <= radius {
				// Inside the icon: solid color, fully opaque
				buf.WriteByte(b) // B
				buf.WriteByte(g) // G
				buf.WriteByte(r) // R
				buf.WriteByte(255) // A
			} else {
				// Outside: transparent
				buf.WriteByte(0)
				buf.WriteByte(0)
				buf.WriteByte(0)
				buf.WriteByte(0)
			}
		}
	}

	// AND mask (all zeros = fully controlled by alpha channel)
	andMask := make([]byte, andMaskSize)
	buf.Write(andMask)

	return buf.Bytes()
}

func abs(x float64) float64 {
	if x < 0 {
		return -x
	}
	return x
}

func max(a, b float64) float64 {
	if a > b {
		return a
	}
	return b
}

func main() {
	type iconSpec struct {
		name string
		r, g, b byte
	}

	icons := []iconSpec{
		{"connected.ico", 0x2E, 0xCC, 0x40},    // Green
		{"connecting.ico", 0xFF, 0x99, 0x00},     // Orange
		{"disconnected.ico", 0xE0, 0x3C, 0x3C},   // Red
	}

	traySizes := []int{16, 32, 48}
	trayDir := filepath.Join("internal", "tray")
	assetsDir := filepath.Join("assets", "icons")
	installerDir := "installer"

	os.MkdirAll(assetsDir, 0755)
	os.MkdirAll(installerDir, 0755)

	for _, icon := range icons {
		// Generate tray icons (16, 32, 48)
		data, err := generateICO(traySizes, icon.r, icon.g, icon.b)
		if err != nil {
			fmt.Fprintf(os.Stderr, "error generating %s: %v\n", icon.name, err)
			os.Exit(1)
		}
		trayPath := filepath.Join(trayDir, icon.name)
		if err := os.WriteFile(trayPath, data, 0644); err != nil {
			fmt.Fprintf(os.Stderr, "error writing %s: %v\n", trayPath, err)
			os.Exit(1)
		}
		fmt.Printf("Generated %s (%d bytes)\n", trayPath, len(data))

		// Copy to assets/icons/ for GoReleaser archive
		assetsPath := filepath.Join(assetsDir, icon.name)
		if err := os.WriteFile(assetsPath, data, 0644); err != nil {
			fmt.Fprintf(os.Stderr, "error writing %s: %v\n", assetsPath, err)
			os.Exit(1)
		}
		fmt.Printf("Generated %s (%d bytes)\n", assetsPath, len(data))
	}

	// Generate installer icon with 256px (multi-resolution for Windows Explorer)
	installerSizes := []int{16, 32, 48, 256}
	installerData, err := generateICO(installerSizes, 0x2E, 0xCC, 0x40) // Green (connected)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error generating installer icon: %v\n", err)
		os.Exit(1)
	}
	installerPath := filepath.Join(installerDir, "levoile.ico")
	if err := os.WriteFile(installerPath, installerData, 0644); err != nil {
		fmt.Fprintf(os.Stderr, "error writing %s: %v\n", installerPath, err)
		os.Exit(1)
	}
	fmt.Printf("Generated %s (%d bytes)\n", installerPath, len(installerData))
}
