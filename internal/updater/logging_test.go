package updater

import (
	"bytes"
	"context"
	"regexp"
	"strings"
	"testing"
)

// Story 8.1 AC11 — Updater MUST emit structured `service: updater: ...` lines
// to a caller-provided writer (typically wired to syslog/Event Log via
// kardianos/service). Lines must NEVER contain PII: no IPv4/IPv6 in dotted
// form, no `/home/<user>` or `/Users/<user>` paths, no `%USERPROFILE%`,
// nothing from the downloaded payload.

// Sensitive patterns we forbid in log output. Conservative: any IP-like
// sequence triggers a fail (URLs to api.github.com use hostnames, never IPs,
// so a hit means we accidentally logged client/relay state).
var sensitivePatterns = []*regexp.Regexp{
	regexp.MustCompile(`\b\d{1,3}\.\d{1,3}\.\d{1,3}\.\d{1,3}\b`), // IPv4
	regexp.MustCompile(`(?i)/home/[^/\s]+`),                       // Linux home
	regexp.MustCompile(`(?i)/users/[^/\s]+`),                      // macOS home (defensive — no macOS support yet)
	regexp.MustCompile(`(?i)c:\\users\\[^\\\s]+`),                 // Windows profile
	regexp.MustCompile(`(?i)%userprofile%`),                       // Windows env var
	regexp.MustCompile(`[0-9a-f]{2}(:[0-9a-f]{2}){5}`),           // MAC address
}

func TestUpdater_Logging_EmitsCycleEvents(t *testing.T) {
	env := setupTestUpdaterEnv(t, "binary content v2", "2.0.0")
	defer env.server.Close()

	var buf bytes.Buffer
	upd := env.newUpdaterWithLogger(t, "1.0.0", &buf)

	if _, err := upd.CheckAndDownload(context.Background()); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	out := buf.String()
	if out == "" {
		t.Fatal("expected logger output, got empty buffer")
	}
	// Every line must start with the canonical prefix so syslog tooling
	// (logger / journalctl --identifier=...) can filter cleanly.
	for _, line := range strings.Split(strings.TrimSpace(out), "\n") {
		if !strings.HasPrefix(line, "service: updater: ") {
			t.Errorf("log line lacks canonical prefix: %q", line)
		}
	}
	// At minimum we expect cycle-start and update-ready markers.
	for _, needle := range []string{"cycle start", "version=2.0.0"} {
		if !strings.Contains(out, needle) {
			t.Errorf("missing expected log marker %q in:\n%s", needle, out)
		}
	}
}

func TestUpdater_Logging_NoSensitiveData(t *testing.T) {
	env := setupTestUpdaterEnv(t, "binary content v2", "2.0.0")
	defer env.server.Close()

	var buf bytes.Buffer
	upd := env.newUpdaterWithLogger(t, "1.0.0", &buf)

	if _, err := upd.CheckAndDownload(context.Background()); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	out := buf.String()
	for _, re := range sensitivePatterns {
		if loc := re.FindStringIndex(out); loc != nil {
			t.Errorf("forbidden pattern %s found in updater logs near offset %d:\n%s",
				re.String(), loc[0], out[max(0, loc[0]-40):min(len(out), loc[1]+40)])
		}
	}
}

func TestUpdater_Logging_NilLoggerIsSafe(t *testing.T) {
	env := setupTestUpdaterEnv(t, "binary content v2", "2.0.0")
	defer env.server.Close()

	// No Logger in the config → must not panic.
	upd := env.newUpdater(t, "1.0.0")
	if _, err := upd.CheckAndDownload(context.Background()); err != nil {
		t.Fatalf("unexpected error with nil logger: %v", err)
	}
}

// Code review M1 — the original anti-PII test only exercised the happy path,
// so an `err=%v` formatter that leaks `/home/<user>/...` (e.g. on a stale
// staging dir, permission denied) would have slipped through.
// This test injects two error-path scenarios and asserts:
//  1. the offending log line is emitted (so we know we're really exercising
//     the failure branch),
//  2. the home-path segment is collapsed to "$HOME" by sanitizePII.
func TestUpdater_Logging_ErrorPathsScrubHome(t *testing.T) {
	cases := []struct {
		name      string
		raw       string
		mustHave  string // post-scrub substring that proves sanitization ran
		mustMatch *regexp.Regexp
	}{
		{
			name:      "linux home segment",
			raw:       "open /home/akerimus/.config/levoile/staging/failed_version.txt: permission denied",
			mustHave:  "$HOME/.config/levoile/staging/failed_version.txt",
			mustMatch: regexp.MustCompile(`permission denied`),
		},
		{
			name:      "windows profile segment",
			raw:       `open C:\Users\Akerimus\AppData\Local\LeVoile\update\binary.tmp: access denied`,
			mustHave:  `$HOME\AppData\Local\LeVoile\update\binary.tmp`,
			mustMatch: regexp.MustCompile(`access denied`),
		},
		{
			name:      "root home",
			raw:       "open /root/.cache/levoile/x: no such file",
			mustHave:  "$HOME/.cache/levoile/x",
			mustMatch: regexp.MustCompile(`no such file`),
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			env := setupTestUpdaterEnv(t, "binary", "2.0.0")
			defer env.server.Close()

			var buf bytes.Buffer
			upd := env.newUpdaterWithLogger(t, "1.0.0", &buf)

			// Drive an error through logf directly — equivalent to what
			// CheckAndDownload would log on a real `os.Open` failure. This
			// avoids racing the test against the real download/verify
			// paths that already pass.
			upd.logf("download failed err=%v", errorString(tc.raw))

			out := buf.String()
			if !strings.Contains(out, tc.mustHave) {
				t.Errorf("scrubbed output missing %q in:\n%s", tc.mustHave, out)
			}
			if !tc.mustMatch.MatchString(out) {
				t.Errorf("scrubbed output missing %s; lost the underlying error", tc.mustMatch)
			}
			// Belt-and-braces: ensure no raw home segment survived.
			for _, re := range sensitivePatterns {
				if loc := re.FindStringIndex(out); loc != nil {
					t.Errorf("forbidden pattern %s leaked through sanitizePII at offset %d:\n%s",
						re.String(), loc[0], out)
				}
			}
		})
	}
}

// errorString is a trivial error-typed wrapper so we exercise the
// `case error` branch of sanitizePII (the `case string` branch is reached
// when callers pass plain strings).
type errorString string

func (e errorString) Error() string { return string(e) }

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}
func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
