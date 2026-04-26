//go:build e2e

package routing

import (
	"errors"
	"net"
	"runtime"
	"testing"
	"time"
)

// TestE2E_FullCycle exécute un cycle Setup → vérification → Teardown →
// vérification restauration. Nécessite root/admin et une interface TUN.
func TestE2E_FullCycle(t *testing.T) {
	skipIfUnprivileged(t)

	rm := New()
	relay := net.ParseIP("198.51.100.1")

	gw, iface, err := CaptureOriginalRoute()
	if err != nil {
		t.Skipf("cannot capture route: %v", err)
	}
	t.Logf("original: gateway=%s iface=%s", gw, iface)

	tunName := ensureTestTUN(t)
	defer cleanupTestTUN(t, tunName)

	// --- Setup ---
	if err := rm.Setup(tunName, relay, gw, iface); err != nil {
		t.Fatalf("Setup: %v", err)
	}

	saved := rm.Saved()
	if saved == nil {
		t.Fatal("Saved() nil after Setup")
	}
	if !saved.OrigGateway.Equal(gw) {
		t.Errorf("OrigGateway = %v, want %v", saved.OrigGateway, gw)
	}
	if saved.TUNName != tunName {
		t.Errorf("TUNName = %q, want %q", saved.TUNName, tunName)
	}

	verifyRoutesPresent(t)

	// --- Teardown ---
	if err := rm.Teardown(); err != nil {
		t.Fatalf("Teardown: %v", err)
	}

	if rm.Saved() != nil {
		t.Error("Saved() should be nil after Teardown")
	}

	verifyRoutesAbsent(t)
}

// TestE2E_CrashCleanup simule un crash : Setup puis Cleanup sans Teardown.
func TestE2E_CrashCleanup(t *testing.T) {
	skipIfUnprivileged(t)

	rm1 := New()
	relay := net.ParseIP("198.51.100.1")

	gw, iface, err := CaptureOriginalRoute()
	if err != nil {
		t.Skipf("cannot capture route: %v", err)
	}

	tunName := ensureTestTUN(t)
	defer cleanupTestTUN(t, tunName)

	if err := rm1.Setup(tunName, relay, gw, iface); err != nil {
		t.Fatalf("Setup: %v", err)
	}

	// Simuler un crash : on ne fait PAS Teardown.
	rm2 := New()
	start := time.Now()
	if err := rm2.Cleanup(); err != nil {
		t.Fatalf("Cleanup: %v", err)
	}
	elapsed := time.Since(start)
	if elapsed > 5*time.Second {
		t.Errorf("Cleanup took %v, NFR17 requires < 5s", elapsed)
	}

	_ = rm1.Teardown()
}

// TestE2E_DoubleSetup vérifie qu'un second Setup retourne ErrAlreadyActive.
func TestE2E_DoubleSetup(t *testing.T) {
	skipIfUnprivileged(t)

	rm := New()
	relay := net.ParseIP("198.51.100.1")

	gw, iface, err := CaptureOriginalRoute()
	if err != nil {
		t.Skipf("cannot capture route: %v", err)
	}

	tunName := ensureTestTUN(t)
	defer cleanupTestTUN(t, tunName)

	if err := rm.Setup(tunName, relay, gw, iface); err != nil {
		t.Fatalf("first Setup: %v", err)
	}
	defer func() { _ = rm.Teardown() }()

	err = rm.Setup(tunName, relay, gw, iface)
	if !errors.Is(err, ErrAlreadyActive) {
		t.Fatalf("second Setup = %v, want ErrAlreadyActive", err)
	}
}

// --- OS-specific helpers (compile on both platforms) ---

func skipIfUnprivileged(t *testing.T) {
	t.Helper()
	if runtime.GOOS == "linux" {
		skipIfNotRoot(t)
	} else if runtime.GOOS == "windows" {
		skipIfNotAdmin(t)
	}
}
