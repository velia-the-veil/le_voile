//go:build windows

package elevation

import (
	"runtime"
	"strings"
	"testing"
)

func TestIsElevated_ReturnsBoolean(t *testing.T) {
	// IsElevated should return a boolean without panicking.
	got := IsElevated()
	t.Logf("IsElevated() = %v", got)
}

func TestRelaunchElevated_Unix(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("RelaunchElevated on Windows requires UAC interaction")
	}
	err := RelaunchElevated()
	if err == nil {
		t.Error("expected error on Unix, got nil")
	}
	if !strings.Contains(err.Error(), "sudo") {
		t.Errorf("expected error to mention sudo, got %q", err.Error())
	}
}
