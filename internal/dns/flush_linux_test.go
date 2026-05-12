//go:build linux

package dns

import (
	"context"
	"errors"
	"io"
	"os"
	"syscall"
	"testing"
)

// suppressFlushLogs silences flush log output for a test and returns a cleanup function.
func suppressFlushLogs() func() {
	orig := flushLogWriter
	flushLogWriter = io.Discard
	return func() { flushLogWriter = orig }
}

// stubFlushRunner returns a runner that records calls and responds based on
// a command→response map. Unmatched commands return an error.
func stubFlushRunner(responses map[string]struct {
	out []byte
	err error
}) (commandRunner, *[]string) {
	var calls []string
	runner := func(_ context.Context, name string, args ...string) ([]byte, error) {
		key := name
		for _, a := range args {
			key += " " + a
		}
		calls = append(calls, key)
		if resp, ok := responses[key]; ok {
			return resp.out, resp.err
		}
		return nil, errors.New("command not stubbed: " + key)
	}
	return runner, &calls
}

func TestFlush_Linux_NoResolver(t *testing.T) {
	defer suppressFlushLogs()()
	origRunner := flushRunner
	origLookPath := lookPathFunc
	origReadFile := readFileFunc
	origScan := scanProcCommFunc
	defer func() {
		flushRunner = origRunner
		lookPathFunc = origLookPath
		readFileFunc = origReadFile
		scanProcCommFunc = origScan
	}()

	flushRunner = func(_ context.Context, _ string, _ ...string) ([]byte, error) {
		return nil, errors.New("inactive")
	}
	lookPathFunc = func(_ string) (string, error) {
		return "", errors.New("not found")
	}
	readFileFunc = func(_ string) ([]byte, error) {
		return nil, os.ErrNotExist
	}
	scanProcCommFunc = func(_ string) int { return 0 }

	err := Flush(context.Background())
	if err != nil {
		t.Errorf("expected nil for no resolvers, got %v", err)
	}
}

func TestFlush_Linux_SystemdOnly(t *testing.T) {
	defer suppressFlushLogs()()
	origRunner := flushRunner
	origLookPath := lookPathFunc
	origReadFile := readFileFunc
	origScan := scanProcCommFunc
	defer func() {
		flushRunner = origRunner
		lookPathFunc = origLookPath
		readFileFunc = origReadFile
		scanProcCommFunc = origScan
	}()

	responses := map[string]struct {
		out []byte
		err error
	}{
		"systemctl is-active --quiet systemd-resolved": {nil, nil},
		"resolvectl flush-caches":                      {[]byte(""), nil},
	}
	runner, _ := stubFlushRunner(responses)
	flushRunner = runner
	lookPathFunc = func(_ string) (string, error) { return "", errors.New("not found") }
	readFileFunc = func(_ string) ([]byte, error) { return nil, os.ErrNotExist }
	scanProcCommFunc = func(_ string) int { return 0 }

	err := Flush(context.Background())
	if err != nil {
		t.Errorf("expected nil, got %v", err)
	}
}

func TestFlush_Linux_MultiResolver(t *testing.T) {
	defer suppressFlushLogs()()
	origRunner := flushRunner
	origLookPath := lookPathFunc
	origReadFile := readFileFunc
	origKill := killFunc
	origScan := scanProcCommFunc
	defer func() {
		flushRunner = origRunner
		lookPathFunc = origLookPath
		readFileFunc = origReadFile
		killFunc = origKill
		scanProcCommFunc = origScan
	}()
	scanProcCommFunc = func(_ string) int { return 0 } // pidfile path used instead

	responses := map[string]struct {
		out []byte
		err error
	}{
		"systemctl is-active --quiet systemd-resolved": {nil, nil},
		"resolvectl flush-caches":                      {nil, nil},
		"nscd -i hosts":                                {nil, nil},
	}
	runner, calls := stubFlushRunner(responses)
	flushRunner = runner
	lookPathFunc = func(name string) (string, error) {
		if name == "nscd" {
			return "/usr/sbin/nscd", nil
		}
		return "", errors.New("not found")
	}
	// dnsmasq pidfile exists
	readFileFunc = func(path string) ([]byte, error) {
		if path == "/var/run/dnsmasq/dnsmasq.pid" {
			return []byte("1234\n"), nil
		}
		return nil, os.ErrNotExist
	}
	var killedPID int
	var killedSig syscall.Signal
	killFunc = func(pid int, sig syscall.Signal) error {
		killedPID = pid
		killedSig = sig
		return nil
	}

	err := Flush(context.Background())
	if err != nil {
		t.Errorf("expected nil with multi-resolver, got %v", err)
	}

	// Verify all three resolvers were flushed (AC6)
	found := map[string]bool{
		"resolvectl": false,
		"nscd":       false,
		"dnsmasq":    false,
	}
	for _, c := range *calls {
		if c == "resolvectl flush-caches" {
			found["resolvectl"] = true
		}
		if c == "nscd -i hosts" {
			found["nscd"] = true
		}
	}
	if killedPID == 1234 && killedSig == syscall.SIGHUP {
		found["dnsmasq"] = true
	}

	for name, ok := range found {
		if !ok {
			t.Errorf("expected %s flush to be called", name)
		}
	}
}

func TestFlush_Linux_DnsmasqTOCTOU(t *testing.T) {
	defer suppressFlushLogs()()
	origRunner := flushRunner
	origLookPath := lookPathFunc
	origReadFile := readFileFunc
	origKill := killFunc
	origScan := scanProcCommFunc
	defer func() {
		flushRunner = origRunner
		lookPathFunc = origLookPath
		readFileFunc = origReadFile
		killFunc = origKill
		scanProcCommFunc = origScan
	}()

	// Scenario: dnsmasq detected via pidfile during detectResolvers,
	// but pidfile vanishes before flushDnsmasq re-reads it.
	// scanProcCommFunc also returns 0 (process gone). Flush should return nil.
	readCount := 0
	flushRunner = func(_ context.Context, _ string, _ ...string) ([]byte, error) {
		return nil, errors.New("inactive")
	}
	lookPathFunc = func(_ string) (string, error) { return "", errors.New("not found") }
	readFileFunc = func(path string) ([]byte, error) {
		if path == "/var/run/dnsmasq/dnsmasq.pid" {
			readCount++
			if readCount <= 1 {
				return []byte("1234\n"), nil // first read: pidfile exists (detection)
			}
			return nil, os.ErrNotExist // subsequent reads: pidfile gone (flush)
		}
		return nil, os.ErrNotExist
	}
	scanProcCommFunc = func(_ string) int { return 0 } // process also gone
	killFunc = func(_ int, _ syscall.Signal) error {
		t.Error("kill should not be called when PID is gone")
		return nil
	}

	err := Flush(context.Background())
	if err != nil {
		t.Errorf("expected nil on TOCTOU (pidfile vanished), got %v", err)
	}
}

func TestFlush_Linux_DnsmasqKillFails(t *testing.T) {
	defer suppressFlushLogs()()
	origRunner := flushRunner
	origLookPath := lookPathFunc
	origReadFile := readFileFunc
	origKill := killFunc
	origScan := scanProcCommFunc
	defer func() {
		flushRunner = origRunner
		lookPathFunc = origLookPath
		readFileFunc = origReadFile
		killFunc = origKill
		scanProcCommFunc = origScan
	}()

	flushRunner = func(_ context.Context, _ string, _ ...string) ([]byte, error) {
		return nil, errors.New("inactive")
	}
	lookPathFunc = func(_ string) (string, error) { return "", errors.New("not found") }
	readFileFunc = func(path string) ([]byte, error) {
		if path == "/var/run/dnsmasq/dnsmasq.pid" {
			return []byte("999\n"), nil
		}
		return nil, os.ErrNotExist
	}
	scanProcCommFunc = func(_ string) int { return 0 }
	killFunc = func(_ int, _ syscall.Signal) error {
		return errors.New("operation not permitted")
	}

	// flushDnsmasq returns nil even on kill failure (warning only)
	err := Flush(context.Background())
	if err != nil {
		t.Errorf("expected nil even when kill fails (warning only), got %v", err)
	}
}

func TestFlush_Linux_ContextCancelled(t *testing.T) {
	defer suppressFlushLogs()()
	origRunner := flushRunner
	origLookPath := lookPathFunc
	origReadFile := readFileFunc
	origScan := scanProcCommFunc
	defer func() {
		flushRunner = origRunner
		lookPathFunc = origLookPath
		readFileFunc = origReadFile
		scanProcCommFunc = origScan
	}()

	// systemd-resolved active, but resolvectl will see cancelled context
	flushRunner = func(ctx context.Context, name string, _ ...string) ([]byte, error) {
		if name == "systemctl" {
			return nil, nil // detected despite cancelled ctx (mock)
		}
		// resolvectl call — propagate context error
		return nil, ctx.Err()
	}
	lookPathFunc = func(_ string) (string, error) { return "", errors.New("not found") }
	readFileFunc = func(_ string) ([]byte, error) { return nil, os.ErrNotExist }
	scanProcCommFunc = func(_ string) int { return 0 }

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	err := Flush(ctx)
	// systemd-resolved detected → resolvectl called → returns context.Canceled → error propagated
	if err == nil {
		t.Error("expected non-nil error when context is cancelled during flush")
	}
}

func TestDetectResolvers_NscdViaPidfile(t *testing.T) {
	defer suppressFlushLogs()()
	origRunner := flushRunner
	origLookPath := lookPathFunc
	origReadFile := readFileFunc
	origScan := scanProcCommFunc
	defer func() {
		flushRunner = origRunner
		lookPathFunc = origLookPath
		readFileFunc = origReadFile
		scanProcCommFunc = origScan
	}()

	flushRunner = func(_ context.Context, _ string, _ ...string) ([]byte, error) {
		return nil, errors.New("inactive")
	}
	lookPathFunc = func(_ string) (string, error) { return "", errors.New("not found") }
	readFileFunc = func(path string) ([]byte, error) {
		if path == "/var/run/nscd/nscd.pid" {
			return []byte("42\n"), nil
		}
		return nil, os.ErrNotExist
	}
	scanProcCommFunc = func(_ string) int { return 0 }

	resolvers := detectResolvers(context.Background())
	if len(resolvers) != 1 || resolvers[0] != "nscd" {
		t.Errorf("expected [nscd], got %v", resolvers)
	}
}

func TestDetectResolvers_DnsmasqViaProcScan(t *testing.T) {
	defer suppressFlushLogs()()
	origRunner := flushRunner
	origLookPath := lookPathFunc
	origReadFile := readFileFunc
	origScan := scanProcCommFunc
	defer func() {
		flushRunner = origRunner
		lookPathFunc = origLookPath
		readFileFunc = origReadFile
		scanProcCommFunc = origScan
	}()

	flushRunner = func(_ context.Context, _ string, _ ...string) ([]byte, error) {
		return nil, errors.New("inactive")
	}
	lookPathFunc = func(_ string) (string, error) { return "", errors.New("not found") }
	readFileFunc = func(_ string) ([]byte, error) { return nil, os.ErrNotExist } // no pidfiles
	scanProcCommFunc = func(name string) int {
		if name == "dnsmasq" {
			return 5678
		}
		return 0
	}

	resolvers := detectResolvers(context.Background())
	if len(resolvers) != 1 || resolvers[0] != "dnsmasq" {
		t.Errorf("expected [dnsmasq] via proc scan, got %v", resolvers)
	}
}

func TestFlushDnsmasq_ProcScanFallback(t *testing.T) {
	defer suppressFlushLogs()()
	origReadFile := readFileFunc
	origScan := scanProcCommFunc
	origKill := killFunc
	defer func() {
		readFileFunc = origReadFile
		scanProcCommFunc = origScan
		killFunc = origKill
	}()

	readFileFunc = func(_ string) ([]byte, error) { return nil, os.ErrNotExist } // no pidfiles
	scanProcCommFunc = func(name string) int {
		if name == "dnsmasq" {
			return 4321
		}
		return 0
	}
	var killedPID int
	killFunc = func(pid int, sig syscall.Signal) error {
		killedPID = pid
		if sig != syscall.SIGHUP {
			t.Errorf("expected SIGHUP, got %v", sig)
		}
		return nil
	}

	err := flushDnsmasq(context.Background())
	if err != nil {
		t.Errorf("expected nil, got %v", err)
	}
	if killedPID != 4321 {
		t.Errorf("expected kill PID 4321 (from proc scan), got %d", killedPID)
	}
}
