//go:build windows

package browser

import (
	"strings"

	"golang.org/x/sys/windows/registry"
)

const appPathsKey = `SOFTWARE\Microsoft\Windows\CurrentVersion\App Paths`

// DetectBrowsers scans the Windows registry for standard installed browsers.
func DetectBrowsers() ([]BrowserInfo, error) {
	seen := make(map[string]bool)
	var browsers []BrowserInfo

	// Scan App Paths for known browser executables.
	appPaths, err := registry.OpenKey(registry.LOCAL_MACHINE, appPathsKey, registry.ENUMERATE_SUB_KEYS)
	if err == nil {
		defer appPaths.Close()
		subkeys, _ := appPaths.ReadSubKeyNames(-1)
		for _, sub := range subkeys {
			name, ok := windowsAppPathExes[sub]
			if !ok {
				continue
			}
			if seen[name] {
				continue
			}
			info := browserInfoForWindows(name)
			if info != nil {
				seen[name] = true
				browsers = append(browsers, *info)
			}
		}
	}

	// Scan Uninstall keys for additional browsers not in App Paths.
	uninstallKeys := []string{
		`SOFTWARE\Microsoft\Windows\CurrentVersion\Uninstall`,
		`SOFTWARE\WOW6432Node\Microsoft\Windows\CurrentVersion\Uninstall`,
	}
	for _, ukPath := range uninstallKeys {
		uk, err := registry.OpenKey(registry.LOCAL_MACHINE, ukPath, registry.ENUMERATE_SUB_KEYS)
		if err != nil {
			continue
		}
		subkeys, _ := uk.ReadSubKeyNames(-1)
		uk.Close()

		for _, sub := range subkeys {
			sk, err := registry.OpenKey(registry.LOCAL_MACHINE, ukPath+`\`+sub, registry.QUERY_VALUE)
			if err != nil {
				continue
			}
			displayName, _, _ := sk.GetStringValue("DisplayName")
			sk.Close()

			name := matchDisplayName(displayName)
			if name == "" || seen[name] {
				continue
			}
			info := browserInfoForWindows(name)
			if info != nil {
				seen[name] = true
				browsers = append(browsers, *info)
			}
		}
	}

	return browsers, nil
}

// browserInfoForWindows returns BrowserInfo with the appropriate policy path.
func browserInfoForWindows(name string) *BrowserInfo {
	if name == BrowserFirefox {
		return &BrowserInfo{
			Name:       BrowserFirefox,
			Family:     Firefox,
			PolicyPath: firefoxPolicyPathWindows,
		}
	}
	policyPath, ok := chromiumVendorWindows[name]
	if !ok {
		return nil
	}
	return &BrowserInfo{
		Name:       name,
		Family:     Chromium,
		PolicyPath: policyPath,
	}
}

// matchDisplayName matches a registry DisplayName to a known browser name.
func matchDisplayName(displayName string) string {
	lower := strings.ToLower(displayName)
	switch {
	case strings.Contains(lower, "google chrome"):
		return BrowserChrome
	case strings.Contains(lower, "microsoft edge"):
		return BrowserEdge
	case strings.Contains(lower, "brave"):
		return BrowserBrave
	case strings.Contains(lower, "vivaldi"):
		return BrowserVivaldi
	case strings.Contains(lower, "opera"):
		return BrowserOpera
	case strings.Contains(lower, "chromium"):
		return BrowserChromium
	case strings.Contains(lower, "mozilla firefox"):
		return BrowserFirefox
	}
	return ""
}
