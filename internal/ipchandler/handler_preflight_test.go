package ipchandler

import (
	"testing"

	"github.com/velia-the-veil/le_voile/internal/ipc"
	"github.com/velia-the-veil/le_voile/internal/preflight"
)

type fakeDetector struct{ err error }

func (f *fakeDetector) DetectConcurrentVPN() error { return f.err }

func TestHandle_Connect_ConcurrentVPN(t *testing.T) {
	// Un VPN concurrent est détecté → le Connect IPC doit être refusé
	// immédiatement, avec ConcurrentVPN=true et le message FR littéral, sans
	// toucher au tunnel (tc=nil doit rester sans effet : preflight passe avant).
	prg := newTestProgram()
	prg.SetPreflightDetector(&fakeDetector{
		err: &preflight.ErrConcurrentVPN{InterfaceName: "wg0", MatchedPattern: "wg"},
	})
	resp := Handle(prg, ipc.Request{Action: ipc.ActionConnect}, Options{})

	if resp.Status != ipc.StatusError {
		t.Errorf("Status=%q, want error", resp.Status)
	}
	if !resp.ConcurrentVPN {
		t.Errorf("ConcurrentVPN=false, want true")
	}
	want := "VPN concurrent détecté (wg0). Déconnectez-le pour utiliser Le Voile."
	if resp.Error != want {
		t.Errorf("Error=%q, want %q", resp.Error, want)
	}
}

func TestHandle_Connect_NoConcurrentVPN_FallsThrough(t *testing.T) {
	// Détection négative : on doit retomber sur service_not_ready (tunnel nil)
	// comme l'ancien comportement.
	prg := newTestProgram()
	prg.SetPreflightDetector(&fakeDetector{err: nil})
	resp := Handle(prg, ipc.Request{Action: ipc.ActionConnect}, Options{})
	if resp.Status != ipc.StatusError || resp.Error != "service_not_ready" {
		t.Errorf("got %q/%q, want error/service_not_ready", resp.Status, resp.Error)
	}
	if resp.ConcurrentVPN {
		t.Error("ConcurrentVPN=true on clean scan")
	}
}

func TestHandle_GetStatus_ConcurrentVPN(t *testing.T) {
	// Si le preflight au démarrage a détecté un VPN concurrent (état stocké
	// sur Program), GetStatus doit le surfacer sans tenter d'inspecter le
	// tunnel (qui n'existe pas puisque run() a court-circuité).
	prg := newTestProgram()
	prg.SetPreflightDetector(&fakeDetector{
		err: &preflight.ErrConcurrentVPN{InterfaceName: "wg0", MatchedPattern: "wg"},
	})
	// Simule le scan run() qui stocke l'erreur.
	_ = prg.DetectConcurrentVPN()

	resp := Handle(prg, ipc.Request{Action: ipc.ActionGetStatus}, Options{})
	if resp.Status != ipc.StatusError {
		t.Errorf("Status=%q, want error", resp.Status)
	}
	if !resp.ConcurrentVPN {
		t.Errorf("ConcurrentVPN=false, want true")
	}
	if resp.Error == "" {
		t.Error("Error empty")
	}
}
