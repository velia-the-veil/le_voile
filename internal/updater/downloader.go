package updater

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"golang.org/x/time/rate"
)

const (
	defaultRateLimitBytesPerSec = 512 * 1024 // 512 KB/s
	allowedDownloadHost         = "github.com"
	allowedDownloadPathPrefix   = "/velia-the-veil/le_voile/releases"
)

// StagedUpdate holds paths to downloaded and verified update files.
type StagedUpdate struct {
	BinaryPath    string
	ChecksumPath  string
	SignaturePath string
	Version       string
	VersionFile   string // path to staged_version.txt (set by installer)
}

// Downloader handles rate-limited file downloads.
type Downloader struct {
	httpClient *http.Client
	rateLimit  int64
	stagingDir string
}

// NewDownloader creates a Downloader that stores files in stagingDir.
// Creates the staging directory if it does not exist.
func NewDownloader(stagingDir string) (*Downloader, error) {
	if err := os.MkdirAll(stagingDir, 0o700); err != nil {
		return nil, fmt.Errorf("updater: downloader: create staging dir: %w", err)
	}
	return &Downloader{
		httpClient: &http.Client{Timeout: 10 * time.Minute},
		rateLimit:  defaultRateLimitBytesPerSec,
		stagingDir: stagingDir,
	}, nil
}

// SetRateLimit sets the download rate limit in bytes per second.
func (d *Downloader) SetRateLimit(bytesPerSec int64) {
	if bytesPerSec > 0 {
		d.rateLimit = bytesPerSec
	}
}

// Download downloads a file from url to the staging directory with rate limiting.
// Returns the path of the downloaded file. The download is atomic: the file is
// written to a .tmp file first, then renamed on success.
func (d *Downloader) Download(ctx context.Context, rawURL string) (string, error) {
	if err := validateDownloadURL(rawURL); err != nil {
		return "", fmt.Errorf("updater: download: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return "", fmt.Errorf("updater: download: %w", err)
	}
	req.Header.Set("User-Agent", "LeVoile/"+CurrentVersion())

	resp, err := d.httpClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("updater: download: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("updater: download: unexpected status %d", resp.StatusCode)
	}

	parsed, _ := url.Parse(rawURL) // already validated above
	filename := filepath.Base(parsed.Path)
	tmpPath := filepath.Join(d.stagingDir, filename+".tmp")
	finalPath := filepath.Join(d.stagingDir, filename)

	f, err := os.OpenFile(tmpPath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o600)
	if err != nil {
		return "", fmt.Errorf("updater: download: create file: %w", err)
	}

	reader := newRateLimitedReader(ctx, resp.Body, d.rateLimit)
	_, copyErr := io.Copy(f, reader)
	closeErr := f.Close()

	if copyErr != nil {
		os.Remove(tmpPath)
		return "", fmt.Errorf("updater: download: %w", copyErr)
	}
	if closeErr != nil {
		os.Remove(tmpPath)
		return "", fmt.Errorf("updater: download: close: %w", closeErr)
	}

	if err := os.Rename(tmpPath, finalPath); err != nil {
		os.Remove(tmpPath)
		return "", fmt.Errorf("updater: download: rename: %w", err)
	}

	return finalPath, nil
}

// DownloadRelease downloads the binary, checksums, and signature for a release.
func (d *Downloader) DownloadRelease(ctx context.Context, release *ReleaseInfo) (*StagedUpdate, error) {
	binaryPath, err := d.Download(ctx, release.DownloadURL)
	if err != nil {
		return nil, fmt.Errorf("updater: download release: binary: %w", err)
	}

	checksumPath, err := d.Download(ctx, release.ChecksumURL)
	if err != nil {
		os.Remove(binaryPath)
		return nil, fmt.Errorf("updater: download release: checksums: %w", err)
	}

	sigPath, err := d.Download(ctx, release.SignatureURL)
	if err != nil {
		os.Remove(binaryPath)
		os.Remove(checksumPath)
		return nil, fmt.Errorf("updater: download release: signature: %w", err)
	}

	return &StagedUpdate{
		BinaryPath:    binaryPath,
		ChecksumPath:  checksumPath,
		SignaturePath: sigPath,
		Version:       release.Version,
	}, nil
}

// validateDownloadURL ensures the URL points to the allowed GitHub repository.
func validateDownloadURL(rawURL string) error {
	u, err := url.Parse(rawURL)
	if err != nil {
		return fmt.Errorf("invalid URL: %w", err)
	}
	if u.Scheme != "https" {
		return fmt.Errorf("URL must use HTTPS, got %q", u.Scheme)
	}
	if u.Host != allowedDownloadHost {
		return fmt.Errorf("URL host %q not allowed, must be %q", u.Host, allowedDownloadHost)
	}
	if !strings.HasPrefix(u.Path, allowedDownloadPathPrefix) {
		return fmt.Errorf("URL path %q not in allowed prefix %q", u.Path, allowedDownloadPathPrefix)
	}
	return nil
}

// rateLimitedReader wraps an io.Reader with a rate limiter.
type rateLimitedReader struct {
	reader  io.Reader
	limiter *rate.Limiter
	ctx     context.Context
}

func newRateLimitedReader(ctx context.Context, r io.Reader, bytesPerSec int64) *rateLimitedReader {
	return &rateLimitedReader{
		reader:  r,
		limiter: rate.NewLimiter(rate.Limit(bytesPerSec), int(bytesPerSec)),
		ctx:     ctx,
	}
}

func (r *rateLimitedReader) Read(p []byte) (int, error) {
	// Cap read buffer to burst size to prevent WaitN from exceeding the limiter's burst.
	burst := r.limiter.Burst()
	if len(p) > burst {
		p = p[:burst]
	}
	n, err := r.reader.Read(p)
	if n > 0 {
		if waitErr := r.limiter.WaitN(r.ctx, n); waitErr != nil {
			return n, waitErr
		}
	}
	return n, err
}
