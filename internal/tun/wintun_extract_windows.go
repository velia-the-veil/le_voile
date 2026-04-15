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

// embeddedWintunDLL contient la DLL Wintun signée Microsoft. Fournie par
// l'installeur / script build (placer wintun.dll 0.14.1 signée dans
// internal/tun/wintun/wintun.dll avant `go build`). Si vide, l'extraction
// échoue et New() retourne ErrUnavailable — cohérent avec dev local sans
// Wintun installé.
//
// La directive //go:embed est placée dans un fichier distinct pour rester
// optionnelle (cf. wintun_embed_windows.go).

var (
	extractOnce sync.Once
	extractErr  error
)

// ensureWintunDLL extrait wintun.dll vers %ProgramData%/LeVoile/ si absent
// ou si son SHA-256 diffère de la version embarquée, puis ajoute ce dossier
// au DLL search path via SetDllDirectory pour que wgtun.CreateTUN puisse la
// charger.
func ensureWintunDLL() error {
	extractOnce.Do(func() {
		extractErr = doEnsureWintunDLL()
	})
	return extractErr
}

func doEnsureWintunDLL() error {
	if len(embeddedWintunDLL) == 0 {
		return errors.New("wintun.dll non embarquée — placer la DLL signée dans internal/tun/wintun/wintun.dll avant build")
	}
	programData := os.Getenv("PROGRAMDATA")
	if programData == "" {
		programData = `C:\ProgramData`
	}
	dir := filepath.Join(programData, "LeVoile")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("mkdir %s: %w", dir, err)
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
			return fmt.Errorf("write %s: %w", tmp, err)
		}
		if err := os.Rename(tmp, dst); err != nil {
			os.Remove(tmp)
			return fmt.Errorf("rename %s → %s: %w", tmp, dst, err)
		}
	}

	// Ajoute %ProgramData%/LeVoile/ au DLL search path pour que
	// LoadLibrary("wintun.dll") résolve ce chemin, y compris en dev local
	// (hors installation NSIS qui copierait wintun.dll dans Program Files).
	if err := windows.SetDllDirectory(dir); err != nil {
		return fmt.Errorf("SetDllDirectory %s: %w", dir, err)
	}
	return nil
}
