//go:build darwin

package browser

// DetectBrowsers is a no-op stub on macOS.
func DetectBrowsers() ([]BrowserInfo, error) {
	return nil, nil
}
