//go:build e2e && windows

package dns

import (
	"context"
	"testing"
	"time"
)

func TestE2E_Flush_Windows(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	// Real ipconfig /flushdns — works without elevation on Windows 10/11.
	err := Flush(ctx)
	if err != nil {
		t.Errorf("Flush() on Windows returned error: %v", err)
	}
}
