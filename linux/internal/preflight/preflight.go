//go:build linux

// Package preflight vérifie l'état du système avant de créer l'interface
// levoile0 et de monter le tunnel. Le seul contrôle implémenté pour l'instant
// (story 2.3) est la détection d'un VPN concurrent déjà actif — le but est
// de refuser de démarrer plutôt que produire un routage incohérent entre deux
// tunnels (routing loops, DNS contradictoires, kill switches concurrents).
//
// Le package est purement read-only : il n'écrit ni interface, ni route, ni
// règle firewall. En cas de détection, l'appelant doit court-circuiter la
// séquence Connect (aucun rollback à prévoir).
package preflight

import (
	"fmt"
	"strings"
)

// OwnInterfaceName est le nom de l'interface TUN/Wintun créée par le service.
// Toujours exclue du scan (faux positif trivial si 2.1 a déjà créé l'interface
// ou si le crash-recovery l'a reprise).
const OwnInterfaceName = "levoile0"

// linuxVPNPrefixes est la liste des préfixes de noms d'interfaces qui
// indiquent qu'un VPN concurrent est actif sur Linux. Matching case-insensitive
// par préfixe. Tous en minuscules (pré-normalisé).
//   - tun/tap/utun : OpenVPN, WireGuard-go, divers
//   - wg/wireguard : WireGuard kernel
//   - ppp          : L2TP/PPTP
//   - gpd          : Palo Alto GlobalProtect / Cisco AnyConnect GP
//   - ipsec        : strongSwan / Libreswan
var linuxVPNPrefixes = []string{"tun", "tap", "utun", "wg", "wireguard", "ppp", "gpd", "ipsec"}

// windowsVPNPatterns porte les versions display (casse d'origine) des
// sous-chaînes matchées contre InterfaceDescription des adaptateurs Windows.
var windowsVPNPatterns = []string{
	"TAP-Windows",
	"WireGuard Tunnel",
	"OpenVPN",
	"Cisco AnyConnect",
	"Wintun",
	"NordVPN",
	"ExpressVPN",
	"ProtonVPN",
}

// windowsVPNPatternsLower est la version pré-lowercasée de windowsVPNPatterns,
// calculée une seule fois à l'init pour éviter des allocations dans la boucle
// de matching (fix L1).
var windowsVPNPatternsLower []string

func init() {
	windowsVPNPatternsLower = make([]string, len(windowsVPNPatterns))
	for i, p := range windowsVPNPatterns {
		windowsVPNPatternsLower[i] = strings.ToLower(p)
	}
}

// LinuxVPNPrefixes retourne une copie de la liste des préfixes VPN Linux.
// Safe pour les tests qui veulent inspecter la liste sans risque de mutation.
func LinuxVPNPrefixes() []string {
	cp := make([]string, len(linuxVPNPrefixes))
	copy(cp, linuxVPNPrefixes)
	return cp
}

// WindowsVPNPatterns retourne une copie de la liste des patterns VPN Windows
// (casse d'origine). Safe pour les tests.
func WindowsVPNPatterns() []string {
	cp := make([]string, len(windowsVPNPatterns))
	copy(cp, windowsVPNPatterns)
	return cp
}

// Interface représente un adaptateur réseau observé par le détecteur.
// Description est vide sur Linux et porte InterfaceDescription sur Windows.
type Interface struct {
	Name        string
	Description string
	IsUp        bool
}

// Lister énumère les interfaces réseau pour un OS donné. Injecté dans les
// tests unitaires via les constructeurs de détecteurs afin de ne pas dépendre
// de l'état réseau réel.
type Lister func() ([]Interface, error)

// ErrConcurrentVPN est renvoyée quand un VPN concurrent est détecté.
// InterfaceName porte le nom (Linux) ou la description (Windows) de
// l'adaptateur ayant matché ; MatchedPattern porte le préfixe/pattern exact
// ayant déclenché le refus (utile pour logs et tests).
type ErrConcurrentVPN struct {
	InterfaceName  string
	MatchedPattern string
}

func (e *ErrConcurrentVPN) Error() string {
	return fmt.Sprintf("VPN concurrent détecté (%s). Déconnectez-le pour utiliser Le Voile.", e.InterfaceName)
}

// VPNDetector est l'interface minimale consommée par le service. L'implémentation
// retournée par New() est OS-spécifique (build tags).
type VPNDetector interface {
	DetectConcurrentVPN() error
}

// Logger accepte un message formaté. Implémenté trivialement par log.Printf
// ou un wrapper autour du stderr du service. Un nil Logger désactive les logs.
type Logger func(level, msg string)

// matchLinux retourne le préfixe matché (et true) si name correspond à une
// interface VPN concurrent sur Linux. lo et OwnInterfaceName ne matchent jamais.
// Le matching est case-insensitive et préfixe-strict (wg0, wg-mullvad, tun5, …).
func matchLinux(name string) (string, bool) {
	if name == "" || name == "lo" || strings.EqualFold(name, OwnInterfaceName) {
		return "", false
	}
	lower := strings.ToLower(name)
	for _, p := range linuxVPNPrefixes {
		if strings.HasPrefix(lower, p) {
			return p, true
		}
	}
	return "", false
}

// matchWindows retourne le pattern matché (et true) si description contient
// l'une des sous-chaînes VPN connues. OwnInterfaceName sur l'adaptateur
// Wintun légitime est exclu (on match Wintun sinon → notre propre interface
// déclencherait un faux positif).
// Si Description est vide (fallback net.Interfaces — pas de PowerShell), on
// retombe sur matchLinux pour le matching par nom.
func matchWindows(name, description string) (string, bool) {
	if strings.EqualFold(name, OwnInterfaceName) {
		return "", false
	}
	// Primary: match Description (PowerShell Get-NetAdapter path).
	if description != "" {
		lower := strings.ToLower(description)
		for i, p := range windowsVPNPatternsLower {
			if strings.Contains(lower, p) {
				return windowsVPNPatterns[i], true
			}
		}
		return "", false
	}
	// Fallback: match Name by Linux-style prefixes (net.Interfaces path —
	// no Description available when PowerShell fails).
	return matchLinux(name)
}

// detect applique matcher à chaque interface UP et retourne la première
// détection. scannedUp est renseignée pour l'observabilité (log WARN/INFO).
func detect(ifs []Interface, matcher func(Interface) (string, bool)) (scannedUp []string, err error) {
	scannedUp = make([]string, 0, len(ifs))
	for _, i := range ifs {
		if !i.IsUp {
			continue
		}
		if i.Name == "lo" || strings.EqualFold(i.Name, OwnInterfaceName) {
			continue
		}
		scannedUp = append(scannedUp, i.Name)
		if pat, matched := matcher(i); matched {
			name := i.Name
			if i.Description != "" {
				name = i.Description
			}
			return scannedUp, &ErrConcurrentVPN{InterfaceName: name, MatchedPattern: pat}
		}
	}
	return scannedUp, nil
}
