//go:build linux

package preflight

import (
	"errors"
	"testing"
)

func TestLinuxDetector_Nominal(t *testing.T) {
	list := func() ([]Interface, error) {
		return []Interface{
			{Name: "lo", IsUp: true},
			{Name: "eth0", IsUp: true},
			{Name: "wlan0", IsUp: true},
			{Name: "docker0", IsUp: true},
		}, nil
	}
	d := NewWithLister(list, nil)
	if err := d.DetectConcurrentVPN(); err != nil {
		t.Errorf("DetectConcurrentVPN() = %v, want nil", err)
	}
}

func TestLinuxDetector_ConcurrentWireGuard(t *testing.T) {
	list := func() ([]Interface, error) {
		return []Interface{
			{Name: "eth0", IsUp: true},
			{Name: "wg0", IsUp: true},
		}, nil
	}
	d := NewWithLister(list, nil)
	var e *ErrConcurrentVPN
	if err := d.DetectConcurrentVPN(); !errors.As(err, &e) {
		t.Fatalf("DetectConcurrentVPN() = %v, want *ErrConcurrentVPN", err)
	}
	if e.InterfaceName != "wg0" || e.MatchedPattern != "wg" {
		t.Errorf("got name=%q pattern=%q, want wg0/wg", e.InterfaceName, e.MatchedPattern)
	}
}

func TestLinuxDetector_OwnInterfaceReused(t *testing.T) {
	// Crash-recovery (2.1) : levoile0 existe déjà UP, ne doit PAS matcher.
	list := func() ([]Interface, error) {
		return []Interface{
			{Name: "eth0", IsUp: true},
			{Name: OwnInterfaceName, IsUp: true},
		}, nil
	}
	d := NewWithLister(list, nil)
	if err := d.DetectConcurrentVPN(); err != nil {
		t.Errorf("DetectConcurrentVPN() = %v, want nil", err)
	}
}

func TestLinuxDetector_DownVPNIgnored(t *testing.T) {
	list := func() ([]Interface, error) {
		return []Interface{
			{Name: "eth0", IsUp: true},
			{Name: "tun5", IsUp: false}, // VPN présent mais DOWN
		}, nil
	}
	d := NewWithLister(list, nil)
	if err := d.DetectConcurrentVPN(); err != nil {
		t.Errorf("DetectConcurrentVPN() = %v, want nil", err)
	}
}

func TestLinuxDetector_ListerErrorFailsOpen(t *testing.T) {
	list := func() ([]Interface, error) {
		return nil, errors.New("boom")
	}
	d := NewWithLister(list, nil)
	if err := d.DetectConcurrentVPN(); err != nil {
		t.Errorf("DetectConcurrentVPN() = %v, want nil (fail-open)", err)
	}
}

func TestLinuxDetector_LoggerCalled(t *testing.T) {
	var levels []string
	logger := func(level, msg string) { levels = append(levels, level) }
	list := func() ([]Interface, error) {
		return []Interface{{Name: "wg0", IsUp: true}}, nil
	}
	d := NewWithLister(list, logger)
	_ = d.DetectConcurrentVPN()
	found := false
	for _, l := range levels {
		if l == "WARN" {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("expected WARN log on concurrent VPN, got %v", levels)
	}
}
