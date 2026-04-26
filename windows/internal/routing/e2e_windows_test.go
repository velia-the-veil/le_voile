//go:build e2e && windows

package routing

import (
	"strings"
	"testing"

	"golang.org/x/sys/windows"
)

func skipIfNotAdmin(t *testing.T) {
	t.Helper()
	var sid *windows.SID
	err := windows.AllocateAndInitializeSid(
		&windows.SECURITY_NT_AUTHORITY,
		2,
		windows.SECURITY_BUILTIN_DOMAIN_RID,
		windows.DOMAIN_ALIAS_RID_ADMINS,
		0, 0, 0, 0, 0, 0,
		&sid,
	)
	if err != nil {
		t.Skipf("cannot check admin: %v", err)
	}
	defer windows.FreeSid(sid)
	token := windows.Token(0)
	member, err := token.IsMember(sid)
	if err != nil || !member {
		t.Skip("requires admin privileges")
	}
}

func ensureTestTUN(t *testing.T) string {
	t.Helper()
	name := "levoile0"
	// Vérifier que l'interface Wintun existe (doit être créée par tun.New
	// dans un processus admin avant de lancer les tests E2E).
	out, err := hiddenCmd("netsh", "interface", "show", "interface", name)
	if err != nil {
		t.Skipf("interface %s not found (run tun.New first): %v: %s", name, err, out)
	}
	return name
}

func cleanupTestTUN(t *testing.T, name string) {
	t.Helper()
	// On ne détruit pas l'interface Wintun — elle est gérée par le test
	// ou le service qui l'a créée.
}

func verifyRoutesPresent(t *testing.T) {
	t.Helper()
	out, err := hiddenCmd("netsh", "interface", "ipv4", "show", "route")
	if err != nil {
		t.Errorf("netsh show route: %v", err)
		return
	}
	s := string(out)
	if !strings.Contains(s, "levoile0") {
		t.Errorf("no route via levoile0 found in routing table")
	}
}

func verifyRoutesAbsent(t *testing.T) {
	t.Helper()
	out, _ := hiddenCmd("netsh", "interface", "ipv4", "show", "route")
	lines := strings.Split(string(out), "\n")
	for _, line := range lines {
		if strings.Contains(line, "0.0.0.0/0") && strings.Contains(line, "levoile0") {
			t.Errorf("default route via levoile0 still present after Teardown")
		}
	}
}
