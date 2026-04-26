//go:build linux

// Package tun crée et détruit l'interface virtuelle levoile0 (TUN Linux /
// Wintun Windows) via golang.zx2c4.com/wireguard/tun. L'API expose des
// opérations Read/Write de paquets IP bruts et découple l'orchestrateur
// service de la bibliothèque sous-jacente.
package tun

import (
	"errors"
	"fmt"
	"regexp"
)

const (
	// DefaultName est le nom d'interface utilisé par l'architecture.
	DefaultName = "levoile0"
	// DefaultMTU est la MTU par défaut (architecture.md — révision 2026-04-15).
	DefaultMTU = 1420
	// MinMTU/MaxMTU bornent la MTU acceptée par New.
	MinMTU = 576
	MaxMTU = 9000
)

var (
	// ErrPermission indique un manque de privilèges (CAP_NET_ADMIN Linux /
	// LocalSystem Windows).
	ErrPermission = errors.New("tun: privilèges insuffisants (CAP_NET_ADMIN Linux / LocalSystem Windows)")
	// ErrUnavailable indique l'absence du backend OS (/dev/net/tun Linux,
	// wintun.dll Windows).
	ErrUnavailable = errors.New("tun: backend OS indisponible")

	nameRe = regexp.MustCompile(`^[a-z][a-z0-9]{0,14}$`)
)

// Device représente une interface TUN/Wintun ouverte. Toutes les méthodes
// sont sûres pour un appel unique concurrent Read/Write (une goroutine
// lecture, une goroutine écriture — pattern wireguard-go). NE JAMAIS lancer
// deux Read ou deux Write concurrents.
type Device interface {
	// Read lit un paquet IP depuis l'interface. Retourne le nombre d'octets
	// écrits dans buf.
	Read(buf []byte) (int, error)
	// Write envoie un paquet IP vers l'interface.
	Write(pkt []byte) (int, error)
	// Name retourne le nom effectif de l'interface (levoile0 ou variante si
	// OS a renommé).
	Name() string
	// MTU retourne la MTU configurée.
	MTU() int
	// Close détruit l'interface. Idempotent.
	Close() error
}

// validateParams rejette un nom ou une MTU hors bornes avant tout appel OS.
func validateParams(name string, mtu int) error {
	if !nameRe.MatchString(name) {
		return fmt.Errorf("tun: nom invalide %q (regex ^[a-z][a-z0-9]{0,14}$)", name)
	}
	if mtu < MinMTU || mtu > MaxMTU {
		return fmt.Errorf("tun: MTU %d hors bornes [%d,%d]", mtu, MinMTU, MaxMTU)
	}
	return nil
}
