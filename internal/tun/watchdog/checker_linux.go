//go:build linux

package watchdog

import (
	"errors"
	"net"
	"strings"
)

// netInterfaceByName est le wrapper stdlib pour les tests d'intégration.
var netInterfaceByName = net.InterfaceByName

// isNotFound distingue « interface absente » d'une erreur netlink réelle.
// net.InterfaceByName ne retourne pas os.ErrNotExist ; on matche le message
// documenté par la stdlib (« no such network interface »).
func isNotFound(err error) bool {
	for e := err; e != nil; e = errors.Unwrap(e) {
		msg := e.Error()
		if strings.Contains(msg, "no such network interface") ||
			strings.Contains(msg, "no such device") {
			return true
		}
	}
	return false
}
