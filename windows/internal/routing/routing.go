//go:build windows

// Package routing gère les routes système pour capturer le trafic IPv4
// via l'interface TUN levoile0. Il expose une interface RouteManager avec
// des implémentations OS-spécifiques (build tags linux/windows).
//
// Séquence d'orchestration (architecture §Enforcement) :
//
//	Connect  : elevation → tun.New → routing.Setup → firewall.Activate → tunnel
//	Disconnect : tunnel → firewall.Deactivate → routing.Teardown → tun.Close
package routing

import (
	"errors"
	"net"
)

// Erreurs sentinelles préfixées "routing:" (convention architecture §Conventions).
var (
	// ErrAlreadyActive indique que Setup a déjà été appelé sans Teardown.
	ErrAlreadyActive = errors.New("routing: already active")
	// ErrGatewayResolve indique que la gateway originale n'a pas pu être
	// capturée — sans elle, pas de route /32 relais = routing loop garanti.
	ErrGatewayResolve = errors.New("routing: impossible de résoudre la gateway originale")
)

// SavedRoutes stocke l'état réseau original capturé avant toute mutation,
// afin de permettre la restauration complète (NFR6).
type SavedRoutes struct {
	OrigGateway     net.IP // gateway par défaut originale
	OrigDefaultIface string // nom de l'interface de la route par défaut
	RelayIP         net.IP // IP du relais (pour la route /32)
	TUNName         string // nom de l'interface TUN
}

// RouteManager définit le contrat pour la gestion des routes système.
// Chaque OS fournit son implémentation via build tags.
type RouteManager interface {
	// Setup configure le routage : route par défaut via TUN + route /32
	// vers relayIP via la gateway originale. origIface est le nom de
	// l'interface de la route par défaut (capturée par CaptureOriginalRoute
	// en un seul appel atomique avec origGateway pour éviter une race
	// TOCTOU). Doit être appelé après tun.New et avant firewall.Activate.
	// Retourne ErrAlreadyActive si déjà configuré.
	Setup(tunName string, relayIP net.IP, origGateway net.IP, origIface string) error

	// Teardown supprime toutes les routes/rules ajoutées par Le Voile et
	// restaure la route par défaut originale. Idempotent : un second appel
	// retourne nil sans erreur.
	Teardown() error

	// Cleanup purge les routes/rules orphelines laissées par un crash
	// précédent (NFR17 < 5s). Appelé au boot du service avant Setup.
	Cleanup() error

	// Saved retourne les routes sauvegardées, ou nil si Setup n'a pas
	// encore été appelé.
	Saved() *SavedRoutes
}
