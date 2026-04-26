//go:build windows

package service

import (
	"errors"
	"testing"

	"github.com/velia-the-veil/le_voile/internal/preflight"
)

// fakeDetector permet d'injecter un verdict preflight sans toucher au réseau.
type fakeDetector struct {
	err error
}

func (f *fakeDetector) DetectConcurrentVPN() error { return f.err }

func TestProgram_DetectConcurrentVPN_Clean(t *testing.T) {
	prg := NewProgram(Config{RelayDomain: "x", RelayPubKey: "AAAA"})
	prg.SetPreflightDetector(&fakeDetector{err: nil})

	if e := prg.DetectConcurrentVPN(); e != nil {
		t.Errorf("DetectConcurrentVPN() = %v, want nil", e)
	}
	if prg.ConcurrentVPNError() != nil {
		t.Errorf("ConcurrentVPNError = %v, want nil", prg.ConcurrentVPNError())
	}
}

func TestProgram_DetectConcurrentVPN_Positive(t *testing.T) {
	prg := NewProgram(Config{RelayDomain: "x", RelayPubKey: "AAAA"})
	want := &preflight.ErrConcurrentVPN{InterfaceName: "wg0", MatchedPattern: "wg"}
	prg.SetPreflightDetector(&fakeDetector{err: want})

	got := prg.DetectConcurrentVPN()
	if got == nil {
		t.Fatal("DetectConcurrentVPN() = nil, want *ErrConcurrentVPN")
	}
	if got.InterfaceName != "wg0" || got.MatchedPattern != "wg" {
		t.Errorf("got %+v, want %+v", got, want)
	}
	if stored := prg.ConcurrentVPNError(); stored == nil || stored.InterfaceName != "wg0" {
		t.Errorf("ConcurrentVPNError = %+v", stored)
	}
}

func TestProgram_DetectConcurrentVPN_ResetOnClean(t *testing.T) {
	prg := NewProgram(Config{RelayDomain: "x", RelayPubKey: "AAAA"})
	// Première passe : positive
	prg.SetPreflightDetector(&fakeDetector{err: &preflight.ErrConcurrentVPN{InterfaceName: "wg0", MatchedPattern: "wg"}})
	if e := prg.DetectConcurrentVPN(); e == nil {
		t.Fatal("want err on first pass")
	}
	// Deuxième passe : nominal → doit clear l'état
	prg.SetPreflightDetector(&fakeDetector{err: nil})
	if e := prg.DetectConcurrentVPN(); e != nil {
		t.Fatalf("second pass = %v, want nil", e)
	}
	if prg.ConcurrentVPNError() != nil {
		t.Error("ConcurrentVPNError not cleared after clean scan")
	}
}

func TestProgram_DetectConcurrentVPN_GenericErrorIgnored(t *testing.T) {
	// Une erreur qui n'est PAS ErrConcurrentVPN (ex: lister error) doit
	// laisser l'état nil — pas de faux positif sur l'IPC.
	prg := NewProgram(Config{RelayDomain: "x", RelayPubKey: "AAAA"})
	prg.SetPreflightDetector(&fakeDetector{err: errors.New("unknown")})
	if e := prg.DetectConcurrentVPN(); e != nil {
		t.Errorf("got %v, want nil (generic err not surfaced)", e)
	}
	if prg.ConcurrentVPNError() != nil {
		t.Error("ConcurrentVPNError should be nil for generic errors")
	}
}
