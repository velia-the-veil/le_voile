//go:build e2e && windows

package browser

import (
	"context"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"

	"golang.org/x/sys/windows/registry"
)

func isElevated() bool {
	err := exec.Command("net", "session").Run()
	return err == nil
}

// TestE2E_ChromiumPoliciesApplied applies browser policies and verifies that
// the Chromium ExtensionInstallForcelist registry key contains the Le Voile
// extension ID.
func TestE2E_ChromiumPoliciesApplied(t *testing.T) {
	if os.Getenv("E2E") != "1" {
		t.Skip("set E2E=1 to run")
	}
	if !isElevated() {
		t.Skip("requires admin")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	mgr := NewPolicyManager()
	t.Cleanup(func() {
		restoreCtx, c := context.WithTimeout(context.Background(), 10*time.Second)
		defer c()
		mgr.RestorePolicies(restoreCtx)
	})

	result, err := mgr.ApplyPolicies(ctx)
	if err != nil {
		t.Fatalf("ApplyPolicies: %v", err)
	}

	// Check if any Chromium browser was detected and had policies applied.
	chromiumApplied := false
	for _, name := range result.Applied {
		if name == BrowserChrome || name == BrowserEdge || name == BrowserBrave ||
			name == BrowserVivaldi || name == BrowserOpera || name == BrowserChromium {
			chromiumApplied = true
			break
		}
	}

	if !chromiumApplied {
		t.Skip("no Chromium-based browser detected on this system")
	}

	// Verify the ExtensionInstallForcelist registry key contains the extension ID.
	vendors := map[string]string{
		BrowserChrome:   `SOFTWARE\Policies\Google\Chrome`,
		BrowserEdge:     `SOFTWARE\Policies\Microsoft\Edge`,
		BrowserBrave:    `SOFTWARE\Policies\BraveSoftware\Brave`,
		BrowserVivaldi:  `SOFTWARE\Policies\Vivaldi`,
		BrowserOpera:    `SOFTWARE\Policies\Opera Software\Opera`,
		BrowserChromium: `SOFTWARE\Policies\Chromium`,
	}

	foundExtension := false
	for _, name := range result.Applied {
		path, ok := vendors[name]
		if !ok {
			continue
		}
		forcelistKey := path + `\ExtensionInstallForcelist`
		key, err := registry.OpenKey(registry.LOCAL_MACHINE, forcelistKey, registry.READ)
		if err != nil {
			continue
		}
		// Read all values in the forcelist key and check for extension ID.
		names, err := key.ReadValueNames(-1)
		key.Close()
		if err != nil {
			continue
		}
		for _, vn := range names {
			k2, _ := registry.OpenKey(registry.LOCAL_MACHINE, forcelistKey, registry.READ)
			val, _, err := k2.GetStringValue(vn)
			k2.Close()
			if err != nil {
				continue
			}
			if strings.Contains(val, ExtensionID) {
				foundExtension = true
				t.Logf("%s ExtensionInstallForcelist contains extension ID %q (value: %q)", name, ExtensionID, val)
			}
		}
	}

	if !foundExtension {
		t.Error("ExtensionInstallForcelist does not contain Le Voile extension ID in any Chromium browser registry key")
	}

	t.Logf("Chromium policies applied successfully to: %v", result.Applied)
}

// TestE2E_WebRTCPoliciesApplied_Chromium verifies that the WebRTC IP handling
// policy is set to "disable_non_proxied_udp" in the registry after ApplyPolicies.
func TestE2E_WebRTCPoliciesApplied_Chromium(t *testing.T) {
	if os.Getenv("E2E") != "1" {
		t.Skip("set E2E=1 to run")
	}
	if !isElevated() {
		t.Skip("requires admin")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	mgr := NewPolicyManager()
	t.Cleanup(func() {
		restoreCtx, c := context.WithTimeout(context.Background(), 10*time.Second)
		defer c()
		mgr.RestorePolicies(restoreCtx)
	})

	result, err := mgr.ApplyPolicies(ctx)
	if err != nil {
		t.Fatalf("ApplyPolicies: %v", err)
	}

	// Check for any Chromium browser.
	chromiumApplied := false
	for _, name := range result.Applied {
		if name == BrowserChrome || name == BrowserEdge || name == BrowserBrave ||
			name == BrowserVivaldi || name == BrowserOpera || name == BrowserChromium {
			chromiumApplied = true
			break
		}
	}
	if !chromiumApplied {
		t.Skip("no Chromium browser detected")
	}

	// Check WebRtcIPHandlingPolicy in one of the applied Chromium browsers.
	vendors := map[string]string{
		BrowserChrome:   `SOFTWARE\Policies\Google\Chrome`,
		BrowserEdge:     `SOFTWARE\Policies\Microsoft\Edge`,
		BrowserBrave:    `SOFTWARE\Policies\BraveSoftware\Brave`,
		BrowserVivaldi:  `SOFTWARE\Policies\Vivaldi`,
		BrowserOpera:    `SOFTWARE\Policies\Opera Software\Opera`,
		BrowserChromium: `SOFTWARE\Policies\Chromium`,
	}

	found := false
	for _, name := range result.Applied {
		path, ok := vendors[name]
		if !ok {
			continue
		}
		key, err := registry.OpenKey(registry.LOCAL_MACHINE, path, registry.READ)
		if err != nil {
			continue
		}
		val, _, err := key.GetStringValue("WebRtcIPHandlingPolicy")
		key.Close()
		if err != nil {
			continue
		}
		if val != "disable_non_proxied_udp" {
			t.Errorf("%s WebRtcIPHandlingPolicy = %q, want %q", name, val, "disable_non_proxied_udp")
		} else {
			found = true
			t.Logf("%s WebRtcIPHandlingPolicy = %q (correct)", name, val)
		}
	}

	if !found {
		t.Error("WebRtcIPHandlingPolicy not found in any Chromium browser registry key")
	}
}
