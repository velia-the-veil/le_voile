package dns

import (
	"context"
	"errors"
	"strings"
	"testing"
)

func TestKillSwitch_ReactivateAfterFailedDeactivate(t *testing.T) {
	mock := &mockDNSManager{originalAddr: "8.8.8.8"}
	stopProxy := func() {}
	startProxy := func(_ context.Context) error {
		return errors.New("bind failed")
	}

	ks := NewKillSwitch(mock, stopProxy, startProxy)
	ks.SetForceResolver(func(_ context.Context, _ string) error { return nil })
	ks.active = true

	// Deactivate should fail because startProxy fails.
	err := ks.Deactivate(context.Background())
	if err == nil {
		t.Fatal("expected deactivate error, got nil")
	}

	// Kill switch should remain active after failed deactivation.
	if !ks.IsActive() {
		t.Error("expected kill switch to remain active after failed deactivation")
	}

	// Should be able to re-activate (returns already active error).
	err = ks.Activate(context.Background())
	if !errors.Is(err, ErrKillSwitchAlreadyActive) {
		t.Errorf("expected ErrKillSwitchAlreadyActive, got %v", err)
	}

	// Now fix startProxy and deactivate again.
	ks.startProxy = func(_ context.Context) error { return nil }
	err = ks.Deactivate(context.Background())
	if err != nil {
		t.Fatalf("expected successful deactivation, got %v", err)
	}
	if ks.IsActive() {
		t.Error("expected kill switch to be inactive after successful deactivation")
	}
}

func TestKillSwitch_Activate_SetResolverFails_NoRollback(t *testing.T) {
	// When SetResolver fails during Activate, the kill switch should NOT
	// be marked active (no partial state).
	mock := &mockDNSManager{
		originalAddr:   "", // will call SetResolver
		setResolverErr: errors.New("permission denied"),
	}

	var stopCalled int
	stopProxy := func() { stopCalled++ }
	startProxy := func(_ context.Context) error { return nil }

	ks := NewKillSwitch(mock, stopProxy, startProxy)

	err := ks.Activate(context.Background())
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !strings.Contains(err.Error(), "activate") {
		t.Errorf("error should contain 'activate', got %q", err.Error())
	}

	// stopProxy was called (DNS blocked), but activation failed.
	if stopCalled != 1 {
		t.Errorf("stopProxy called %d times, want 1", stopCalled)
	}

	// Kill switch should NOT be active.
	if ks.IsActive() {
		t.Error("kill switch should not be active when SetResolver fails")
	}

	// Proxy was stopped but SetResolver failed — this is the security-critical
	// state where DNS is blocked but kill switch reports inactive.
	// Caller should handle this by checking the error.
}

func TestKillSwitch_SetForceResolver_EffectOnActivate(t *testing.T) {
	var forceCalledWith string
	mock := &mockDNSManager{originalAddr: "8.8.8.8"} // original already saved
	stopProxy := func() {}
	startProxy := func(_ context.Context) error { return nil }

	ks := NewKillSwitch(mock, stopProxy, startProxy)

	// Without SetForceResolver, forceResolver is nil.
	// When originalAddr is set and forceResolver is nil, Activate still succeeds
	// but doesn't call forceResolver.
	err := ks.Activate(context.Background())
	if err != nil {
		t.Fatalf("Activate without forceResolver: %v", err)
	}
	if mock.setCalled != 0 {
		t.Errorf("SetResolver should not be called when original is set, got %d calls", mock.setCalled)
	}

	// Reset state.
	ks.active = false

	// Set forceResolver and verify it gets called.
	ks.SetForceResolver(func(_ context.Context, addr string) error {
		forceCalledWith = addr
		return nil
	})

	err = ks.Activate(context.Background())
	if err != nil {
		t.Fatalf("Activate with forceResolver: %v", err)
	}
	if forceCalledWith != "127.0.0.1" {
		t.Errorf("forceResolver called with %q, want %q", forceCalledWith, "127.0.0.1")
	}
}

func TestKillSwitch_Activate_Deactivate_FullCycle(t *testing.T) {
	// Test a full cycle: activate -> deactivate -> activate -> deactivate.
	mock := &mockDNSManager{originalAddr: ""}
	var stopCount, startCount int

	ks := NewKillSwitch(mock,
		func() { stopCount++ },
		func(_ context.Context) error { startCount++; return nil },
	)

	ctx := context.Background()

	// First activate — calls SetResolver (originalAddr empty).
	if err := ks.Activate(ctx); err != nil {
		t.Fatalf("first Activate: %v", err)
	}
	if stopCount != 1 {
		t.Errorf("stopCount = %d, want 1", stopCount)
	}
	if mock.setCalled != 1 {
		t.Errorf("SetResolver called %d, want 1", mock.setCalled)
	}

	// Set originalAddr to simulate SetResolver having saved it.
	mock.originalAddr = "8.8.8.8"

	// Deactivate.
	if err := ks.Deactivate(ctx); err != nil {
		t.Fatalf("first Deactivate: %v", err)
	}
	if startCount != 1 {
		t.Errorf("startCount = %d, want 1", startCount)
	}

	// Second activate — original is now set, so it should not call SetResolver again.
	ks.SetForceResolver(func(_ context.Context, _ string) error { return nil })
	if err := ks.Activate(ctx); err != nil {
		t.Fatalf("second Activate: %v", err)
	}
	if mock.setCalled != 1 {
		t.Errorf("SetResolver should not be called again, got %d", mock.setCalled)
	}
	if stopCount != 2 {
		t.Errorf("stopCount = %d, want 2", stopCount)
	}

	// Second deactivate.
	if err := ks.Deactivate(ctx); err != nil {
		t.Fatalf("second Deactivate: %v", err)
	}
	if startCount != 2 {
		t.Errorf("startCount = %d, want 2", startCount)
	}
}
