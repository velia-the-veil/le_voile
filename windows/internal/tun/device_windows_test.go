//go:build windows

package tun

import (
	"errors"
	"testing"
)

// TestNew_WithoutWintunDLL vérifie que New retourne ErrUnavailable (et non
// un panic) quand wintun.dll n'est pas embarquée — cas dev local typique.
// Si le projet a injecté la DLL, ce test est skip.
func TestNew_WithoutWintunDLL(t *testing.T) {
	if len(embeddedWintunDLL) != 0 {
		t.Skip("wintun.dll embarquée — test d'absence non applicable")
	}
	_, err := New("levoiletst", DefaultMTU)
	if err == nil {
		t.Fatal("New sans Wintun DLL doit échouer")
	}
	if !errors.Is(err, ErrUnavailable) {
		t.Errorf("err = %v, attendu ErrUnavailable", err)
	}
}

func TestCleanupOrphan_IdempotentWithoutDLL(t *testing.T) {
	// Sans Wintun, pas d'orphan possible → cleanup doit être nil.
	if len(embeddedWintunDLL) != 0 {
		t.Skip("wintun.dll embarquée — ce test suppose absence")
	}
	if err := CleanupOrphan("levoiletst"); err != nil {
		t.Errorf("CleanupOrphan sans DLL doit être nil, got: %v", err)
	}
}
