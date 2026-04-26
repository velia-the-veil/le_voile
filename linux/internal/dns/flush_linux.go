//go:build linux

package dns

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"syscall"
)

// lookPathFunc allows test injection for exec.LookPath.
var lookPathFunc = exec.LookPath

// readFileFunc allows test injection for os.ReadFile (pidfiles).
var readFileFunc = os.ReadFile

// killFunc allows test injection for syscall.Kill (dnsmasq SIGHUP).
var killFunc = func(pid int, sig syscall.Signal) error {
	return syscall.Kill(pid, sig)
}

// scanProcCommFunc scans /proc/*/comm for a process by name (AC4 fallback).
// Returns PID if found, 0 otherwise. Injectable for tests.
var scanProcCommFunc = func(name string) int {
	entries, err := os.ReadDir("/proc")
	if err != nil {
		return 0
	}
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		pid, err := strconv.Atoi(e.Name())
		if err != nil || pid <= 0 {
			continue
		}
		data, err := os.ReadFile("/proc/" + e.Name() + "/comm")
		if err != nil {
			continue
		}
		if strings.TrimSpace(string(data)) == name {
			return pid
		}
	}
	return 0
}

// Flush purges DNS caches for ALL detected resolvers on Linux (AC6 — cumul).
// Detection order: systemd-resolved, nscd, dnsmasq.
// No resolver detected → debug log + nil (AC5).
// Returns combined errors for logging; callers treat it as non-fatal (AC7, AC8).
func Flush(ctx context.Context) error {
	resolvers := detectResolvers(ctx)
	if len(resolvers) == 0 {
		logFlush("no DNS resolver detected, skipping flush")
		return nil
	}

	var results []flushResult
	for _, r := range resolvers {
		switch r {
		case "systemd-resolved":
			results = append(results, flushResult{"systemd-resolved", flushSystemd(ctx)})
		case "nscd":
			results = append(results, flushResult{"nscd", flushNscd(ctx)})
		case "dnsmasq":
			results = append(results, flushResult{"dnsmasq", flushDnsmasq(ctx)})
		}
	}

	for _, r := range results {
		if r.err != nil {
			logFlush("%s: %v", r.name, r.err)
		} else {
			logFlush("%s: OK", r.name)
		}
	}

	return combineFlushErrors(results)
}

// detectResolvers returns the list of active DNS resolvers on the system.
func detectResolvers(ctx context.Context) []string {
	var resolvers []string

	// systemd-resolved: systemctl is-active --quiet systemd-resolved (exit 0 = active)
	if _, err := flushRunner(ctx, "systemctl", "is-active", "--quiet", "systemd-resolved"); err == nil {
		resolvers = append(resolvers, "systemd-resolved")
	}

	// nscd: binary in PATH OR pidfile readable
	if _, err := lookPathFunc("nscd"); err == nil {
		resolvers = append(resolvers, "nscd")
	} else if _, err := readFileFunc("/var/run/nscd/nscd.pid"); err == nil {
		resolvers = append(resolvers, "nscd")
	}

	// dnsmasq: pidfile with valid PID OR /proc/*/comm scan (AC4)
	if readDnsmasqPID() > 0 || scanProcCommFunc("dnsmasq") > 0 {
		resolvers = append(resolvers, "dnsmasq")
	}

	return resolvers
}

// readDnsmasqPID reads the dnsmasq PID from known pidfile locations.
// Returns 0 if no pidfile found or PID invalid.
func readDnsmasqPID() int {
	for _, path := range []string{"/var/run/dnsmasq/dnsmasq.pid", "/run/dnsmasq/dnsmasq.pid"} {
		data, err := readFileFunc(path)
		if err != nil {
			continue
		}
		pid, err := strconv.Atoi(strings.TrimSpace(string(data)))
		if err != nil || pid <= 0 {
			continue
		}
		return pid
	}
	return 0
}

// flushSystemd flushes systemd-resolved cache via resolvectl.
func flushSystemd(ctx context.Context) error {
	out, err := flushRunner(ctx, "resolvectl", "flush-caches")
	if err != nil {
		return fmt.Errorf("resolvectl flush-caches: %w (output: %s)", err, string(out))
	}
	return nil
}

// flushNscd invalidates the nscd hosts cache.
func flushNscd(ctx context.Context) error {
	out, err := flushRunner(ctx, "nscd", "-i", "hosts")
	if err != nil {
		return fmt.Errorf("nscd -i hosts: %w (output: %s)", err, string(out))
	}
	return nil
}

// flushDnsmasq sends SIGHUP to the dnsmasq process to flush its cache.
// Tries pidfiles first, falls back to /proc scan (AC4).
// Returns nil even on failure (pidfile missing, permission denied, PID gone) — warning only.
func flushDnsmasq(ctx context.Context) error {
	_ = ctx // dnsmasq flush is signal-based, no subprocess needed
	pid := readDnsmasqPID()
	if pid <= 0 {
		pid = scanProcCommFunc("dnsmasq")
	}
	if pid <= 0 {
		logFlush("dnsmasq: no PID found (pidfiles + /proc scan), skipping")
		return nil
	}
	if err := killFunc(pid, syscall.SIGHUP); err != nil {
		logFlush("dnsmasq: SIGHUP pid %d: %v (non-fatal)", pid, err)
		return nil
	}
	return nil
}
