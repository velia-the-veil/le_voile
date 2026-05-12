//go:build windows

package tun

import (
	"crypto/sha256"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sync"

	"golang.org/x/sys/windows"
)

// ensureWintunDLL résout et pré-charge wintun.dll dans le processus via
// LoadLibraryEx(LOAD_WITH_ALTERED_SEARCH_PATH). Cette approche évite de
// modifier le DLL search path global du processus (ce que SetDllDirectory
// ferait — incompatible avec d'autres LoadLibrary indirects comme
// kardianos/service, go-winio, quic-go).
//
// Priorité de résolution (Story 7.1) :
//  1. <dir(os.Executable())>\wintun.dll — installée par NSIS dans Program Files
//  2. %ProgramData%\LeVoile\wintun.dll — cache d'une extraction précédente
//  3. Embed → extraction vers (2) puis chargement
//
// Une fois la DLL préchargée, wireguard/tun l'utilisera directement par nom
// sans relancer de résolution.

var (
	extractOnce sync.Once
	extractErr  error
)

// LOAD_WITH_ALTERED_SEARCH_PATH : constante Windows pour LoadLibraryEx —
// interprète lpFileName comme un chemin absolu et utilise son dossier
// comme début du search path, sans toucher au search path global.
const loadWithAlteredSearchPath = 0x00000008

// exeDir is a package-level seam so tests can simulate the install layout
// without spawning a fake executable. Default returns dir(os.Executable()).
var exeDir = func() string {
	exe, err := os.Executable()
	if err != nil {
		return ""
	}
	return filepath.Dir(exe)
}

func ensureWintunDLL() error {
	extractOnce.Do(func() {
		extractErr = doEnsureWintunDLL()
	})
	return extractErr
}

// loadDLL pre-loads a wintun.dll from an absolute path. Returns nil if it
// loaded successfully, an error otherwise. The handle is intentionally not
// released — the DLL must stay resident for the life of the process.
//
// Package-level seam so tests can verify the resolution-priority order
// (Story 7.1) without actually calling LoadLibraryEx on a real DLL file.
var loadDLL = func(path string) error {
	if _, err := windows.LoadLibraryEx(path, 0, loadWithAlteredSearchPath); err != nil {
		return fmt.Errorf("tun: LoadLibraryEx %s: %w", path, err)
	}
	return nil
}

func doEnsureWintunDLL() error {
	// (1) NSIS-installed DLL alongside the executable (Program Files\LeVoile).
	// Most Windows production paths land here. No filesystem write needed.
	if dir := exeDir(); dir != "" {
		candidate := filepath.Join(dir, "wintun.dll")
		if _, err := os.Stat(candidate); err == nil {
			return loadDLL(candidate)
		}
	}

	// (2) Cached extraction from a previous run.
	programData := os.Getenv("PROGRAMDATA")
	if programData == "" {
		programData = `C:\ProgramData`
	}
	dir := filepath.Join(programData, "LeVoile")
	dst := filepath.Join(dir, "wintun.dll")

	if len(embeddedWintunDLL) == 0 {
		// No embed: the cached file is our last hope. If absent, hard fail.
		if _, err := os.Stat(dst); err == nil {
			return loadDLL(dst)
		}
		return errors.New("tun: wintun.dll not found (expected at <exe dir>\\wintun.dll, %ProgramData%\\LeVoile\\wintun.dll, or embedded)")
	}

	// (3) Embed → write to cache (atomic) → load. Only writes when missing
	// or hash mismatch — avoids touching disk on every start.
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("tun: mkdir %s: %w", dir, err)
	}

	need := true
	if f, err := os.Open(dst); err == nil {
		h := sha256.New()
		_, cerr := io.Copy(h, f)
		f.Close()
		if cerr == nil {
			onDisk := h.Sum(nil)
			want := sha256.Sum256(embeddedWintunDLL)
			if string(onDisk) == string(want[:]) {
				need = false
			}
		}
	}
	if need {
		tmp := dst + ".tmp"
		if err := os.WriteFile(tmp, embeddedWintunDLL, 0o644); err != nil {
			return fmt.Errorf("tun: write %s: %w", tmp, err)
		}
		if err := os.Rename(tmp, dst); err != nil {
			os.Remove(tmp)
			return fmt.Errorf("tun: rename %s → %s: %w", tmp, dst, err)
		}
	}

	return loadDLL(dst)
}
