//go:build linux

package tun

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"sync"

	wgtun "golang.zx2c4.com/wireguard/tun"
)

// New crée une interface TUN Linux via /dev/net/tun. Requiert CAP_NET_ADMIN
// (fourni par systemd AmbientCapabilities ou sudo en dev local).
func New(name string, mtu int) (Device, error) {
	if err := validateParams(name, mtu); err != nil {
		return nil, err
	}
	dev, err := wgtun.CreateTUN(name, mtu)
	if err != nil {
		// /dev/net/tun absent → module tun non chargé.
		if errors.Is(err, os.ErrNotExist) {
			return nil, fmt.Errorf("%w: %v", ErrUnavailable, err)
		}
		// EPERM / EACCES → capability manquante.
		if errors.Is(err, os.ErrPermission) {
			return nil, fmt.Errorf("%w: %v", ErrPermission, err)
		}
		return nil, fmt.Errorf("tun: CreateTUN: %w", err)
	}
	effName, err := dev.Name()
	if err != nil {
		dev.Close()
		return nil, fmt.Errorf("tun: Name: %w", err)
	}
	effMTU, err := dev.MTU()
	if err != nil {
		dev.Close()
		return nil, fmt.Errorf("tun: MTU: %w", err)
	}
	// Bring admin state UP. wgtun.CreateTUN creates the device but leaves it
	// DOWN, ce qui fait échouer routing.Setup avec « Device for nexthop is
	// not up » sur `ip route add 0.0.0.0/0 dev levoile0`. Sans ça, le service
	// remontait une pile L3 partielle (TUN existant mais inutilisable) et le
	// kill-switch s'armait sur du vide → internet bloqué jusqu'au teardown.
	if out, err := exec.Command("ip", "link", "set", "dev", effName, "up").CombinedOutput(); err != nil {
		dev.Close()
		return nil, fmt.Errorf("tun: link up: %s: %w", strings.TrimSpace(string(out)), err)
	}
	// Limiter GSO à la MTU pour éviter que le kernel génère des frames TSO
	// (≤64 KB) destinées à notre TUN. wgtun active TUN_F_TSO4/6/USO via
	// TUNSETOFFLOAD à la création (tun_linux.go:532) → le kernel a le droit
	// d'envoyer des TSO frames qui passeraient ensuite par gsoSplit côté
	// Read, demandant un slice de plusieurs buffers que notre wrapper
	// single-packet ne fournit pas (ErrTooManySegments → reader exit →
	// outbound pump mort → kill-switch up + outbound mort = internet coupé
	// après quelques secondes de download). gso_max_segs=1 + gso_max_size=mtu
	// désactivent la coalesce sans toucher à la négociation TUNSETOFFLOAD
	// interne de wgtun (vnetHdr reste actif, le préfixe virtio est toujours
	// requis côté Write — cf. wgDevice.Write).
	mtuStr := fmt.Sprintf("%d", effMTU)
	if out, err := exec.Command("ip", "link", "set", "dev", effName, "gso_max_size", mtuStr, "gso_max_segs", "1").CombinedOutput(); err != nil {
		// Non-fatal : sur kernels/iproute2 trop anciens (<6.0) la syntaxe
		// peut être absente. Le service tourne, juste avec un risque résiduel
		// si le kernel produit du TSO. À surfacer en log opérationnel.
		fmt.Fprintf(os.Stderr, "tun: WARN gso_max disable failed (%s): %v — TSO frames may break the reader if kernel coalesces\n", strings.TrimSpace(string(out)), err)
	}
	return &wgDevice{inner: dev, name: effName, mtu: effMTU}, nil
}

// wgDevice wrappe wgtun.Device pour exposer une API Read/Write single-packet.
type wgDevice struct {
	inner    wgtun.Device
	name     string
	mtu      int
	closed   bool
	closeErr error
	mu       sync.Mutex
}

func (d *wgDevice) Read(buf []byte) (int, error) {
	// API batched : 1 buffer, offset 0. offset tient compte du header
	// virtio sur certaines plateformes — wireguard/tun gère ça en interne.
	bufs := [][]byte{buf}
	sizes := []int{0}
	n, err := d.inner.Read(bufs, sizes, 0)
	if err != nil {
		return 0, err
	}
	if n == 0 {
		return 0, nil
	}
	return sizes[0], nil
}

// virtioNetHdrLen est la taille du préfixe virtio_net_hdr exigé par wgtun.Write
// quand l'offload GRO/GSO est actif (cas par défaut sur Linux ≥4.x avec
// TUN_F_CSUM/TSO/USO supportés). wgtun écrit le header virtio dans
// bufs[i][offset-virtioNetHdrLen:] et lit le paquet IP depuis bufs[i][offset:],
// donc tout buffer passé à Write doit avoir au moins 10 octets de marge en
// tête. Notre wrapper alloue un buffer préfixé à chaque appel — coût d'une
// copie par paquet contre le gain de l'offload sur le path Read côté kernel
// (les Initial QUIC arrivent en clair, mais les bursts TCP sont coalescés).
//
// Constante miroir de tun/offload_linux.go:virtioNetHdrLen (sizeof virtioNetHdr
// = 10 octets, stable depuis 2017 dans le kernel + binding wgtun).
const virtioNetHdrLen = 10

func (d *wgDevice) Write(pkt []byte) (int, error) {
	// Allouer un buffer avec préfixe virtio_net_hdr. wgtun.Write avec
	// vnetHdr=true exige offset >= virtioNetHdrLen — passer offset=0 fait
	// échouer handleGRO avec « invalid offset » (offload_linux.go:867) et
	// renvoie ce wrapper en boucle infinie côté pump. Sans vnetHdr (cas rare,
	// kernel sans offload), wgtun écrit bufs[i][offset:] directement vers le
	// fd TUN — passer offset=10 reste correct car notre IP packet commence
	// bien à l'offset 10 du buffer alloué.
	buf := make([]byte, virtioNetHdrLen+len(pkt))
	copy(buf[virtioNetHdrLen:], pkt)
	bufs := [][]byte{buf}
	n, err := d.inner.Write(bufs, virtioNetHdrLen)
	if err != nil {
		return 0, err
	}
	if n == 0 {
		return 0, nil
	}
	return len(pkt), nil
}

func (d *wgDevice) Name() string { return d.name }
func (d *wgDevice) MTU() int     { return d.mtu }

// Close est idempotent : retourne l'erreur du premier appel à chaque appel
// suivant (sans relancer inner.Close), pour éviter de masquer un échec de
// cleanup que l'appelant n'aurait vu qu'au premier Close.
func (d *wgDevice) Close() error {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.closed {
		return d.closeErr
	}
	d.closed = true
	d.closeErr = d.inner.Close()
	return d.closeErr
}
