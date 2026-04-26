//go:build windows

package dns

import (
	"context"
	"fmt"
	"io"
	"os"
	"strings"
)

// flushResult captures the outcome of a single resolver flush attempt.
type flushResult struct {
	name string
	err  error
}

// flushRunner is the command runner used by Flush implementations.
// Delegates to defaultRunner (hidden console on Windows via cmd_windows.go init).
// Package-level var for test injection.
var flushRunner commandRunner = func(ctx context.Context, name string, args ...string) ([]byte, error) {
	return defaultRunner(ctx, name, args...)
}

// flushLogWriter is the destination for dns: flush: log messages.
// Defaults to os.Stderr. Override in tests to suppress or capture output.
var flushLogWriter io.Writer = os.Stderr

// logFlush writes a dns: flush: prefixed message to flushLogWriter.
func logFlush(format string, args ...interface{}) {
	fmt.Fprintf(flushLogWriter, "dns: flush: "+format+"\n", args...)
}

// combineFlushErrors joins multiple flushResult errors into one.
// Returns nil if all results succeeded.
func combineFlushErrors(results []flushResult) error {
	var errs []string
	for _, r := range results {
		if r.err != nil {
			errs = append(errs, fmt.Sprintf("%s: %v", r.name, r.err))
		}
	}
	if len(errs) == 0 {
		return nil
	}
	return fmt.Errorf("dns: flush: %s", strings.Join(errs, "; "))
}
