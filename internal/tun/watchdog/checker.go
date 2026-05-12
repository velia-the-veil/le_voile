// Package watchdog détecte la disparition ou l'altération externe de
// l'interface TUN/Wintun levoile0 et déclenche une reconnexion complète.
//
// Le rôle est STRICTEMENT distinct de internal/watchdog/ (résolveur DNS) qui
// sera supprimé en story 2.5 avec le retrait du proxy DNS local.
package watchdog

import (
	"context"
	"errors"
	"fmt"
	"net"
)

// Status qualifie l'état courant de l'interface surveillée.
type Status int

const (
	// StatusOK — interface présente, up, MTU attendu.
	StatusOK Status = iota
	// StatusMissing — interface absente du système.
	StatusMissing
	// StatusInvalid — interface présente mais altérée (down, MTU modifié,
	// NO-CARRIER).
	StatusInvalid
)

// String retourne une représentation lisible pour les logs.
func (s Status) String() string {
	switch s {
	case StatusOK:
		return "ok"
	case StatusMissing:
		return "missing"
	case StatusInvalid:
		return "invalid"
	default:
		return "unknown"
	}
}

// InterfaceChecker inspecte une interface TUN/Wintun et rapporte son état.
// Les implémentations sont sûres pour appel concurrent.
type InterfaceChecker interface {
	// Check retourne le Status courant. Une erreur non-nil indique un échec
	// de l'inspection elle-même (ex: syscall errno inattendu) ; dans ce cas
	// la décision de trigger est laissée à l'appelant.
	Check(ctx context.Context) (Status, error)
}

// CheckerConfig paramètre un NetChecker cross-platform.
type CheckerConfig struct {
	// Name — nom de l'interface à surveiller (ex: "levoile0").
	Name string
	// ExpectedMTU — MTU attendue, comparée strictement (décision story 2.2).
	ExpectedMTU int
}

// Validate vérifie que la configuration est cohérente avant usage.
func (c CheckerConfig) Validate() error {
	if c.Name == "" {
		return errors.New("watchdog: Name requis")
	}
	if c.ExpectedMTU <= 0 {
		return errors.New("watchdog: ExpectedMTU doit être > 0")
	}
	return nil
}

// NetChecker est l'implémentation cross-platform d'InterfaceChecker.
// Utilise net.InterfaceByName (stdlib) pour détecter présence, flags UP et MTU.
type NetChecker struct {
	cfg CheckerConfig
}

// NewNetChecker construit un checker pour la configuration fournie.
func NewNetChecker(cfg CheckerConfig) (*NetChecker, error) {
	if err := cfg.Validate(); err != nil {
		return nil, err
	}
	return &NetChecker{cfg: cfg}, nil
}

// Check inspecte l'interface :
//   - absente → StatusMissing
//   - down ou MTU ≠ ExpectedMTU → StatusInvalid (AC4, MTU strict == 1420)
//   - sinon → StatusOK
//
// NOTE Windows/Wintun : la stdlib lit le MTU NDIS exposé par le driver
// Wintun, qui reste fixé à 65535 (taille max théorique d'un frame) quelle
// que soit la valeur passée à `wintun.CreateAdapter(...).SetMTU()`. Le MTU
// « applicatif » de 1420 est géré côté framing tunnel uniquement, pas côté
// NDIS. Comparer les deux produisait un false positive permanent → watchdog
// déclenchait RecoverFromAnomaly toutes les 3s. Sur Windows on skip donc le
// strict MTU compare — la détection d'altération reste couverte par
// StatusMissing (interface supprimée) et FlagUp (interface désactivée).
// Sur Linux/TUN le MTU NDIS = MTU tunnel, le check reste strict.
func (c *NetChecker) Check(ctx context.Context) (Status, error) {
	if err := ctx.Err(); err != nil {
		return StatusMissing, err
	}
	iface, err := netInterfaceByName(c.cfg.Name)
	if err != nil {
		if isNotFound(err) {
			return StatusMissing, nil
		}
		return StatusMissing, fmt.Errorf("watchdog: InterfaceByName %q: %w", c.cfg.Name, err)
	}
	if iface.Flags&net.FlagUp == 0 {
		return StatusInvalid, nil
	}
	if !mtuMatches(iface.MTU, c.cfg.ExpectedMTU) {
		return StatusInvalid, nil
	}
	return StatusOK, nil
}
