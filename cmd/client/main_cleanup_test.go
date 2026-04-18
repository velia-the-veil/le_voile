package main

import (
	"context"
	"errors"
	"testing"
)

func TestRunCleanup_BothSucceed(t *testing.T) {
	origFw, origTun := cleanupFirewall, cleanupTun
	t.Cleanup(func() { cleanupFirewall, cleanupTun = origFw, origTun })

	cleanupFirewall = func(_ context.Context) (int, error) { return 0, nil }
	cleanupTun = func() error { return nil }

	if got := runCleanup(); got != 0 {
		t.Errorf("expected exit 0 when both clean, got %d", got)
	}
}

func TestRunCleanup_FirewallReportsCount(t *testing.T) {
	origFw, origTun := cleanupFirewall, cleanupTun
	t.Cleanup(func() { cleanupFirewall, cleanupTun = origFw, origTun })

	cleanupFirewall = func(_ context.Context) (int, error) { return 11, nil }
	cleanupTun = func() error { return nil }

	if got := runCleanup(); got != 0 {
		t.Errorf("expected exit 0 when removed=11, got %d", got)
	}
}

func TestRunCleanup_FirewallErrorExits1(t *testing.T) {
	origFw, origTun := cleanupFirewall, cleanupTun
	t.Cleanup(func() { cleanupFirewall, cleanupTun = origFw, origTun })

	cleanupFirewall = func(_ context.Context) (int, error) {
		return 0, errors.New("WFP unavailable")
	}
	cleanupTun = func() error { return nil }

	if got := runCleanup(); got != 1 {
		t.Errorf("expected exit 1 on firewall error, got %d", got)
	}
}

func TestRunCleanup_TunErrorExits1(t *testing.T) {
	origFw, origTun := cleanupFirewall, cleanupTun
	t.Cleanup(func() { cleanupFirewall, cleanupTun = origFw, origTun })

	cleanupFirewall = func(_ context.Context) (int, error) { return 0, nil }
	cleanupTun = func() error { return errors.New("netlink ENOENT") }

	if got := runCleanup(); got != 1 {
		t.Errorf("expected exit 1 on TUN error, got %d", got)
	}
}

func TestRunCleanup_BothFailExits1Once(t *testing.T) {
	origFw, origTun := cleanupFirewall, cleanupTun
	t.Cleanup(func() { cleanupFirewall, cleanupTun = origFw, origTun })

	cleanupFirewall = func(_ context.Context) (int, error) { return 0, errors.New("a") }
	cleanupTun = func() error { return errors.New("b") }

	if got := runCleanup(); got != 1 {
		t.Errorf("expected exit 1 when both fail, got %d", got)
	}
}

func TestRunCleanup_HasTimeout(t *testing.T) {
	origFw, origTun := cleanupFirewall, cleanupTun
	t.Cleanup(func() { cleanupFirewall, cleanupTun = origFw, origTun })

	cleanupFirewall = func(ctx context.Context) (int, error) {
		if _, ok := ctx.Deadline(); !ok {
			t.Error("expected runCleanup to pass a context with deadline")
		}
		return 0, nil
	}
	cleanupTun = func() error { return nil }

	if got := runCleanup(); got != 0 {
		t.Errorf("expected exit 0, got %d", got)
	}
}
