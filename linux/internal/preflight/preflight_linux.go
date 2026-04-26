//go:build linux

package preflight

import (
	"fmt"
	"net"
)

// linuxDetector scanne net.Interfaces() et matche les noms par préfixe.
type linuxDetector struct {
	list   Lister
	logger Logger
}

// New retourne un VPNDetector OS-spécifique utilisant net.Interfaces().
// Passer un Logger nil désactive les logs.
func New(logger Logger) VPNDetector {
	return &linuxDetector{list: defaultLinuxLister, logger: logger}
}

// NewWithLister permet d'injecter un Lister (tests unitaires).
func NewWithLister(list Lister, logger Logger) VPNDetector {
	return &linuxDetector{list: list, logger: logger}
}

// defaultLinuxLister énumère les interfaces via net.Interfaces() et remplit
// uniquement Name et IsUp (Description n'existe pas sur Linux).
func defaultLinuxLister() ([]Interface, error) {
	ifs, err := net.Interfaces()
	if err != nil {
		return nil, fmt.Errorf("preflight: net.Interfaces: %w", err)
	}
	out := make([]Interface, 0, len(ifs))
	for _, i := range ifs {
		out = append(out, Interface{
			Name: i.Name,
			IsUp: i.Flags&net.FlagUp != 0,
		})
	}
	return out, nil
}

func (d *linuxDetector) DetectConcurrentVPN() error {
	ifs, err := d.list()
	if err != nil {
		// Fail-open : une erreur d'énumération ne doit pas bloquer la connexion.
		if d.logger != nil {
			d.logger("WARN", fmt.Sprintf("preflight: énumération échouée, fail-open: %v", err))
		}
		return nil
	}
	scanned, detErr := detect(ifs, func(i Interface) (string, bool) {
		return matchLinux(i.Name)
	})
	if detErr != nil {
		if d.logger != nil {
			if e, ok := detErr.(*ErrConcurrentVPN); ok {
				d.logger("WARN", fmt.Sprintf("preflight: VPN concurrent détecté (interface=%q, pattern=%q, scanned=%v)", e.InterfaceName, e.MatchedPattern, scanned))
			}
		}
		return detErr
	}
	if d.logger != nil {
		d.logger("INFO", fmt.Sprintf("preflight: aucun VPN concurrent (scanned=%v)", scanned))
	}
	return nil
}
