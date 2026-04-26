//go:build e2e && linux

package dns

import (
	"context"
	"testing"
	"time"
)

func TestE2E_Flush_Linux(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	// Real Flush on Linux — should not panic even with no resolver active.
	err := Flush(ctx)
	if err != nil {
		// Non-fatal in production, but log for CI visibility.
		t.Logf("Flush() returned error (may be expected on minimal CI): %v", err)
	}
}
