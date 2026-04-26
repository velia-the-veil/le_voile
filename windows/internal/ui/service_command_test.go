//go:build windows

package ui

import (
	"runtime"
	"strings"
	"testing"
)

// TestServiceStartHintForOS_Windows locks the exact copy used by the frontend
// fallback screen (Story 5.6 AC2). If any of these strings drift, the in-app
// instructions break for users whose service is down — which is precisely the
// moment where clarity matters most.
func TestServiceStartHintForOS_Windows(t *testing.T) {
	h := ServiceStartHintForOS("windows")
	if h.OS != "windows" {
		t.Errorf("OS = %q, want windows", h.OS)
	}
	if h.Command != "sc start LeVoile" {
		t.Errorf("Command = %q, want 'sc start LeVoile'", h.Command)
	}
	if !strings.Contains(h.HumanMessage, "Services.msc") {
		t.Errorf("HumanMessage should mention Services.msc, got %q", h.HumanMessage)
	}
	if !strings.Contains(h.HumanMessage, "administrateur") {
		t.Errorf("HumanMessage should mention admin elevation, got %q", h.HumanMessage)
	}
}

func TestServiceStartHintForOS_Linux(t *testing.T) {
	h := ServiceStartHintForOS("linux")
	if h.OS != "linux" {
		t.Errorf("OS = %q, want linux", h.OS)
	}
	if h.Command != "sudo systemctl start levoile.service" {
		t.Errorf("Command = %q, want 'sudo systemctl start levoile.service'", h.Command)
	}
	if !strings.Contains(h.HumanMessage, "terminal") {
		t.Errorf("HumanMessage should mention terminal, got %q", h.HumanMessage)
	}
}

// TestServiceStartHintForOS_Unknown guards the fallback branch: unknown GOOS
// values (darwin, freebsd, js) must still yield a renderable hint — never
// empty strings that would produce a blank fallback screen.
func TestServiceStartHintForOS_Unknown(t *testing.T) {
	h := ServiceStartHintForOS("darwin")
	if h.OS != "darwin" {
		t.Errorf("OS = %q, want darwin (pass-through)", h.OS)
	}
	if h.HumanMessage == "" {
		t.Error("HumanMessage must not be empty for unknown OS — frontend needs copy to render")
	}
}

// TestCurrentServiceStartHint_UsesOverride verifies the osForHint indirection
// actually reroutes the OS source — this is the mechanism tests rely on.
func TestCurrentServiceStartHint_UsesOverride(t *testing.T) {
	prev := osForHint
	t.Cleanup(func() { osForHint = prev })

	osForHint = func() string { return "linux" }
	if got := CurrentServiceStartHint(); got.OS != "linux" {
		t.Errorf("CurrentServiceStartHint with linux override: OS = %q, want linux", got.OS)
	}

	osForHint = func() string { return "windows" }
	if got := CurrentServiceStartHint(); got.OS != "windows" {
		t.Errorf("CurrentServiceStartHint with windows override: OS = %q, want windows", got.OS)
	}
}

// TestCurrentServiceStartHint_Default confirms that without any override the
// hint matches the build's runtime.GOOS — guards against accidental hardcoding.
func TestCurrentServiceStartHint_Default(t *testing.T) {
	h := CurrentServiceStartHint()
	if h.OS != runtime.GOOS {
		t.Errorf("default CurrentServiceStartHint: OS = %q, want runtime.GOOS %q", h.OS, runtime.GOOS)
	}
}
