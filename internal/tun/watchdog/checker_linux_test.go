//go:build linux

package watchdog

import (
	"context"
	"errors"
	"net"
	"testing"
)

func TestNetChecker_ValidatesConfig(t *testing.T) {
	if _, err := NewNetChecker(CheckerConfig{}); err == nil {
		t.Error("NewNetChecker doit rejeter une CheckerConfig vide")
	}
	if _, err := NewNetChecker(CheckerConfig{Name: "x"}); err == nil {
		t.Error("NewNetChecker doit exiger ExpectedMTU > 0")
	}
}

func TestNetChecker_MissingInterface(t *testing.T) {
	c, err := NewNetChecker(CheckerConfig{Name: "nonexistentiface999", ExpectedMTU: 1420})
	if err != nil {
		t.Fatalf("NewNetChecker: %v", err)
	}
	status, err := c.Check(context.Background())
	if err != nil {
		t.Fatalf("Check: %v", err)
	}
	if status != StatusMissing {
		t.Errorf("status = %v, want StatusMissing", status)
	}
}

func TestNetChecker_LoopbackOK(t *testing.T) {
	// "lo" existe toujours et est UP sur toute machine Linux. On prend son
	// MTU effectif pour que StatusOK soit atteignable.
	lo, err := lookupMTU("lo")
	if err != nil {
		t.Skipf("lo indisponible: %v", err)
	}
	c, err := NewNetChecker(CheckerConfig{Name: "lo", ExpectedMTU: lo})
	if err != nil {
		t.Fatalf("NewNetChecker: %v", err)
	}
	status, err := c.Check(context.Background())
	if err != nil {
		t.Fatalf("Check: %v", err)
	}
	if status != StatusOK {
		t.Errorf("status = %v sur lo, want StatusOK", status)
	}
}

func TestNetChecker_LoopbackInvalidMTU(t *testing.T) {
	// MTU volontairement faux → StatusInvalid (AC4, MTU strict).
	c, err := NewNetChecker(CheckerConfig{Name: "lo", ExpectedMTU: 1})
	if err != nil {
		t.Fatalf("NewNetChecker: %v", err)
	}
	status, err := c.Check(context.Background())
	if err != nil {
		t.Fatalf("Check: %v", err)
	}
	if status != StatusInvalid {
		t.Errorf("status = %v avec MTU faux, want StatusInvalid", status)
	}
}

func TestNetChecker_ContextCancelled(t *testing.T) {
	c, _ := NewNetChecker(CheckerConfig{Name: "lo", ExpectedMTU: 65536})
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := c.Check(ctx)
	if !errors.Is(err, context.Canceled) {
		t.Errorf("err = %v, want context.Canceled", err)
	}
}

// lookupMTU utilise net.InterfaceByName pour récupérer la MTU effective.
func lookupMTU(name string) (int, error) {
	iface, err := net.InterfaceByName(name)
	if err != nil {
		return 0, err
	}
	return iface.MTU, nil
}
