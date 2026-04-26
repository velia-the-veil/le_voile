//go:build linux

package ipchandler

import (
	"context"
	"net"
	"path/filepath"
	"sync/atomic"
	"testing"

	"github.com/velia-the-veil/le_voile/linux/internal/config"
	"github.com/velia-the-veil/le_voile/linux/internal/firewall"
	"github.com/velia-the-veil/le_voile/linux/internal/ipc"
	svc "github.com/velia-the-veil/le_voile/linux/internal/service"
)

// stubFirewall mirrors the one in internal/service tests but lives here so we
// can exercise the cross-package wiring without depending on test-only types.
type stubFirewall struct {
	activated  atomic.Bool
	activates  atomic.Int64
	deacts     atomic.Int64
}

func (s *stubFirewall) Activate(_ context.Context, _ firewall.ActivateParams) error {
	s.activates.Add(1)
	s.activated.Store(true)
	return nil
}
func (s *stubFirewall) Deactivate(_ context.Context) error {
	s.deacts.Add(1)
	s.activated.Store(false)
	return nil
}
func (s *stubFirewall) IsActive(_ context.Context) (bool, error)      { return s.activated.Load(), nil }
func (s *stubFirewall) SetIPv6Policy(_ context.Context, _ bool) error { return nil }
func (s *stubFirewall) CleanupOrphans(_ context.Context) (int, error) { return 0, nil }
func (s *stubFirewall) AlteredCh() <-chan struct{}                    { return nil }

// E2E for Story 5.9: an IPC client request for set_killswitch_mode flows
// through the Handle dispatcher → service.SetKillSwitchMode → stub firewall →
// killSwitchPersist callback. We validate every layer reacts as designed.
func TestE2E_KillSwitch_NormalToDegraded_PersistsAndDeactivates(t *testing.T) {
	cfgPath := filepath.Join(t.TempDir(), "config.toml")
	if err := (&config.Config{
		TUN:      config.TUNConfig{Name: "levoile0", MTU: 1420},
		Firewall: config.FirewallConfig{EnableKillSwitch: true},
	}).Save(cfgPath); err != nil {
		t.Fatal(err)
	}

	prg := svc.NewProgram(svc.Config{FirewallEnabled: true})
	// Strict-auth gate (2026-04 flip) requires a non-empty req.Auth, and
	// handleSetKillSwitchMode additionally calls VerifyCtlToken on non-empty
	// Auth — so seed the same token in both places for this E2E path.
	const testToken = "token-32-bytes-secret-aaaaaaaaaa"
	prg.SetCtlToken([]byte(testToken))
	stub := &stubFirewall{}
	stub.activated.Store(true)
	prg.InjectFirewallForTest(stub, net.IPv4(1, 2, 3, 4))

	var persistCalls atomic.Int64
	prg.SetKillSwitchPersister(func(enabled bool) error {
		persistCalls.Add(1)
		// Mirror the behavior of cmd/client.persistFirewallEnabled — load,
		// modify, save the on-disk TOML so the assertion can read the bit.
		cfg, err := config.Load(cfgPath)
		if err != nil {
			return err
		}
		cfg.Firewall.EnableKillSwitch = enabled
		return cfg.Save(cfgPath)
	})

	resp := Handle(prg,
		ipc.Request{Action: ipc.ActionSetKillSwitchMode, Value: ipc.KillSwitchModeDegraded, Auth: testToken},
		Options{ConfigPathFn: func() string { return cfgPath }})

	if resp.Status != ipc.StatusOK {
		t.Fatalf("response status = %q, want ok (error=%s)", resp.Status, resp.Error)
	}
	if resp.KillSwitchMode != ipc.KillSwitchModeDegraded {
		t.Errorf("response mode = %q, want degraded", resp.KillSwitchMode)
	}
	if persistCalls.Load() != 1 {
		t.Errorf("persist callback fired %d times, want 1", persistCalls.Load())
	}
	if stub.activated.Load() {
		t.Error("firewall stub still active after degraded switch")
	}
	if stub.deacts.Load() != 1 {
		t.Errorf("firewall.Deactivate calls = %d, want 1", stub.deacts.Load())
	}
	if prg.KillSwitchMode() != ipc.KillSwitchModeDegraded {
		t.Errorf("program mode = %q, want degraded", prg.KillSwitchMode())
	}

	cfg, err := config.Load(cfgPath)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Firewall.EnableKillSwitch {
		t.Error("on-disk enable_killswitch must be false after degraded switch")
	}

	// Round-trip: switch back to normal, expect Activate + persist again.
	resp2 := Handle(prg,
		ipc.Request{Action: ipc.ActionSetKillSwitchMode, Value: ipc.KillSwitchModeNormal},
		Options{ConfigPathFn: func() string { return cfgPath }})
	if resp2.Status != ipc.StatusOK {
		t.Fatalf("round-trip status = %q (error=%s)", resp2.Status, resp2.Error)
	}
	if !stub.activated.Load() {
		t.Error("firewall stub must be re-activated after normal switch")
	}
	if persistCalls.Load() != 2 {
		t.Errorf("persist callback fired %d times after round-trip, want 2", persistCalls.Load())
	}
}
