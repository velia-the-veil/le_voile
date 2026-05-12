//go:build windows

package dns

import (
	"context"
	"fmt"
)

// Flush purges the Windows DNS resolver cache via ipconfig /flushdns.
// Uses hiddenRunner (HideWindow: true) to prevent console flashes.
// Returns an error for logging; callers treat it as non-fatal (AC1, AC7, AC8).
func Flush(ctx context.Context) error {
	out, err := flushRunner(ctx, "ipconfig", "/flushdns")
	if err != nil {
		logFlush("ipconfig /flushdns: %v (output: %s)", err, string(out))
		return fmt.Errorf("dns: flush: ipconfig: %w", err)
	}
	logFlush("ipconfig /flushdns: OK")
	return nil
}
