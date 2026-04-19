//go:build linux

package dns

import (
	"context"
	"errors"
	"strings"
	"testing"
)

func TestLinuxManager_Resolvectl_SetResolver_MultiInterface(t *testing.T) {
	var setCalls []string

	runner := func(ctx context.Context, name string, args ...string) ([]byte, error) {
		key := name + " " + strings.Join(args, " ")
		if strings.Contains(key, "which resolvectl") {
			return []byte("/usr/bin/resolvectl\n"), nil
		}
		if name == "resolvectl" && len(args) > 0 && args[0] == "dns" && len(args) == 1 {
			return []byte("Link 2 (eth0): 192.168.1.1\nLink 3 (wlan0): 8.8.8.8\n"), nil
		}
		if name == "resolvectl" && len(args) > 0 && args[0] == "dns" && len(args) > 1 {
			setCalls = append(setCalls, strings.Join(args, " "))
			return []byte(""), nil
		}
		return nil, errors.New("unexpected command: " + key)
	}

	mgr := newManagerWithRunner(runner).(*linuxManager)
	ctx := context.Background()

	if err := mgr.SetResolver(ctx, "127.0.0.1"); err != nil {
		t.Fatalf("SetResolver: %v", err)
	}

	if len(setCalls) != 2 {
		t.Fatalf("expected 2 set calls (eth0 + wlan0), got %d: %v", len(setCalls), setCalls)
	}

	if !strings.Contains(setCalls[0], "eth0 127.0.0.1") {
		t.Errorf("expected 'eth0 127.0.0.1', got: %s", setCalls[0])
	}
	if !strings.Contains(setCalls[1], "wlan0 127.0.0.1") {
		t.Errorf("expected 'wlan0 127.0.0.1', got: %s", setCalls[1])
	}

	if mgr.OriginalResolver() == "" {
		t.Error("OriginalResolver should not be empty after SetResolver")
	}
}

func TestLinuxManager_Resolvectl_RestoreResolver_MultiInterface(t *testing.T) {
	var revertCalls []string

	runner := func(ctx context.Context, name string, args ...string) ([]byte, error) {
		key := name + " " + strings.Join(args, " ")
		if strings.Contains(key, "which resolvectl") {
			return []byte("/usr/bin/resolvectl\n"), nil
		}
		if name == "resolvectl" && len(args) > 0 && args[0] == "revert" {
			revertCalls = append(revertCalls, strings.Join(args, " "))
			return []byte(""), nil
		}
		return nil, errors.New("unexpected command: " + key)
	}

	mgr := newManagerWithRunner(runner).(*linuxManager)
	mgr.originalDNS = "Link 2 (eth0): 192.168.1.1\nLink 3 (wlan0): 8.8.8.8"

	ctx := context.Background()
	if err := mgr.RestoreResolver(ctx); err != nil {
		t.Fatalf("RestoreResolver: %v", err)
	}

	if len(revertCalls) != 2 {
		t.Fatalf("expected 2 revert calls, got %d: %v", len(revertCalls), revertCalls)
	}

	if mgr.OriginalResolver() != "" {
		t.Error("OriginalResolver should be empty after restore")
	}
}

func TestLinuxManager_RestoreResolver_NoOp(t *testing.T) {
	runner := func(ctx context.Context, name string, args ...string) ([]byte, error) {
		if strings.Contains(name+" "+strings.Join(args, " "), "which resolvectl") {
			return nil, errors.New("not found")
		}
		return nil, errors.New("unexpected command")
	}

	mgr := newManagerWithRunner(runner).(*linuxManager)
	ctx := context.Background()

	if err := mgr.RestoreResolver(ctx); err != nil {
		t.Fatalf("RestoreResolver (no-op): %v", err)
	}
}

func TestLinuxManager_OriginalResolver_Empty(t *testing.T) {
	runner := func(ctx context.Context, name string, args ...string) ([]byte, error) {
		if strings.Contains(name+" "+strings.Join(args, " "), "which resolvectl") {
			return nil, errors.New("not found")
		}
		return nil, errors.New("unexpected command")
	}

	mgr := newManagerWithRunner(runner).(*linuxManager)

	if got := mgr.OriginalResolver(); got != "" {
		t.Errorf("OriginalResolver = %q, want empty", got)
	}
}

