//go:build linux && integration

package firewall

import (
	"context"
	"net"
	"os"
	"os/exec"
	"strings"
	"testing"
)

// requireNftRoot skips the test if nft is absent or the process lacks root/CAP_NET_ADMIN.
func requireNftRoot(t *testing.T) {
	t.Helper()
	if os.Geteuid() != 0 {
		t.Skip("requires root or CAP_NET_ADMIN")
	}
	if _, err := exec.LookPath("nft"); err != nil {
		t.Skip("nft binary not found")
	}
}

func TestIntegration_ActivateDeactivate(t *testing.T) {
	requireNftRoot(t)

	ctx := context.Background()
	fw := New(nil, Options{})

	// Activate
	err := fw.Activate(ctx, ActivateParams{Mode: ModeFull, RelayIP: net.ParseIP("198.51.100.42"), TunName: "levoile0"})
	if err != nil {
		t.Fatalf("Activate failed: %v", err)
	}

	// Verify ruleset loaded
	out, err := exec.Command("nft", "list", "ruleset").CombinedOutput()
	if err != nil {
		t.Fatalf("nft list ruleset: %v", err)
	}
	ruleset := string(out)
	if !strings.Contains(ruleset, "inet levoile") {
		t.Errorf("ruleset missing 'inet levoile':\n%s", ruleset)
	}

	// IsActive should be true
	active, err := fw.IsActive(ctx)
	if err != nil {
		t.Fatalf("IsActive: %v", err)
	}
	if !active {
		t.Error("expected IsActive=true after Activate")
	}

	// Deactivate
	if err := fw.Deactivate(ctx); err != nil {
		t.Fatalf("Deactivate: %v", err)
	}

	// Verify ruleset gone
	active, err = fw.IsActive(ctx)
	if err != nil {
		t.Fatalf("IsActive after Deactivate: %v", err)
	}
	if active {
		t.Error("expected IsActive=false after Deactivate")
	}
}

func TestIntegration_OrphanReplacement(t *testing.T) {
	requireNftRoot(t)

	ctx := context.Background()
	log := &testLogger{}
	fw := New(log, Options{}).(*nftFirewall)

	// First Activate — installs rules
	if err := fw.Activate(ctx, ActivateParams{Mode: ModeFull, RelayIP: net.ParseIP("198.51.100.42"), TunName: "levoile0"}); err != nil {
		t.Fatalf("first Activate: %v", err)
	}

	// Simulate crash: do NOT Deactivate. Re-Activate should detect orphan.
	if err := fw.Activate(ctx, ActivateParams{Mode: ModeFull, RelayIP: net.ParseIP("198.51.100.42"), TunName: "levoile0"}); err != nil {
		t.Fatalf("second Activate (orphan replace): %v", err)
	}

	foundOrphanWarn := false
	for _, w := range log.warns {
		if strings.Contains(w, "orphan") {
			foundOrphanWarn = true
		}
	}
	if !foundOrphanWarn {
		t.Error("expected WARN about orphan ruleset on second Activate")
	}

	// Cleanup
	_ = fw.Deactivate(ctx)
}

func TestIntegration_NftAbsent(t *testing.T) {
	if os.Geteuid() != 0 {
		t.Skip("requires root")
	}

	// Override PATH to exclude nft
	origPath := os.Getenv("PATH")
	defer os.Setenv("PATH", origPath)
	os.Setenv("PATH", "/nonexistent")

	// Also override lookPathFunc
	origLookPath := lookPathFunc
	defer func() { lookPathFunc = origLookPath }()
	lookPathFunc = exec.LookPath // use real LookPath but with empty PATH

	fw := New(nil, Options{})
	err := fw.Activate(context.Background(), ActivateParams{Mode: ModeFull, RelayIP: net.ParseIP("1.2.3.4"), TunName: "levoile0"})
	if err == nil {
		t.Fatal("expected error when nft is absent")
	}
}
