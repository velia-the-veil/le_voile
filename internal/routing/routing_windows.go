//go:build windows

package routing

import (
	"bytes"
	"fmt"
	"net"
	"os/exec"
	"strings"
	"sync"
	"syscall"
)

type windowsRouteManager struct {
	mu     sync.Mutex
	active bool
	saved  *SavedRoutes
}

// New retourne un RouteManager pour Windows (netsh/route shellout).
func New() RouteManager {
	return &windowsRouteManager{}
}

func (m *windowsRouteManager) Setup(tunName string, relayIP net.IP, origGateway net.IP, origIface string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.active {
		return ErrAlreadyActive
	}

	relay4 := relayIP.To4()
	if relay4 == nil {
		return fmt.Errorf("routing: relayIP %v n'est pas IPv4", relayIP)
	}
	gw4 := origGateway.To4()
	if gw4 == nil {
		return ErrGatewayResolve
	}
	if origIface == "" {
		return fmt.Errorf("routing: origIface vide")
	}

	m.saved = &SavedRoutes{
		OrigGateway:      gw4,
		OrigDefaultIface: origIface,
		RelayIP:          relay4,
		TUNName:          tunName,
	}

	// 1. Route par défaut (0.0.0.0/0) via TUN avec metric basse (5).
	// netsh interface ipv4 add route 0.0.0.0/0 "levoile0" 0.0.0.0 metric=5
	//
	// Delete-before-add : netsh "add route" échoue avec "L'objet existe déjà"
	// si une route identique traîne encore (crash précédent sans teardown,
	// country switch avorté à mi-chemin, ou manipulation manuelle). Un
	// delete best-effort avant le add rend Setup idempotent et évite
	// d'avorter un country switch sur un état résiduel.
	_ = netshRoute("delete", "0.0.0.0/0", tunName, "0.0.0.0", "")
	if err := netshRoute("add", "0.0.0.0/0", tunName, "0.0.0.0", "5"); err != nil {
		m.saved = nil
		return fmt.Errorf("routing: add default route via TUN: %w", err)
	}

	// 2. Route /32 vers relais via gateway originale avec metric haute (1)
	// sur l'interface originale — longest-prefix match garantit que /32 > /0.
	relayStr := relay4.String() + "/32"
	_ = netshRoute("delete", relayStr, origIface, gw4.String(), "")
	if err := netshRoute("add", relayStr, origIface, gw4.String(), "1"); err != nil {
		// Rollback route par défaut.
		_ = netshRoute("delete", "0.0.0.0/0", tunName, "0.0.0.0", "")
		m.saved = nil
		return fmt.Errorf("routing: add relay route %s via %s: %w", relayStr, gw4, err)
	}

	m.active = true
	return nil
}

func (m *windowsRouteManager) Teardown() error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if !m.active {
		return nil // idempotent
	}

	var errs []string

	// Supprimer route /32 relais.
	if m.saved != nil && m.saved.RelayIP != nil {
		relayStr := m.saved.RelayIP.String() + "/32"
		if err := netshRoute("delete", relayStr, m.saved.OrigDefaultIface, m.saved.OrigGateway.String(), ""); err != nil {
			if !isRouteNotFound(err) {
				errs = append(errs, fmt.Sprintf("delete relay route: %v", err))
			}
		}
	}

	// Supprimer route par défaut via TUN.
	if m.saved != nil {
		if err := netshRoute("delete", "0.0.0.0/0", m.saved.TUNName, "0.0.0.0", ""); err != nil {
			if !isRouteNotFound(err) {
				errs = append(errs, fmt.Sprintf("delete default route: %v", err))
			}
		}
	}

	m.active = false
	m.saved = nil

	if len(errs) > 0 {
		return fmt.Errorf("routing: teardown: %s", strings.Join(errs, "; "))
	}
	return nil
}

func (m *windowsRouteManager) Cleanup() error {
	m.mu.Lock()
	defer m.mu.Unlock()

	// Supprimer route 0.0.0.0/0 résiduelle sur levoile0 (crash recovery).
	_ = netshRoute("delete", "0.0.0.0/0", "levoile0", "0.0.0.0", "")

	// Supprimer les routes /32 orphelines passant par levoile0 (AC6).
	// Parser "netsh interface ipv4 show route" pour trouver les routes
	// dont l'interface est levoile0 et le préfixe est un /32.
	out, err := hiddenCmd("netsh", "interface", "ipv4", "show", "route")
	if err != nil {
		return nil // best-effort
	}
	for _, line := range strings.Split(string(out), "\n") {
		line = strings.TrimSpace(line)
		if !strings.Contains(line, "/32") {
			continue
		}
		if !strings.Contains(line, "levoile") {
			continue
		}
		// Extraire le préfixe (champ contenant /32).
		fields := strings.Fields(line)
		for _, f := range fields {
			if strings.HasSuffix(f, "/32") {
				_ = netshRoute("delete", f, "levoile0", "0.0.0.0", "")
				break
			}
		}
	}

	return nil
}

func (m *windowsRouteManager) Saved() *SavedRoutes {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.saved
}

// CaptureOriginalRoute retourne la gateway IPv4 par défaut ET le nom de
// l'interface dans un seul appel "netsh interface ipv4 show route" — élimine
// la race TOCTOU entre deux appels séparés. Doit être appelée AVANT toute
// mutation de route (AC3).
//
// Format netsh (localisé FR/EN) :
//
//	Publier  Type      Mét  Préfixe      Idx  Nom passerelle/interface
//	Non      Manuel    0    0.0.0.0/0      6  192.168.1.1
func CaptureOriginalRoute() (gateway net.IP, iface string, err error) {
	out, err := hiddenCmd("netsh", "interface", "ipv4", "show", "route")
	if err != nil {
		return nil, "", fmt.Errorf("routing: netsh show route: %w: %s", err, out)
	}
	lines := strings.Split(string(out), "\n")
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if !strings.Contains(line, "0.0.0.0/0") {
			continue
		}
		if strings.Contains(line, "levoile") {
			continue
		}
		// Extraire gateway (champ après Idx) et Idx pour résoudre le nom.
		fields := strings.Fields(line)
		var gw net.IP
		var idx string
		for i, f := range fields {
			if f == "0.0.0.0/0" && i+2 < len(fields) {
				idx = fields[i+1]
				gw = net.ParseIP(fields[i+2])
			}
		}
		if gw == nil || idx == "" {
			continue
		}
		name, nameErr := ifIdxToName(idx)
		if nameErr != nil {
			continue
		}
		return gw.To4(), name, nil
	}
	return nil, "", fmt.Errorf("%w: impossible de parser la route par défaut depuis netsh show route", ErrGatewayResolve)
}

// extractIfIdx extrait le champ Idx d'une ligne "netsh show route".
// Le format est : Publish Type Met Prefix Idx Gateway/InterfaceName
func extractIfIdx(line string) string {
	fields := strings.Fields(line)
	for i, f := range fields {
		if f == "0.0.0.0/0" && i+1 < len(fields) {
			return fields[i+1]
		}
	}
	return ""
}

// ifIdxToName résout un index d'interface Windows en son nom via netsh.
// Format : Idx  Met  MTU  État  Nom
//            6   20  1500  connected  Ethernet
func ifIdxToName(idx string) (string, error) {
	out, err := hiddenCmd("netsh", "interface", "ipv4", "show", "interfaces")
	if err != nil {
		return "", err
	}
	lines := strings.Split(string(out), "\n")
	for _, line := range lines {
		fields := strings.Fields(strings.TrimSpace(line))
		// Idx Met MTU État Nom... → 5 fields minimum, nom à partir de [4]
		if len(fields) >= 5 && fields[0] == idx {
			return strings.Join(fields[4:], " "), nil
		}
	}
	return "", fmt.Errorf("interface index %s non trouvé", idx)
}

// netshRoute exécute "netsh interface ipv4 <action> route <prefix>
// <interface> <nexthop> [metric=<m>]".
func netshRoute(action, prefix, iface, nexthop, metric string) error {
	args := []string{"interface", "ipv4", action, "route", prefix, iface, nexthop}
	if metric != "" {
		args = append(args, "metric="+metric)
	}
	out, err := hiddenCmd("netsh", args...)
	if err != nil {
		return fmt.Errorf("routing: netsh %s route: %w: %s", action, err, bytes.TrimSpace(out))
	}
	return nil
}

// hiddenCmd exécute une commande Windows sans flasher de console
// (pattern dns/cmd_windows.go).
func hiddenCmd(name string, args ...string) ([]byte, error) {
	cmd := exec.Command(name, args...)
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}
	return cmd.CombinedOutput()
}

// isRouteNotFound détecte quand la route n'existe pas/plus (idempotent).
func isRouteNotFound(err error) bool {
	if err == nil {
		return false
	}
	s := strings.ToLower(err.Error())
	return strings.Contains(s, "not found") || strings.Contains(s, "introuvable") ||
		strings.Contains(s, "element not found") || strings.Contains(s, "élément introuvable")
}
