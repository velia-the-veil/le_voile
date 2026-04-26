//go:build windows

package watchdog

import (
	"context"
	"errors"
	"net"
	"testing"
)

func TestNetChecker_ValidatesConfig_Windows(t *testing.T) {
	if _, err := NewNetChecker(CheckerConfig{}); err == nil {
		t.Error("NewNetChecker doit rejeter une CheckerConfig vide")
	}
}

func TestNetChecker_MissingInterface_Windows(t *testing.T) {
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

func TestNetChecker_AnyExistingAdapter_Windows(t *testing.T) {
	// Cherche une interface existante et UP (ex: Loopback, Ethernet, Wi-Fi)
	// sans présumer d'un nom fixe (variable selon la machine Windows).
	ifaces, err := net.Interfaces()
	if err != nil {
		t.Skipf("net.Interfaces: %v", err)
	}
	var target *net.Interface
	for i := range ifaces {
		if ifaces[i].Flags&net.FlagUp != 0 && ifaces[i].MTU > 0 {
			target = &ifaces[i]
			break
		}
	}
	if target == nil {
		t.Skip("aucune interface UP trouvée sur cette machine")
	}
	c, err := NewNetChecker(CheckerConfig{Name: target.Name, ExpectedMTU: target.MTU})
	if err != nil {
		t.Fatalf("NewNetChecker: %v", err)
	}
	status, err := c.Check(context.Background())
	if err != nil {
		t.Fatalf("Check: %v", err)
	}
	if status != StatusOK {
		t.Errorf("status = %v sur %q, want StatusOK", status, target.Name)
	}
}

func TestNetChecker_ContextCancelled_Windows(t *testing.T) {
	c, _ := NewNetChecker(CheckerConfig{Name: "Loopback Pseudo-Interface 1", ExpectedMTU: 1500})
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := c.Check(ctx)
	if !errors.Is(err, context.Canceled) {
		t.Errorf("err = %v, want context.Canceled", err)
	}
}
