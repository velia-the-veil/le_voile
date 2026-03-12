package updater

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"runtime"
	"testing"
)

func TestCompareVersions(t *testing.T) {
	tests := []struct {
		name    string
		current string
		latest  string
		want    int
	}{
		{"equal", "1.2.3", "1.2.3", 0},
		{"current older", "1.2.3", "1.2.4", -1},
		{"current newer", "1.2.4", "1.2.3", 1},
		{"major diff", "1.0.0", "2.0.0", -1},
		{"minor diff", "1.1.0", "1.2.0", -1},
		{"v prefix current", "v1.2.3", "1.2.3", 0},
		{"v prefix latest", "1.2.3", "v1.2.3", 0},
		{"v prefix both", "v1.2.3", "v1.2.4", -1},
		{"invalid current", "dev", "1.0.0", -1},
		{"invalid latest", "1.0.0", "invalid", 1},
		{"both invalid", "dev", "invalid", 0},
		{"partial version", "1.2", "1.2.1", -1},
		{"single number", "1", "2", -1},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := compareVersions(tt.current, tt.latest)
			if got != tt.want {
				t.Errorf("compareVersions(%q, %q) = %d, want %d", tt.current, tt.latest, got, tt.want)
			}
		})
	}
}

func TestParseGitHubRelease_Valid(t *testing.T) {
	expectedBinary := fmt.Sprintf("le_voile_%s_%s", runtime.GOOS, runtime.GOARCH)
	if runtime.GOOS == "windows" {
		expectedBinary += ".exe"
	}

	body := fmt.Sprintf(`{
		"tag_name": "v1.2.0",
		"name": "Le Voile v1.2.0",
		"body": "Release notes here",
		"published_at": "2026-03-10T12:00:00Z",
		"assets": [
			{"name": %q, "browser_download_url": "https://github.com/velia-the-veil/le_voile/releases/download/v1.2.0/%s"},
			{"name": "checksums.txt", "browser_download_url": "https://github.com/velia-the-veil/le_voile/releases/download/v1.2.0/checksums.txt"},
			{"name": "checksums.txt.sig", "browser_download_url": "https://github.com/velia-the-veil/le_voile/releases/download/v1.2.0/checksums.txt.sig"}
		]
	}`, expectedBinary, expectedBinary)

	info, err := parseGitHubRelease([]byte(body))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if info.Version != "1.2.0" {
		t.Errorf("Version = %q, want %q", info.Version, "1.2.0")
	}
	if info.DownloadURL == "" {
		t.Error("DownloadURL is empty")
	}
	if info.ChecksumURL == "" {
		t.Error("ChecksumURL is empty")
	}
	if info.SignatureURL == "" {
		t.Error("SignatureURL is empty")
	}
	if info.ReleaseNotes != "Release notes here" {
		t.Errorf("ReleaseNotes = %q, want %q", info.ReleaseNotes, "Release notes here")
	}
}

func TestParseGitHubRelease_NoAssets(t *testing.T) {
	body := `{"tag_name": "v1.0.0", "assets": []}`
	_, err := parseGitHubRelease([]byte(body))
	if err == nil {
		t.Error("expected error for release with no assets")
	}
}

func TestParseGitHubRelease_MissingBinaryAsset(t *testing.T) {
	body := `{
		"tag_name": "v1.0.0",
		"assets": [
			{"name": "checksums.txt", "browser_download_url": "https://example.com/checksums.txt"}
		]
	}`
	_, err := parseGitHubRelease([]byte(body))
	if err == nil {
		t.Error("expected error for missing binary asset")
	}
}

func TestParseGitHubRelease_MissingChecksums(t *testing.T) {
	expectedBinary := fmt.Sprintf("le_voile_%s_%s", runtime.GOOS, runtime.GOARCH)
	if runtime.GOOS == "windows" {
		expectedBinary += ".exe"
	}

	body := fmt.Sprintf(`{
		"tag_name": "v1.0.0",
		"assets": [
			{"name": %q, "browser_download_url": "https://github.com/velia-the-veil/le_voile/releases/download/v1.0.0/%s"}
		]
	}`, expectedBinary, expectedBinary)
	_, err := parseGitHubRelease([]byte(body))
	if err == nil {
		t.Error("expected error for missing checksums.txt asset")
	}
}

func TestParseGitHubRelease_MissingSignature(t *testing.T) {
	expectedBinary := fmt.Sprintf("le_voile_%s_%s", runtime.GOOS, runtime.GOARCH)
	if runtime.GOOS == "windows" {
		expectedBinary += ".exe"
	}

	body := fmt.Sprintf(`{
		"tag_name": "v1.0.0",
		"assets": [
			{"name": %q, "browser_download_url": "https://github.com/velia-the-veil/le_voile/releases/download/v1.0.0/%s"},
			{"name": "checksums.txt", "browser_download_url": "https://github.com/velia-the-veil/le_voile/releases/download/v1.0.0/checksums.txt"}
		]
	}`, expectedBinary, expectedBinary)
	_, err := parseGitHubRelease([]byte(body))
	if err == nil {
		t.Error("expected error for missing checksums.txt.sig asset")
	}
}

func TestChecker_CheckLatest_NewVersion(t *testing.T) {
	expectedBinary := fmt.Sprintf("le_voile_%s_%s", runtime.GOOS, runtime.GOARCH)
	if runtime.GOOS == "windows" {
		expectedBinary += ".exe"
	}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprintf(w, `{
			"tag_name": "v99.0.0",
			"body": "new release",
			"published_at": "2026-03-10T12:00:00Z",
			"assets": [
				{"name": %q, "browser_download_url": "https://github.com/velia-the-veil/le_voile/releases/download/v99.0.0/%s"},
				{"name": "checksums.txt", "browser_download_url": "https://github.com/velia-the-veil/le_voile/releases/download/v99.0.0/checksums.txt"},
				{"name": "checksums.txt.sig", "browser_download_url": "https://github.com/velia-the-veil/le_voile/releases/download/v99.0.0/checksums.txt.sig"}
			]
		}`, expectedBinary, expectedBinary)
	}))
	defer srv.Close()

	c := &Checker{
		httpClient:     srv.Client(),
		owner:          "test",
		repo:           "test",
		currentVersion: "1.0.0",
	}

	// Override URL by using a custom transport
	origURL := srv.URL
	c.httpClient.Transport = roundTripFunc(func(req *http.Request) (*http.Response, error) {
		req.URL.Scheme = "http"
		req.URL.Host = origURL[len("http://"):]
		return http.DefaultTransport.RoundTrip(req)
	})

	info, err := c.CheckLatest(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if info == nil {
		t.Fatal("expected ReleaseInfo, got nil")
	}
	if info.Version != "99.0.0" {
		t.Errorf("Version = %q, want %q", info.Version, "99.0.0")
	}
}

func TestChecker_CheckLatest_UpToDate(t *testing.T) {
	expectedBinary := fmt.Sprintf("le_voile_%s_%s", runtime.GOOS, runtime.GOARCH)
	if runtime.GOOS == "windows" {
		expectedBinary += ".exe"
	}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprintf(w, `{
			"tag_name": "v1.0.0",
			"published_at": "2026-03-10T12:00:00Z",
			"assets": [
				{"name": %q, "browser_download_url": "https://example.com/binary"},
				{"name": "checksums.txt", "browser_download_url": "https://example.com/checksums.txt"},
				{"name": "checksums.txt.sig", "browser_download_url": "https://example.com/checksums.txt.sig"}
			]
		}`, expectedBinary)
	}))
	defer srv.Close()

	c := &Checker{
		httpClient:     srv.Client(),
		owner:          "test",
		repo:           "test",
		currentVersion: "1.0.0",
	}
	c.httpClient.Transport = roundTripFunc(func(req *http.Request) (*http.Response, error) {
		req.URL.Scheme = "http"
		req.URL.Host = srv.URL[len("http://"):]
		return http.DefaultTransport.RoundTrip(req)
	})

	info, err := c.CheckLatest(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if info != nil {
		t.Errorf("expected nil (up to date), got %+v", info)
	}
}

func TestChecker_CheckLatest_NotFound(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	defer srv.Close()

	c := &Checker{
		httpClient:     srv.Client(),
		owner:          "test",
		repo:           "test",
		currentVersion: "1.0.0",
	}
	c.httpClient.Transport = roundTripFunc(func(req *http.Request) (*http.Response, error) {
		req.URL.Scheme = "http"
		req.URL.Host = srv.URL[len("http://"):]
		return http.DefaultTransport.RoundTrip(req)
	})

	_, err := c.CheckLatest(context.Background())
	if err == nil {
		t.Error("expected error for 404 response")
	}
}

// roundTripFunc allows using a function as http.RoundTripper.
type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req)
}
