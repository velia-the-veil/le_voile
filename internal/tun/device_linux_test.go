//go:build linux

package tun

import (
	"errors"
	"os"
	"testing"
	"time"
)

// requireCapNetAdmin skip le test si le process ne peut pas créer une TUN.
func requireCapNetAdmin(t *testing.T) {
	t.Helper()
	if os.Geteuid() != 0 {
		// CAP_NET_ADMIN peut être fourni via capabilities sans UID=0 (systemd
		// AmbientCapabilities), mais détecter cela proprement demande
		// /proc/self/status ; un skip simple est suffisant pour CI.
		t.Skip("nécessite root ou CAP_NET_ADMIN (systemd)")
	}
	if _, err := os.Stat("/dev/net/tun"); err != nil {
		t.Skipf("/dev/net/tun indisponible: %v", err)
	}
}

func TestNew_LifecycleLinux(t *testing.T) {
	requireCapNetAdmin(t)

	// Nom unique pour éviter les collisions en CI parallèle.
	name := "levoiletst"
	// Cleanup préalable au cas où un run précédent ait laissé une interface.
	_ = CleanupOrphan(name)

	dev, err := New(name, DefaultMTU)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if got := dev.Name(); got != name {
		t.Errorf("Name = %q, want %q", got, name)
	}
	if got := dev.MTU(); got != DefaultMTU {
		t.Errorf("MTU = %d, want %d", got, DefaultMTU)
	}

	// Vérifie présence via /sys/class/net.
	if _, err := os.Stat("/sys/class/net/" + name); err != nil {
		t.Fatalf("interface %s absente après New: %v", name, err)
	}

	// Close détruit l'interface.
	if err := dev.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	// Close idempotent.
	if err := dev.Close(); err != nil {
		t.Fatalf("Close idempotent: %v", err)
	}

	// Attendre max 1s la disparition via /sys/class/net.
	deadline := time.Now().Add(1 * time.Second)
	for {
		_, err := os.Stat("/sys/class/net/" + name)
		if errors.Is(err, os.ErrNotExist) {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("interface %s toujours présente après Close", name)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func TestCleanupOrphan_NoInterface(t *testing.T) {
	// Idempotent même sans privilèges : stat échoue proprement sans créer
	// de socket netlink.
	if err := CleanupOrphan("nonexistent999"); err != nil {
		t.Fatalf("CleanupOrphan sur interface absente doit être nil: %v", err)
	}
}

func TestCleanupOrphan_CrashRecovery(t *testing.T) {
	requireCapNetAdmin(t)

	name := "levoiletst"
	_ = CleanupOrphan(name)

	// Crée une interface sans appeler Close() — simule un crash.
	dev, err := New(name, DefaultMTU)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	// NOTE : on ferme quand même en fin de test via CleanupOrphan ;
	// la fermeture du fd par wireguard/tun via CreateTUN sans IFF_PERSIST
	// détruirait l'interface au GC. Pour tester crash-recovery on garde
	// une référence forte.
	_ = dev

	// CleanupOrphan doit compléter < 5s (NFR17).
	start := time.Now()
	if err := CleanupOrphan(name); err != nil {
		t.Fatalf("CleanupOrphan: %v", err)
	}
	elapsed := time.Since(start)
	if elapsed > 5*time.Second {
		t.Errorf("CleanupOrphan a pris %v, NFR17 impose < 5s", elapsed)
	}

	// Vérifie que l'interface est bien partie.
	if _, err := os.Stat("/sys/class/net/" + name); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("interface %s toujours présente après CleanupOrphan", name)
	}
}
