//go:build linux

package browser

import (
	"os"
	"path/filepath"
)

// DetectBrowsers scans the filesystem for standard installed browsers.
func DetectBrowsers() ([]BrowserInfo, error) {
	seen := make(map[string]bool)
	var browsers []BrowserInfo

	for _, dir := range linuxSearchDirs {
		for exeName, browserName := range linuxExeNames {
			if seen[browserName] {
				continue
			}
			path := filepath.Join(dir, exeName)
			if _, err := os.Stat(path); err != nil {
				continue
			}
			info := browserInfoForLinux(browserName)
			if info != nil {
				seen[browserName] = true
				browsers = append(browsers, *info)
			}
		}
	}

	return browsers, nil
}

// browserInfoForLinux returns BrowserInfo with the appropriate policy path.
func browserInfoForLinux(name string) *BrowserInfo {
	if name == BrowserFirefox {
		// Use the first existing path, or fallback to standard.
		policyPath := firefoxPolicyPathsLinux[0]
		for _, p := range firefoxPolicyPathsLinux {
			if _, err := os.Stat(filepath.Dir(p)); err == nil {
				policyPath = p
				break
			}
		}
		return &BrowserInfo{
			Name:       BrowserFirefox,
			Family:     Firefox,
			PolicyPath: policyPath,
		}
	}
	policyDir, ok := chromiumVendorLinux[name]
	if !ok {
		return nil
	}
	return &BrowserInfo{
		Name:       name,
		Family:     Chromium,
		PolicyPath: policyDir,
	}
}
