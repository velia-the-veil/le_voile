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

// ensureWintunDLL extrait wintun.dll vers %ProgramData%/LeVoile/ si absent
// ou si son SHA-256 diffère de la version embarquée, puis la pré-charge
// dans le processus via LoadLibraryEx(LOAD_WITH_ALTERED_SEARCH_PATH). Cette
// approche évite de modifier le DLL search path global du processus (ce que
// SetDllDirectory ferait — incompatible avec d'autres LoadLibrary indirects
// comme kardianos/service, go-winio, quic-go).
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

func ensureWintunDLL() error {
	extractOnce.Do(func() {
		extractErr = doEnsureWintunDLL()
	})
	return extractErr
}

func doEnsureWintunDLL() error {
	if len(embeddedWintunDLL) == 0 {
		return errors.New("tun: wintun.dll not embedded — place the signed DLL at internal/tun/wintun/wintun.dll before build (see README)")
	}
	programData := os.Getenv("PROGRAMDATA")
	if programData == "" {
		programData = `C:\ProgramData`
	}
	dir := filepath.Join(programData, "LeVoile")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("tun: mkdir %s: %w", dir, err)
	}
	dst := filepath.Join(dir, "wintun.dll")

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

	// Pré-charge wintun.dll sans modifier le DLL search path global. Le
	// handle est intentionnellement non libéré — la DLL doit rester résidente
	// pour la vie du processus (wireguard/tun la réutilisera par nom).
	if _, err := windows.LoadLibraryEx(dst, 0, loadWithAlteredSearchPath); err != nil {
		return fmt.Errorf("tun: LoadLibraryEx %s: %w", dst, err)
	}
	return nil
}
