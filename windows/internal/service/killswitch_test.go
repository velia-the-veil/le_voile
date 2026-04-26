//go:build windows

package service

import (
	"context"
	"errors"
	"net"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/velia-the-veil/le_voile/internal/firewall"
)

// killSwitchHarness wires a Program with stub firewall + persister and lets
// each test customize captive state, mode pre-conditions, and persistence
// outcome without re-implementing the boilerplate.
type killSwitchHarness struct {
	prg          *Program
	fw           *stubFirewall
	persistCalls atomic.Int64
	persistErr   error
	persistedTo  atomic.Bool
}

func newKillSwitchHarness(t *testing.T, initialFirewallEnabled bool) *killSwitchHarness {
	t.Helper()

	rec := &callRecord{}
	stubFW := &stubFirewall{rec: rec, activated: initialFirewallEnabled}

	origFW := firewallFactory
	firewallFactory = func(_ firewall.Logger, _ firewall.Options) firewall.Firewall { return stubFW }
	t.Cleanup(func() { firewallFactory = origFW })

	p := NewProgram(Config{
		TUNEnabled:      true,
		FirewallEnabled: initialFirewallEnabled,
	})
	p.tunDev = &mockTUN{name: "levoile0", mtu: 1420}
	p.firewallRelayIP.Store(net.IPv4(1, 2, 3, 4))
	if initialFirewallEnabled {
		p.firewallMgr = stubFW
	}

	h := &killSwitchHarness{prg: p, fw: stubFW}
	p.SetKillSwitchPersister(func(enabled bool) error {
		h.persistCalls.Add(1)
		h.persistedTo.Store(enabled)
		return h.persistErr
	})
	return h
}

// Story 5.9 AC1+AC2 — normal -> degraded calls firewall.Deactivate, flips
// in-memory flag, persists once.
func TestSetKillSwitchMode_NormalToDegraded(t *testing.T) {
	h := newKillSwitchHarness(t, true)

	if err := h.prg.SetKillSwitchMode(context.Background(), KillSwitchModeDegraded, "ui"); err != nil {
		t.Fatalf("SetKillSwitchMode: %v", err)
	}
	if h.prg.KillSwitchMode() != KillSwitchModeDegraded {
		t.Errorf("mode = %q, want %q", h.prg.KillSwitchMode(), KillSwitchModeDegraded)
	}
	if h.fw.activated {
		t.Error("firewall must be deactivated after switch to degraded")
	}
	if h.persistCalls.Load() != 1 {
		t.Errorf("persist called %d times, want 1", h.persistCalls.Load())
	}
	if h.persistedTo.Load() != false {
		t.Error("persist value = true, want false")
	}
}

// Story 5.9 AC4 — degraded -> normal calls firewall.Activate.
func TestSetKillSwitchMode_DegradedToNormal(t *testing.T) {
	h := newKillSwitchHarness(t, false)
	// Reset rec to focus on the next transition only.
	h.fw.rec = &callRecord{}

	if err := h.prg.SetKillSwitchMode(context.Background(), KillSwitchModeNormal, "auto-reconnect"); err != nil {
		t.Fatalf("SetKillSwitchMode: %v", err)
	}
	if h.prg.KillSwitchMode() != KillSwitchModeNormal {
		t.Errorf("mode = %q, want normal", h.prg.KillSwitchMode())
	}
	if !h.fw.activated {
		t.Error("firewall must be activated after switch to normal")
	}
	calls := h.fw.rec.snapshot()
	if len(calls) != 1 || calls[0] != "firewall.Activate" {
		t.Errorf("calls = %v, want [firewall.Activate]", calls)
	}
}

// Story 5.9 AC7 — captive portal active refuses any mode change.
func TestSetKillSwitchMode_RefusedDuringCaptive(t *testing.T) {
	h := newKillSwitchHarness(t, true)
	h.prg.captivePortal.Store(true)

	err := h.prg.SetKillSwitchMode(context.Background(), KillSwitchModeDegraded, "ui")
	if !errors.Is(err, ErrKillSwitchCaptiveActive) {
		t.Errorf("err = %v, want ErrKillSwitchCaptiveActive", err)
	}
	if !h.fw.activated {
		t.Error("firewall must remain active when captive blocks the change")
	}
	if h.persistCalls.Load() != 0 {
		t.Errorf("persist called %d times, want 0", h.persistCalls.Load())
	}
}

// Story 5.9 — invalid mode is rejected before any side effect.
func TestSetKillSwitchMode_InvalidMode(t *testing.T) {
	h := newKillSwitchHarness(t, true)

	err := h.prg.SetKillSwitchMode(context.Background(), "bogus", "ui")
	if !errors.Is(err, ErrKillSwitchInvalidMode) {
		t.Errorf("err = %v, want ErrKillSwitchInvalidMode", err)
	}
	if h.persistCalls.Load() != 0 {
		t.Error("persist must not be called on invalid mode")
	}
}

// Story 5.9 — same-mode call is a no-op (no firewall transition, no persist).
func TestSetKillSwitchMode_SameModeNoop(t *testing.T) {
	h := newKillSwitchHarness(t, true)
	h.fw.rec = &callRecord{}

	if err := h.prg.SetKillSwitchMode(context.Background(), KillSwitchModeNormal, "ui"); err != nil {
		t.Fatalf("SetKillSwitchMode: %v", err)
	}
	if h.persistCalls.Load() != 0 {
		t.Errorf("persist called %d times for same-mode noop, want 0", h.persistCalls.Load())
	}
	if calls := h.fw.rec.snapshot(); len(calls) != 0 {
		t.Errorf("firewall calls = %v, want none", calls)
	}
}

// Story 5.9 AC2 atomicity — when persist fails, the firewall transition is
// reverted and the in-memory flag is restored.
func TestSetKillSwitchMode_PersistFailureRollsBack(t *testing.T) {
	h := newKillSwitchHarness(t, true)
	h.persistErr = errors.New("disk full")

	err := h.prg.SetKillSwitchMode(context.Background(), KillSwitchModeDegraded, "ui")
	if err == nil {
		t.Fatal("SetKillSwitchMode must return error when persist fails")
	}
	if !strings.Contains(err.Error(), "killswitch persist") {
		t.Errorf("err = %v, want wrapped 'killswitch persist'", err)
	}

	// Mode stays normal (rolled back).
	if h.prg.KillSwitchMode() != KillSwitchModeNormal {
		t.Errorf("mode = %q after rollback, want normal", h.prg.KillSwitchMode())
	}
	// Firewall ended Activate'd again (rollback Activate).
	if !h.fw.activated {
		t.Error("firewall must be re-activated after rollback")
	}
}

// Story 5.9 AC4 — normal restore needs TUN + relayIP; without them, fail clean.
func TestSetKillSwitchMode_RestoreRequiresTunnel(t *testing.T) {
	h := newKillSwitchHarness(t, false)
	h.prg.tunDev = nil // simulate "tunnel never up since boot"

	err := h.prg.SetKillSwitchMode(context.Background(), KillSwitchModeNormal, "ctl")
	if !errors.Is(err, ErrKillSwitchNotConnected) {
		t.Errorf("err = %v, want ErrKillSwitchNotConnected", err)
	}
}

// Story 5.9 AC4 — MaybeRestoreKillSwitch is a no-op when already normal.
func TestMaybeRestoreKillSwitch_NoopWhenAlreadyNormal(t *testing.T) {
	h := newKillSwitchHarness(t, true)
	h.prg.MaybeRestoreKillSwitch(context.Background(), "auto-reconnect")
	if h.persistCalls.Load() != 0 {
		t.Errorf("persist called %d times, want 0", h.persistCalls.Load())
	}
}

// Story 5.9 AC4 — MaybeRestoreKillSwitch flips degraded -> normal.
func TestMaybeRestoreKillSwitch_FlipsToNormal(t *testing.T) {
	h := newKillSwitchHarness(t, false)
	h.fw.rec = &callRecord{}

	h.prg.MaybeRestoreKillSwitch(context.Background(), "auto-reconnect")
	if h.prg.KillSwitchMode() != KillSwitchModeNormal {
		t.Errorf("mode = %q, want normal", h.prg.KillSwitchMode())
	}
	if h.persistCalls.Load() != 1 {
		t.Errorf("persist called %d times, want 1", h.persistCalls.Load())
	}
}

// Story 5.9 AC7 — MaybeRestoreKillSwitch is a no-op during captive.
func TestMaybeRestoreKillSwitch_NoopDuringCaptive(t *testing.T) {
	h := newKillSwitchHarness(t, false)
	h.prg.captivePortal.Store(true)

	h.prg.MaybeRestoreKillSwitch(context.Background(), "auto-reconnect")
	if h.prg.KillSwitchMode() != KillSwitchModeDegraded {
		t.Errorf("mode = %q, want degraded (no-op)", h.prg.KillSwitchMode())
	}
}

// Story 5.9 AC5 — VerifyCtlToken uses constant-time compare and rejects empty.
func TestVerifyCtlToken(t *testing.T) {
	p := NewProgram(Config{})

	if p.VerifyCtlToken("anything") {
		t.Error("VerifyCtlToken must reject when no token is set")
	}
	if p.HasCtlToken() {
		t.Error("HasCtlToken = true with no token set")
	}

	p.SetCtlToken([]byte("secret-token-32-bytes-long-xxxxxx"))
	if !p.HasCtlToken() {
		t.Error("HasCtlToken = false after SetCtlToken")
	}
	if p.VerifyCtlToken("secret-token-32-bytes-long-xxxxxx") != true {
		t.Error("VerifyCtlToken rejected correct token")
	}
	if p.VerifyCtlToken("secret-token-32-bytes-long-WRONG!") {
		t.Error("VerifyCtlToken accepted wrong token of equal length")
	}
	if p.VerifyCtlToken("short") {
		t.Error("VerifyCtlToken accepted short token")
	}
	if p.VerifyCtlToken("") {
		t.Error("VerifyCtlToken accepted empty input")
	}

	p.SetCtlToken(nil)
	if p.HasCtlToken() {
		t.Error("HasCtlToken = true after SetCtlToken(nil)")
	}
}
