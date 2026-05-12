//go:build windows

package watchdog

import (
	"errors"
	"net"
	"strings"
)

// netInterfaceByName est le wrapper stdlib pour les tests.
var netInterfaceByName = net.InterfaceByName

// isNotFound distingue « interface absente » d'une erreur réelle. Sur
// Windows, la stdlib renvoie un message du type « no such network
// interface » via GetAdaptersAddresses.
func isNotFound(err error) bool {
	for e := err; e != nil; e = errors.Unwrap(e) {
		msg := e.Error()
		if strings.Contains(msg, "no such network interface") ||
			strings.Contains(msg, "not found") {
			return true
		}
	}
	return false
}
