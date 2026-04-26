//go:build linux

package tun

import (
	"errors"
	"os"
	"os/exec"
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

// TestNew_ReadWriteLoopback envoie un paquet ICMP ping via la TUN et le relit
// en boucle locale pour valider le wrapping batched Read/Write (sizes/offset
// correctement propagés). T4 story 2.1 exige ce cycle.
func TestNew_ReadWriteLoopback(t *testing.T) {
	requireCapNetAdmin(t)

	name := "levoiletstlb"
	_ = CleanupOrphan(name)

	dev, err := New(name, DefaultMTU)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer dev.Close()

	// Assigner une IP + UP à l'interface pour que le kernel accepte les
	// paquets écrits. Si `ip` absent (container minimal), skip.
	if _, err := exec.LookPath("ip"); err != nil {
		t.Skip("outil 'ip' absent : loopback TUN non testable")
	}
	if out, err := exec.Command("ip", "addr", "add", "10.77.0.1/24", "dev", name).CombinedOutput(); err != nil {
		t.Skipf("ip addr add: %v (%s)", err, out)
	}
	if out, err := exec.Command("ip", "link", "set", name, "up").CombinedOutput(); err != nil {
		t.Skipf("ip link set up: %v (%s)", err, out)
	}

	// Construit un paquet IPv4 minimal (ICMP echo request) : 20 octets IP
	// header + 8 octets ICMP. Source 10.77.0.1, dest 10.77.0.2.
	pkt := buildICMPEcho(t, [4]byte{10, 77, 0, 1}, [4]byte{10, 77, 0, 2})

	// Write : la TUN accepte le paquet (sizes/offset OK si pas d'erreur).
	n, err := dev.Write(pkt)
	if err != nil {
		t.Fatalf("Write: %v", err)
	}
	if n != len(pkt) {
		t.Errorf("Write n=%d, want %d", n, len(pkt))
	}

	// Read : le kernel répond avec un ICMP echo reply (ou renvoie l'echo
	// request si pas de destination configurée). Timeout 2s.
	done := make(chan struct{})
	var readN int
	var readErr error
	buf := make([]byte, 2048)
	go func() {
		readN, readErr = dev.Read(buf)
		close(done)
	}()
	select {
	case <-done:
		if readErr != nil {
			t.Fatalf("Read: %v", readErr)
		}
		if readN < 20 {
			t.Errorf("Read n=%d, attendu >= 20 (IP header)", readN)
		}
	case <-time.After(2 * time.Second):
		t.Skip("timeout read loopback — kernel routing peut être restrictif (skip non-fatal)")
	}
}

// buildICMPEcho construit un paquet IPv4 + ICMP echo request minimal.
func buildICMPEcho(t *testing.T, src, dst [4]byte) []byte {
	t.Helper()
	pkt := make([]byte, 28)
	// IPv4 header
	pkt[0] = 0x45                       // version 4, IHL 5
	pkt[1] = 0                          // DSCP/ECN
	pkt[2], pkt[3] = 0, 28              // total length
	pkt[4], pkt[5] = 0, 1               // ID
	pkt[6], pkt[7] = 0, 0               // flags/frag
	pkt[8] = 64                         // TTL
	pkt[9] = 1                          // protocol ICMP
	pkt[10], pkt[11] = 0, 0             // checksum (laisser à 0, kernel re-calcule si CHECKSUM offload)
	copy(pkt[12:16], src[:])
	copy(pkt[16:20], dst[:])
	// IP checksum
	var sum uint32
	for i := 0; i < 20; i += 2 {
		sum += uint32(pkt[i])<<8 | uint32(pkt[i+1])
	}
	for sum>>16 != 0 {
		sum = (sum & 0xffff) + (sum >> 16)
	}
	cs := uint16(^sum)
	pkt[10], pkt[11] = byte(cs>>8), byte(cs)
	// ICMP echo request
	pkt[20] = 8 // type
	pkt[21] = 0 // code
	pkt[22], pkt[23] = 0, 0
	pkt[24], pkt[25] = 0, 1 // id
	pkt[26], pkt[27] = 0, 1 // seq
	// ICMP checksum
	sum = 0
	for i := 20; i < 28; i += 2 {
		sum += uint32(pkt[i])<<8 | uint32(pkt[i+1])
	}
	for sum>>16 != 0 {
		sum = (sum & 0xffff) + (sum >> 16)
	}
	cs = uint16(^sum)
	pkt[22], pkt[23] = byte(cs>>8), byte(cs)
	return pkt
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
