//go:build ignore

// gen_icons generates Le Voile ICO files.
// Tray: colored circle (status) with blue "V".
// Taskbar/installer: blue circle with green "V".
// Usage: go run tools/gen_icons.go
package main

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"math"
	"os"
	"path/filepath"
)

type iconDir struct {
	Reserved uint16
	Type     uint16
	Count    uint16
}

type iconDirEntry struct {
	Width       byte
	Height      byte
	ColorCount  byte
	Reserved    byte
	Planes      uint16
	BitCount    uint16
	SizeInBytes uint32
	Offset      uint32
}

type bitmapInfoHeader struct {
	Size          uint32
	Width         int32
	Height        int32
	Planes        uint16
	BitCount      uint16
	Compression   uint32
	SizeImage     uint32
	XPelsPerMeter int32
	YPelsPerMeter int32
	ClrUsed       uint32
	ClrImportant  uint32
}

type rgba struct{ r, g, b, a byte }

var transparent = rgba{0, 0, 0, 0}

// Colors
var blue = rgba{0x1a, 0x6f, 0xc4, 255}
var green = rgba{0x4a, 0xde, 0x80, 255}
var orange = rgba{0xfb, 0x92, 0x3c, 255}
var red = rgba{0xff, 0x3c, 0x3c, 255}

func generateICO(sizes []int, drawFn func(size int) []rgba) ([]byte, error) {
	var buf bytes.Buffer
	headerSize := 6 + 16*len(sizes)

	type imgEntry struct{ data []byte }
	images := make([]imgEntry, len(sizes))
	for i, size := range sizes {
		images[i].data = pixelsToBMP(size, drawFn(size))
	}

	dir := iconDir{Type: 1, Count: uint16(len(sizes))}
	binary.Write(&buf, binary.LittleEndian, dir)

	offset := uint32(headerSize)
	for i, size := range sizes {
		w, h := byte(size), byte(size)
		if size == 256 {
			w, h = 0, 0
		}
		entry := iconDirEntry{
			Width: w, Height: h, Planes: 1, BitCount: 32,
			SizeInBytes: uint32(len(images[i].data)), Offset: offset,
		}
		binary.Write(&buf, binary.LittleEndian, entry)
		offset += uint32(len(images[i].data))
	}
	for _, img := range images {
		buf.Write(img.data)
	}
	return buf.Bytes(), nil
}

func pixelsToBMP(size int, pixels []rgba) []byte {
	var buf bytes.Buffer
	andRowSize := ((size + 31) / 32) * 4
	pixelDataSize := size * 4 * size
	andMaskSize := andRowSize * size

	hdr := bitmapInfoHeader{
		Size: 40, Width: int32(size), Height: int32(size * 2),
		Planes: 1, BitCount: 32, SizeImage: uint32(pixelDataSize + andMaskSize),
	}
	binary.Write(&buf, binary.LittleEndian, hdr)

	// BMP is bottom-up
	for y := size - 1; y >= 0; y-- {
		for x := 0; x < size; x++ {
			c := pixels[y*size+x]
			buf.WriteByte(c.b)
			buf.WriteByte(c.g)
			buf.WriteByte(c.r)
			buf.WriteByte(c.a)
		}
	}
	buf.Write(make([]byte, andMaskSize))
	return buf.Bytes()
}

// drawCircleWithV draws a filled circle of bgColor with a "V" letter in fgColor.
func drawCircleWithV(size int, bgColor, fgColor rgba) []rgba {
	px := make([]rgba, size*size)
	s := float64(size)
	cx, cy := s/2, s/2
	radius := s/2 - 0.5

	for y := 0; y < size; y++ {
		for x := 0; x < size; x++ {
			fx, fy := float64(x)+0.5, float64(y)+0.5
			dist := math.Hypot(fx-cx, fy-cy)

			if dist > radius {
				px[y*size+x] = transparent
				continue
			}

			if inVLetter(fx, fy, s) {
				px[y*size+x] = fgColor
			} else {
				px[y*size+x] = bgColor
			}
		}
	}
	return px
}

// inVLetter tests if (fx,fy) is inside a "V" shape centered in a square of side s.
func inVLetter(fx, fy, s float64) bool {
	// V extends from top 25% to bottom 78% of the icon
	topY := s * 0.22
	botY := s * 0.78
	if fy < topY || fy > botY {
		return false
	}

	cx := s / 2
	// Normalize progress from top to bottom: 0..1
	t := (fy - topY) / (botY - topY)

	// Stroke thickness proportional to size
	thick := s * 0.09
	if thick < 1.5 {
		thick = 1.5
	}

	// Left stroke goes from (-0.22*s, topY) to (cx, botY)
	leftX := cx - (s * 0.22 * (1 - t))
	// Right stroke goes from (+0.22*s, topY) to (cx, botY)
	rightX := cx + (s * 0.22 * (1 - t))

	if math.Abs(fx-leftX) < thick || math.Abs(fx-rightX) < thick {
		return true
	}

	// Fill the bottom area between the two strokes where they meet
	if t > 0.85 && fx >= leftX-thick && fx <= rightX+thick {
		return true
	}

	return false
}

func main() {
	traySizes := []int{16, 32, 48}
	allSizes := []int{16, 32, 48, 256}

	type iconSpec struct {
		name    string
		bg, fg  rgba
		sizes   []int
	}

	// Tray icons: status-colored circle + blue V
	// Taskbar/installer icon: blue circle + green V
	trayIcons := []iconSpec{
		{"connected.ico", green, blue, traySizes},
		{"connecting.ico", orange, blue, traySizes},
		{"disconnected.ico", red, blue, traySizes},
	}

	appIcon := iconSpec{"levoile.ico", blue, green, allSizes}

	dirs := []string{
		filepath.Join("internal", "ui", "icons"),
		filepath.Join("internal", "tray"),
	}

	// Generate tray icons
	for _, icon := range trayIcons {
		bg, fg := icon.bg, icon.fg
		data, err := generateICO(icon.sizes, func(size int) []rgba { return drawCircleWithV(size, bg, fg) })
		if err != nil {
			fmt.Fprintf(os.Stderr, "error: %v\n", err)
			os.Exit(1)
		}
		for _, dir := range dirs {
			os.MkdirAll(dir, 0755)
			path := filepath.Join(dir, icon.name)
			os.WriteFile(path, data, 0644)
			fmt.Printf("Generated %s (%d bytes)\n", path, len(data))
		}
	}

	// Generate app icon (taskbar + installer)
	{
		bg, fg := appIcon.bg, appIcon.fg
		data, err := generateICO(appIcon.sizes, func(size int) []rgba { return drawCircleWithV(size, bg, fg) })
		if err != nil {
			fmt.Fprintf(os.Stderr, "error: %v\n", err)
			os.Exit(1)
		}
		for _, dir := range dirs {
			os.MkdirAll(dir, 0755)
			path := filepath.Join(dir, appIcon.name)
			os.WriteFile(path, data, 0644)
			fmt.Printf("Generated %s (%d bytes)\n", path, len(data))
		}
		// Installer icon
		installerPath := filepath.Join("installer", "levoile.ico")
		os.WriteFile(installerPath, data, 0644)
		fmt.Printf("Generated %s (%d bytes)\n", installerPath, len(data))
	}
}
