package updater

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"runtime"
	"strconv"
	"strings"
	"time"
)

// Checker queries GitHub Releases API for the latest version.
type Checker struct {
	httpClient     *http.Client
	owner          string
	repo           string
	currentVersion string
}

// NewChecker creates a Checker for the given GitHub owner/repo.
func NewChecker(owner, repo string) *Checker {
	return &Checker{
		httpClient: &http.Client{
			Timeout: 30 * time.Second,
		},
		owner:          owner,
		repo:           repo,
		currentVersion: CurrentVersion(),
	}
}

// CheckLatest queries the GitHub Releases API for the latest release.
// Returns nil, nil if the current version is already up to date.
func (c *Checker) CheckLatest(ctx context.Context) (*ReleaseInfo, error) {
	url := fmt.Sprintf("https://api.github.com/repos/%s/%s/releases/latest", c.owner, c.repo)

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, fmt.Errorf("updater: check: %w", err)
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("User-Agent", "LeVoile/"+c.currentVersion)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("updater: check: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("updater: check: unexpected status %d", resp.StatusCode)
	}

	const maxResponseSize = 2 * 1024 * 1024 // 2 MB
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxResponseSize))
	if err != nil {
		return nil, fmt.Errorf("updater: check: read body: %w", err)
	}

	info, err := parseGitHubRelease(body)
	if err != nil {
		return nil, fmt.Errorf("updater: check: %w", err)
	}

	if compareVersions(c.currentVersion, info.Version) >= 0 {
		return nil, nil
	}

	return info, nil
}

// githubRelease is the subset of the GitHub Release API response we need.
type githubRelease struct {
	TagName     string         `json:"tag_name"`
	Name        string         `json:"name"`
	Body        string         `json:"body"`
	PublishedAt time.Time      `json:"published_at"`
	Assets      []githubAsset  `json:"assets"`
}

type githubAsset struct {
	Name               string `json:"name"`
	BrowserDownloadURL string `json:"browser_download_url"`
}

// parseGitHubRelease parses a GitHub Release JSON response and extracts
// the relevant assets for the current platform.
func parseGitHubRelease(body []byte) (*ReleaseInfo, error) {
	var rel githubRelease
	if err := json.Unmarshal(body, &rel); err != nil {
		return nil, fmt.Errorf("parse release: %w", err)
	}

	expectedBinary := fmt.Sprintf("le_voile_%s_%s", runtime.GOOS, runtime.GOARCH)
	if runtime.GOOS == "windows" {
		expectedBinary += ".exe"
	}

	info := &ReleaseInfo{
		Version:      strings.TrimPrefix(rel.TagName, "v"),
		ReleaseNotes: rel.Body,
		PublishedAt:  rel.PublishedAt,
	}

	for _, asset := range rel.Assets {
		switch asset.Name {
		case expectedBinary:
			info.DownloadURL = asset.BrowserDownloadURL
		case "checksums.txt":
			info.ChecksumURL = asset.BrowserDownloadURL
		case "checksums.txt.sig":
			info.SignatureURL = asset.BrowserDownloadURL
		}
	}

	if info.DownloadURL == "" {
		return nil, fmt.Errorf("parse release: no asset %q found", expectedBinary)
	}
	if info.ChecksumURL == "" {
		return nil, fmt.Errorf("parse release: no checksums.txt asset found")
	}
	if info.SignatureURL == "" {
		return nil, fmt.Errorf("parse release: no checksums.txt.sig asset found")
	}

	return info, nil
}

// compareVersions compares two semver strings.
// Returns -1 if current < latest, 0 if equal, 1 if current > latest.
// Handles the "v" prefix (v1.2.3 == 1.2.3).
// Non-numeric or malformed versions compare as 0.0.0.
func compareVersions(current, latest string) int {
	parse := func(v string) [3]int {
		v = strings.TrimPrefix(v, "v")
		parts := strings.SplitN(v, ".", 3)
		var nums [3]int
		for i := 0; i < 3 && i < len(parts); i++ {
			n, err := strconv.Atoi(parts[i])
			if err != nil {
				return [3]int{}
			}
			nums[i] = n
		}
		return nums
	}

	c := parse(current)
	l := parse(latest)

	for i := 0; i < 3; i++ {
		if c[i] < l[i] {
			return -1
		}
		if c[i] > l[i] {
			return 1
		}
	}
	return 0
}
