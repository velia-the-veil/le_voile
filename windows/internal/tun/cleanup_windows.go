//go:build windows

package tun

import (
	"fmt"

	"golang.zx2c4.com/wintun"
)

// CleanupOrphan détruit un adaptateur Wintun résiduel portant le nom donné.
// Idempotent : retourne nil si l'adaptateur n'existe pas. NFR17 < 5s.
func CleanupOrphan(name string) error {
	if !nameRe.MatchString(name) {
		return fmt.Errorf("tun: nom invalide %q", name)
	}
	if err := ensureWintunDLL(); err != nil {
		// Sans Wintun installé, il ne peut pas y avoir d'orphan Wintun →
		// traiter comme idempotent.
		return nil
	}
	adapter, err := wintun.OpenAdapter(name)
	if err != nil {
		// Wintun retourne ERROR_FILE_NOT_FOUND si l'adaptateur n'existe pas.
		// On considère toute erreur d'ouverture comme "pas d'orphan à nettoyer".
		return nil
	}
	if err := adapter.Close(); err != nil {
		return fmt.Errorf("tun: cleanup %s: close adapter: %w", name, err)
	}
	return nil
}
