//go:build linux

package routing

import (
	"bytes"
	"fmt"
	"net"
	"os/exec"
	"strings"
	"sync"
)

// routingTable est la table de routage dédiée Le Voile (convention WireGuard).
const routingTable = "51820"

// rulePriority est la priorité de la ip rule (avant les rules système par
// défaut 32766/32767).
const rulePriority = "100"

type linuxRouteManager struct {
	mu     sync.Mutex
	active bool
	saved  *SavedRoutes
}

// New retourne un RouteManager pour Linux (iproute2 shellout).
func New() RouteManager {
	return &linuxRouteManager{}
}

func (m *linuxRouteManager) Setup(tunName string, relayIP net.IP, origGateway net.IP, origIface string) error {
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

	// 1. Route par défaut via TUN dans table 51820.
	//
	// Delete-before-add : `ip route add` échoue avec "File exists" si une
	// route identique traîne encore (crash précédent sans teardown, country
	// switch avorté à mi-chemin). Flush + add rend Setup idempotent et
	// évite d'avorter un country switch sur un état résiduel.
	_ = ipCmd("route", "flush", "table", routingTable)
	if err := ipCmd("route", "add", "0.0.0.0/0", "dev", tunName, "table", routingTable); err != nil {
		m.saved = nil
		return fmt.Errorf("routing: add default route table %s: %w", routingTable, err)
	}

	// 2. Rule pour utiliser la table 51820.
	_ = ipCmd("rule", "del", "priority", rulePriority)
	if err := ipCmd("rule", "add", "from", "all", "lookup", routingTable, "priority", rulePriority); err != nil {
		// Rollback route.
		_ = ipCmd("route", "flush", "table", routingTable)
		m.saved = nil
		return fmt.Errorf("routing: add rule priority %s: %w", rulePriority, err)
	}

	// 3. Route /32 vers relais via gateway originale (évite routing loop).
	relayStr := relay4.String() + "/32"
	_ = ipCmd("route", "del", relayStr)
	if err := ipCmd("route", "add", relayStr, "via", gw4.String()); err != nil {
		// Rollback rule + route.
		_ = ipCmd("rule", "del", "priority", rulePriority)
		_ = ipCmd("route", "flush", "table", routingTable)
		m.saved = nil
		return fmt.Errorf("routing: add relay route %s via %s: %w", relayStr, gw4, err)
	}

	m.active = true
	return nil
}

func (m *linuxRouteManager) Teardown() error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if !m.active {
		return nil // idempotent
	}

	var errs []string

	// Supprimer rule.
	if err := ipCmd("rule", "del", "priority", rulePriority); err != nil {
		if !isNoSuchRule(err) {
			errs = append(errs, fmt.Sprintf("rule del: %v", err))
		}
	}

	// Flush table 51820.
	if err := ipCmd("route", "flush", "table", routingTable); err != nil {
		errs = append(errs, fmt.Sprintf("route flush table %s: %v", routingTable, err))
	}

	// Supprimer route /32 relais.
	if m.saved != nil && m.saved.RelayIP != nil {
		relayStr := m.saved.RelayIP.String() + "/32"
		if err := ipCmd("route", "del", relayStr); err != nil {
			if !isNoSuchRoute(err) {
				errs = append(errs, fmt.Sprintf("route del %s: %v", relayStr, err))
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

func (m *linuxRouteManager) Cleanup() error {
	m.mu.Lock()
	defer m.mu.Unlock()

	// Purger les rules orphelines lookup 51820.
	out, err := exec.Command("ip", "rule", "list").CombinedOutput()
	if err == nil && bytes.Contains(out, []byte("lookup "+routingTable)) {
		_ = ipCmd("rule", "del", "priority", rulePriority)
	}

	// Flush table 51820 si non vide.
	out, err = exec.Command("ip", "route", "show", "table", routingTable).CombinedOutput()
	if err == nil && len(bytes.TrimSpace(out)) > 0 {
		_ = ipCmd("route", "flush", "table", routingTable)
	}

	return nil
}

func (m *linuxRouteManager) Saved() *SavedRoutes {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.saved
}

// CaptureOriginalRoute retourne la gateway IPv4 par défaut ET le nom de
// l'interface dans un seul appel "ip route show default" — élimine la race
// TOCTOU entre deux appels séparés. Doit être appelée AVANT toute mutation
// de route (AC3). Erreur bloquante si la gateway ne peut être résolue.
func CaptureOriginalRoute() (gateway net.IP, iface string, err error) {
	out, err := exec.Command("ip", "route", "show", "default").CombinedOutput()
	if err != nil {
		return nil, "", fmt.Errorf("routing: ip route show default: %w: %s", err, out)
	}
	fields := strings.Fields(string(out))
	for i, f := range fields {
		if f == "via" && i+1 < len(fields) {
			gateway = net.ParseIP(fields[i+1])
		}
		if f == "dev" && i+1 < len(fields) {
			iface = fields[i+1]
		}
	}
	if gateway == nil {
		return nil, "", fmt.Errorf("%w: impossible de parser la gateway dans: %s", ErrGatewayResolve, out)
	}
	if iface == "" {
		return nil, "", fmt.Errorf("routing: impossible de parser l'interface par défaut: %s", out)
	}
	return gateway.To4(), iface, nil
}

// ipCmd exécute "ip <args>" et retourne une erreur wrappée avec préfixe
// routing: si la commande échoue.
func ipCmd(args ...string) error {
	cmd := exec.Command("ip", args...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("%w: %s", err, bytes.TrimSpace(out))
	}
	return nil
}

// isNoSuchRoute détecte les erreurs "No such process" / "No such file" de
// iproute2 quand la route n'existe déjà plus (idempotent).
func isNoSuchRoute(err error) bool {
	if err == nil {
		return false
	}
	s := err.Error()
	return strings.Contains(s, "No such process") || strings.Contains(s, "No such file")
}

// isNoSuchRule détecte l'erreur quand la rule n'existe pas.
func isNoSuchRule(err error) bool {
	return isNoSuchRoute(err) // même message d'erreur kernel
}
