//go:build linux

package firewall

import (
	"context"
	"errors"
	"fmt"
	"net"
	"strings"
	"testing"
)

// testLogger captures log messages for assertions.
type testLogger struct {
	infos  []string
	warns  []string
	errs   []string
	debugs []string
}

func (l *testLogger) Infof(f string, a ...any)  { l.infos = append(l.infos, fmt.Sprintf(f, a...)) }
func (l *testLogger) Warnf(f string, a ...any)  { l.warns = append(l.warns, fmt.Sprintf(f, a...)) }
func (l *testLogger) Errorf(f string, a ...any) { l.errs = append(l.errs, fmt.Sprintf(f, a...)) }
func (l *testLogger) Debugf(f string, a ...any) { l.debugs = append(l.debugs, fmt.Sprintf(f, a...)) }

// cmdDispatcher routes commands to specific handlers based on args.
type cmdDispatcher struct {
	handlers map[string]func() ([]byte, error)
}

func (d *cmdDispatcher) run(_ context.Context, name string, args ...string) ([]byte, error) {
	key := name + " " + strings.Join(args, " ")
	if h, ok := d.handlers[key]; ok {
		return h()
	}
	return nil, fmt.Errorf("unstubbed command: %s", key)
}

func TestActivate_Nominal(t *testing.T) {
	orig := lookPathFunc
	defer func() { lookPathFunc = orig }()
	lookPathFunc = func(string) (string, error) { return "/usr/sbin/nft", nil }

	log := &testLogger{}
	dispatch := &cmdDispatcher{handlers: map[string]func() ([]byte, error){
		// detectNft probe
		"nft list ruleset": func() ([]byte, error) { return []byte(""), nil },
		// IsActive pre-check (no orphan)
		"nft list table inet levoile": func() ([]byte, error) {
			return []byte("No such file or directory"), fmt.Errorf("exit 1")
		},
	}}

	// After apply, IsActive should succeed
	applied := false
	stdinFn := func(_ context.Context, _ string, _ []string, stdin string) ([]byte, error) {
		if !strings.Contains(stdin, "flush table inet levoile") {
			t.Error("stdin missing flush command")
		}
		applied = true
		// After apply, update handler so IsActive returns true
		dispatch.handlers["nft list table inet levoile"] = func() ([]byte, error) {
			return []byte("table inet levoile { ... }"), nil
		}
		return nil, nil
	}

	fw := &nftFirewall{log: log, run: dispatch.run, stdinRun: stdinFn}
	err := fw.Activate(context.Background(), ActivateParams{Mode: ModeFull, RelayIP: net.ParseIP("198.51.100.42"), TunName: "levoile0"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !applied {
		t.Error("applyRuleset was not called")
	}
	if len(log.infos) == 0 {
		t.Error("expected INFO log for activation")
	}
}

func TestActivate_DetectNftFails(t *testing.T) {
	orig := lookPathFunc
	defer func() { lookPathFunc = orig }()
	lookPathFunc = func(string) (string, error) {
		return "", fmt.Errorf("not found")
	}

	fw := &nftFirewall{log: &testLogger{}, run: stubRunner(nil, nil)}
	err := fw.Activate(context.Background(), ActivateParams{Mode: ModeFull, RelayIP: net.ParseIP("1.2.3.4"), TunName: "levoile0"})
	if err == nil {
		t.Fatal("expected error when nft missing")
	}
	if !errors.Is(err, ErrNftablesUnavailable) {
		t.Errorf("expected ErrNftablesUnavailable, got: %v", err)
	}
}

func TestActivate_OrphanDetected(t *testing.T) {
	orig := lookPathFunc
	defer func() { lookPathFunc = orig }()
	lookPathFunc = func(string) (string, error) { return "/usr/sbin/nft", nil }

	log := &testLogger{}
	dispatch := &cmdDispatcher{handlers: map[string]func() ([]byte, error){
		"nft list ruleset":            func() ([]byte, error) { return nil, nil },
		"nft list table inet levoile": func() ([]byte, error) { return []byte("table inet levoile { ... }"), nil },
	}}

	stdinFn := func(_ context.Context, _ string, _ []string, _ string) ([]byte, error) {
		return nil, nil
	}

	fw := &nftFirewall{log: log, run: dispatch.run, stdinRun: stdinFn}
	err := fw.Activate(context.Background(), ActivateParams{Mode: ModeFull, RelayIP: net.ParseIP("10.0.0.1"), TunName: "levoile0"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	foundOrphanWarn := false
	for _, w := range log.warns {
		if strings.Contains(w, "orphan") {
			foundOrphanWarn = true
		}
	}
	if !foundOrphanWarn {
		t.Error("expected WARN log about orphan ruleset")
	}
}

func TestActivate_ShelloutFails(t *testing.T) {
	orig := lookPathFunc
	defer func() { lookPathFunc = orig }()
	lookPathFunc = func(string) (string, error) { return "/usr/sbin/nft", nil }

	dispatch := &cmdDispatcher{handlers: map[string]func() ([]byte, error){
		"nft list ruleset": func() ([]byte, error) { return nil, nil },
		"nft list table inet levoile": func() ([]byte, error) {
			return []byte("No such file or directory"), fmt.Errorf("exit 1")
		},
	}}

	stdinFn := func(_ context.Context, _ string, _ []string, _ string) ([]byte, error) {
		return []byte("Error: syntax error"), fmt.Errorf("exit status 1")
	}

	fw := &nftFirewall{log: &testLogger{}, run: dispatch.run, stdinRun: stdinFn}
	err := fw.Activate(context.Background(), ActivateParams{Mode: ModeFull, RelayIP: net.ParseIP("1.2.3.4"), TunName: "levoile0"})
	if err == nil {
		t.Fatal("expected error for shellout failure")
	}
}
