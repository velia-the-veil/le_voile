//go:build windows

package preflight

import (
	"errors"
	"testing"
)

func TestWindowsDetector_Nominal(t *testing.T) {
	list := func() ([]Interface, error) {
		return []Interface{
			{Name: "Ethernet", Description: "Intel(R) Ethernet Connection I219-V", IsUp: true},
			{Name: "Wi-Fi", Description: "Intel(R) Wi-Fi 6 AX201 160MHz", IsUp: true},
		}, nil
	}
	d := NewWithLister(list, nil)
	if err := d.DetectConcurrentVPN(); err != nil {
		t.Errorf("DetectConcurrentVPN() = %v, want nil", err)
	}
}

func TestWindowsDetector_ConcurrentWireGuard(t *testing.T) {
	list := func() ([]Interface, error) {
		return []Interface{
			{Name: "Ethernet", Description: "Intel(R) Ethernet Connection I219-V", IsUp: true},
			{Name: "Mullvad", Description: "WireGuard Tunnel #3", IsUp: true},
		}, nil
	}
	d := NewWithLister(list, nil)
	var e *ErrConcurrentVPN
	if err := d.DetectConcurrentVPN(); !errors.As(err, &e) {
		t.Fatalf("DetectConcurrentVPN() = %v, want *ErrConcurrentVPN", err)
	}
	if e.MatchedPattern != "WireGuard Tunnel" {
		t.Errorf("pattern=%q, want WireGuard Tunnel", e.MatchedPattern)
	}
	if e.InterfaceName != "WireGuard Tunnel #3" {
		t.Errorf("name=%q, want description value", e.InterfaceName)
	}
}

func TestWindowsDetector_OwnWintunIgnored(t *testing.T) {
	// Notre Wintun levoile0 ne doit jamais matcher Wintun.
	list := func() ([]Interface, error) {
		return []Interface{
			{Name: "Ethernet", Description: "Intel Ethernet", IsUp: true},
			{Name: OwnInterfaceName, Description: "Wintun Userspace Tunnel", IsUp: true},
		}, nil
	}
	d := NewWithLister(list, nil)
	if err := d.DetectConcurrentVPN(); err != nil {
		t.Errorf("DetectConcurrentVPN() = %v, want nil", err)
	}
}

func TestWindowsDetector_ListerErrorFailsOpen(t *testing.T) {
	list := func() ([]Interface, error) {
		return nil, errors.New("powershell missing")
	}
	d := NewWithLister(list, nil)
	if err := d.DetectConcurrentVPN(); err != nil {
		t.Errorf("DetectConcurrentVPN() = %v, want nil (fail-open)", err)
	}
}

func TestParsePSOutput_Array(t *testing.T) {
	in := []byte(`[{"Name":"Ethernet","InterfaceDescription":"Intel","Status":"Up"},{"Name":"Wi-Fi","InterfaceDescription":"AX201","Status":"Disabled"}]`)
	got, err := parsePSOutput(in)
	if err != nil {
		t.Fatalf("parsePSOutput err=%v", err)
	}
	if len(got) != 2 {
		t.Fatalf("len=%d, want 2", len(got))
	}
	if !got[0].IsUp || got[1].IsUp {
		t.Errorf("Status mapping wrong: %+v", got)
	}
	if got[0].Description != "Intel" {
		t.Errorf("desc=%q, want Intel", got[0].Description)
	}
}

func TestParsePSOutput_SingleObject(t *testing.T) {
	in := []byte(`{"Name":"Ethernet","InterfaceDescription":"Intel","Status":"Up"}`)
	got, err := parsePSOutput(in)
	if err != nil {
		t.Fatalf("parsePSOutput err=%v", err)
	}
	if len(got) != 1 || got[0].Name != "Ethernet" || !got[0].IsUp {
		t.Errorf("got %+v", got)
	}
}

func TestParsePSOutput_BOMStripped(t *testing.T) {
	in := append([]byte{0xEF, 0xBB, 0xBF}, []byte(`{"Name":"E","InterfaceDescription":"x","Status":"Up"}`)...)
	got, err := parsePSOutput(in)
	if err != nil {
		t.Fatalf("parsePSOutput err=%v", err)
	}
	if len(got) != 1 {
		t.Errorf("len=%d, want 1", len(got))
	}
}

func TestParsePSOutput_Empty(t *testing.T) {
	got, err := parsePSOutput([]byte("  \r\n"))
	if err != nil {
		t.Fatalf("err=%v", err)
	}
	if len(got) != 0 {
		t.Errorf("want empty, got %+v", got)
	}
}
