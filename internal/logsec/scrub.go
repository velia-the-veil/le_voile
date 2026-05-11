// Package logsec provides log sanitisation primitives shared between the
// Windows and Linux service binaries. Audit fix F-18 (2026-05-04): the
// updater package already redacted user-home paths in its own log
// pipeline (internal/updater/sanitizePII), but the service's stderr
// stream still wrote raw os.PathError values containing /home/<user>,
// C:\Users\<user>, %APPDATA%\<user>... to the OS log (Event Log on
// Windows, journald on Linux). This package extracts the scrubbing
// helpers into a shared dependency so both the updater and the service
// stderr writer apply the exact same policy.
package logsec

import (
	"io"
	"os"
	"regexp"
	"strings"
	"sync"
)

// homeUserRE matches /home/<user>, /Users/<user>, and Windows
// C:\Users\<user> (case-insensitive on the literal "Users" segment so
// drive letters and other roots match the same behaviour). The
// captured user segment is stripped together with the parent so the
// replacement is collapsed to "$HOME".
//
// Fix 2026-05-11 : la partie Windows avait `\\\\` (4 backslashes raw =
// regex "match 2 backslashes") au lieu de `\\` (2 raw = regex "match 1
// backslash"). Le test TestScrubLine_WindowsProfile passait en local
// Windows uniquement parce que la 3e étape (strings.ReplaceAll avec
// os.UserHomeDir()) faisait le remplacement par hasard ; sur runner CI
// Ubuntu, UserHomeDir()=/home/runner qui ne matche pas C:\Users\<user>
// → regex Windows seule en jeu → fail.
var homeUserRE = regexp.MustCompile(`(?i)(/home/|/Users/|[A-Z]:\\Users\\)[^/\\:|"'\s]+`)

// rootHomeRE catches the /root path used by services that drop into a
// privileged shell. /root has no trailing username segment because it
// IS the home directory.
var rootHomeRE = regexp.MustCompile(`/root\b`)

// homeDirCache memoises os.UserHomeDir() so ScrubLine doesn't reach for
// the env on every write. The home directory is captured at process
// start; if the user re-logs and the binary persists, the staleness is
// cosmetic only (the regex catches the standard layouts).
var (
	homeDirOnce sync.Once
	homeDir     string
)

func resolvedHomeDir() string {
	homeDirOnce.Do(func() {
		if h, err := os.UserHomeDir(); err == nil {
			homeDir = h
		}
	})
	return homeDir
}

// ScrubLine collapses every recognised user-home prefix in s to the
// literal token "$HOME". The function is idempotent and never returns
// a longer string than its input. Empty input returns empty output.
//
// The literal-string replacement is required because the regex pkg
// otherwise interprets the dollar sign as a back-reference token —
// which would emit "" instead of "$HOME" on the right-hand side.
func ScrubLine(s string) string {
	if s == "" {
		return s
	}
	out := homeUserRE.ReplaceAllLiteralString(s, "$HOME")
	out = rootHomeRE.ReplaceAllLiteralString(out, "$HOME")
	if h := resolvedHomeDir(); h != "" && strings.Contains(out, h) {
		out = strings.ReplaceAll(out, h, "$HOME")
	}
	return out
}

// scrubWriter wraps an io.Writer and applies ScrubLine to every Write
// call. It does NOT buffer across writes: a single Write that splits a
// path mid-token will leak the path. In practice the service's stderr
// is always written via fmt.Fprintf which formats one log entry per
// call, so the boundary lines up with the natural scrub unit.
type scrubWriter struct {
	mu     sync.Mutex
	target io.Writer
}

// NewWriter wraps target with a PII-scrubbing writer. Safe for
// concurrent use (an internal mutex serialises Write calls so the
// scrubbed output stays line-coherent). Pass through is exact: an
// empty Write is forwarded unchanged.
func NewWriter(target io.Writer) io.Writer {
	if target == nil {
		return target
	}
	return &scrubWriter{target: target}
}

func (w *scrubWriter) Write(p []byte) (int, error) {
	if len(p) == 0 {
		return w.target.Write(p)
	}
	scrubbed := ScrubLine(string(p))
	w.mu.Lock()
	defer w.mu.Unlock()
	if scrubbed == string(p) {
		return w.target.Write(p)
	}
	if _, err := w.target.Write([]byte(scrubbed)); err != nil {
		return 0, err
	}
	// Report the original length so callers using fmt.Fprintf etc.
	// don't see a length mismatch and report a short-write error.
	return len(p), nil
}
