package updater

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"runtime"
	"strconv"
	"strings"
	"time"
)

// validGitHubName matches valid GitHub owner and repo names.
var validGitHubName = regexp.MustCompile(`^[a-zA-Z0-9._-]+$`)

// Checker queries GitHub Releases API for the latest version.
type Checker struct {
	httpClient     *http.Client
	owner          string
	repo           string
	currentVersion string
}

// NewChecker creates a Checker for the given GitHub owner/repo.
// Returns an error if owner or repo contain invalid characters.
func NewChecker(owner, repo string) (*Checker, error) {
	if !validGitHubName.MatchString(owner) {
		return nil, fmt.Errorf("updater: checker: invalid owner %q", owner)
	}
	if !validGitHubName.MatchString(repo) {
		return nil, fmt.Errorf("updater: checker: invalid repo %q", repo)
	}
	return &Checker{
		httpClient: &http.Client{
			Timeout: 30 * time.Second,
		},
		owner:          owner,
		repo:           repo,
		currentVersion: CurrentVersion(),
	}, nil
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

// parsedVersion holds a parsed semver version with optional pre-release suffix.
type parsedVersion struct {
	nums       [3]int
	prerelease string // empty string means stable release
}

// compareVersions compares two semver strings.
// Returns -1 if current < latest, 0 if equal, 1 if current > latest.
// Handles the "v" prefix (v1.2.3 == 1.2.3).
// Pre-release versions (e.g., 1.2.3-beta.1) compare as less than the
// corresponding release (1.2.3-beta.1 < 1.2.3).
// Non-numeric or malformed versions compare as 0.0.0.
func compareVersions(current, latest string) int {
	parse := func(v string) parsedVersion {
		v = strings.TrimPrefix(v, "v")
		var pv parsedVersion
		parts := strings.SplitN(v, ".", 3)
		for i := 0; i < 3 && i < len(parts); i++ {
			seg := parts[i]
			// Strip pre-release suffix from the last segment (e.g., "3-beta.1" → "3").
			if i == len(parts)-1 {
				if idx := strings.IndexByte(seg, '-'); idx >= 0 {
					pv.prerelease = seg[idx+1:]
					seg = seg[:idx]
				}
			}
			n, err := strconv.Atoi(seg)
			if err != nil {
				return parsedVersion{}
			}
			pv.nums[i] = n
		}
		return pv
	}

	c := parse(current)
	l := parse(latest)

	for i := 0; i < 3; i++ {
		if c.nums[i] < l.nums[i] {
			return -1
		}
		if c.nums[i] > l.nums[i] {
			return 1
		}
	}

	// Same numeric version — pre-release < stable.
	switch {
	case c.prerelease != "" && l.prerelease == "":
		return -1
	case c.prerelease == "" && l.prerelease != "":
		return 1
	case c.prerelease != "" && l.prerelease != "":
		if c.prerelease < l.prerelease {
			return -1
		}
		if c.prerelease > l.prerelease {
			return 1
		}
	}
	return 0
}
