//go:build e2e && linux

package routing

import (
	"os"
	"os/exec"
	"strings"
	"testing"
)

func skipIfNotRoot(t *testing.T) {
	t.Helper()
	if os.Geteuid() != 0 {
		t.Skip("requires root (CAP_NET_ADMIN)")
	}
}

func ensureTestTUN(t *testing.T) string {
	t.Helper()
	name := "levoile0"
	if err := ipCmd("tuntap", "add", "dev", name, "mode", "tun"); err != nil {
		t.Skipf("cannot create TUN: %v", err)
	}
	if err := ipCmd("link", "set", name, "up"); err != nil {
		_ = ipCmd("link", "del", name)
		t.Skipf("cannot bring up TUN: %v", err)
	}
	return name
}

func cleanupTestTUN(t *testing.T, name string) {
	t.Helper()
	_ = ipCmd("link", "del", name)
}

func verifyRoutesPresent(t *testing.T) {
	t.Helper()
	out, err := exec.Command("ip", "route", "show", "table", routingTable).CombinedOutput()
	if err != nil {
		t.Errorf("ip route show table %s: %v", routingTable, err)
		return
	}
	if !strings.Contains(string(out), "0.0.0.0/0") {
		t.Errorf("default route not found in table %s: %s", routingTable, out)
	}

	out, err = exec.Command("ip", "rule", "list").CombinedOutput()
	if err != nil {
		t.Errorf("ip rule list: %v", err)
		return
	}
	if !strings.Contains(string(out), "lookup "+routingTable) {
		t.Errorf("rule lookup %s not found: %s", routingTable, out)
	}
}

func verifyRoutesAbsent(t *testing.T) {
	t.Helper()
	out, _ := exec.Command("ip", "route", "show", "table", routingTable).CombinedOutput()
	if strings.Contains(string(out), "0.0.0.0/0") {
		t.Errorf("default route still present in table %s after Teardown", routingTable)
	}
}
