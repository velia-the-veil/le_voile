package updater

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"time"
)

const (
	defaultCheckInterval      = 6 * time.Hour
	initialDelay              = 1 * time.Minute
	retryIntervalOnNetwork    = 30 * time.Minute
	retryIntervalOnIntegrity  = 1 * time.Hour
	maxConsecutiveRetries     = 3
)

// UpdaterConfig holds configuration for the Updater.
type UpdaterConfig struct {
	Owner                string
	Repo                 string
	PubKeyBase64         string
	StagingDir           string
	CheckInterval        time.Duration
	RateLimitBytesPerSec int64
}

// Updater orchestrates periodic update checking, downloading, and verification.
type Updater struct {
	checker       *Checker
	downloader    *Downloader
	verifier      *Verifier
	stagingDir    string
	checkInterval time.Duration
	onUpdateReady func(version string)

	downloading atomic.Bool
	mu          sync.Mutex
	stagedVer   string
	cycleMu     sync.Mutex // serializes CheckAndDownload calls (protects failed version file)
}

// NewUpdater creates an Updater from the given configuration.
func NewUpdater(cfg UpdaterConfig) (*Updater, error) {
	checker := NewChecker(cfg.Owner, cfg.Repo)

	dl, err := NewDownloader(cfg.StagingDir)
	if err != nil {
		return nil, fmt.Errorf("updater: %w", err)
	}
	if cfg.RateLimitBytesPerSec > 0 {
		dl.SetRateLimit(cfg.RateLimitBytesPerSec)
	}

	verifier, err := NewVerifier(cfg.PubKeyBase64)
	if err != nil {
		return nil, fmt.Errorf("updater: %w", err)
	}

	interval := cfg.CheckInterval
	if interval <= 0 {
		interval = defaultCheckInterval
	}

	return &Updater{
		checker:       checker,
		downloader:    dl,
		verifier:      verifier,
		stagingDir:    cfg.StagingDir,
		checkInterval: interval,
	}, nil
}

// SetOnUpdateReady sets the callback invoked when a verified update is staged.
func (u *Updater) SetOnUpdateReady(fn func(version string)) {
	u.onUpdateReady = fn
}

// Start begins the periodic update check loop.
// It waits initialDelay before the first check, then checks every checkInterval.
// The loop is interruptible via context cancellation.
func (u *Updater) Start(ctx context.Context) error {
	// Wait initial delay before first check
	initTimer := time.NewTimer(initialDelay)
	select {
	case <-initTimer.C:
	case <-ctx.Done():
		initTimer.Stop()
		return ctx.Err()
	}

	consecutiveFailures := 0
	for {
		_, err := u.CheckAndDownload(ctx)
		if err != nil {
			consecutiveFailures++
			var wait time.Duration
			if consecutiveFailures >= maxConsecutiveRetries {
				// Reset to normal cycle after max retries
				consecutiveFailures = 0
				wait = u.checkInterval
			} else if isIntegrityError(err) {
				// Integrity failures (checksum/signature) use longer retry interval
				wait = retryIntervalOnIntegrity
			} else {
				wait = retryIntervalOnNetwork
			}
			retryTimer := time.NewTimer(wait)
			select {
			case <-retryTimer.C:
			case <-ctx.Done():
				retryTimer.Stop()
				return ctx.Err()
			}
			continue
		}

		consecutiveFailures = 0
		cycleTimer := time.NewTimer(u.checkInterval)
		select {
		case <-cycleTimer.C:
		case <-ctx.Done():
			cycleTimer.Stop()
			return ctx.Err()
		}
	}
}

// CheckAndDownload performs a single check+download+verify cycle.
// Returns the StagedUpdate if a new version was found and verified, or nil if up to date.
// Serialized via cycleMu to prevent concurrent access to failed version state.
func (u *Updater) CheckAndDownload(ctx context.Context) (*StagedUpdate, error) {
	u.cycleMu.Lock()
	defer u.cycleMu.Unlock()

	release, err := u.checker.CheckLatest(ctx)
	if err != nil {
		return nil, fmt.Errorf("updater: check and download: %w", err)
	}
	if release == nil {
		return nil, nil // up to date
	}

	// Check if this version previously failed (rollback occurred)
	failedVer, err := ReadFailedVersion(u.stagingDir)
	if err != nil {
		return nil, fmt.Errorf("updater: check and download: read failed version: %w", err)
	}
	if failedVer != "" {
		if release.Version == failedVer {
			// Same version that failed — skip silently
			return nil, nil
		}
		// New release available — clear the failed version marker
		if err := ClearFailedVersion(u.stagingDir); err != nil {
			return nil, fmt.Errorf("updater: check and download: clear failed version: %w", err)
		}
	}

	u.downloading.Store(true)
	defer u.downloading.Store(false)

	staged, err := u.downloader.DownloadRelease(ctx, release)
	if err != nil {
		return nil, fmt.Errorf("updater: check and download: %w", err)
	}

	if err := u.verifier.VerifyStagedUpdate(staged); err != nil {
		return nil, fmt.Errorf("updater: check and download: verify: %w", err)
	}

	// Persist staged version for the installer to read at next startup
	if err := writeStagedVersion(u.downloader.stagingDir, staged.Version); err != nil {
		return nil, fmt.Errorf("updater: check and download: %w", err)
	}

	u.mu.Lock()
	u.stagedVer = staged.Version
	u.mu.Unlock()

	if u.onUpdateReady != nil {
		u.onUpdateReady(staged.Version)
	}

	return staged, nil
}

// StagedVersion returns the version of the currently staged update, or empty string.
func (u *Updater) StagedVersion() string {
	u.mu.Lock()
	defer u.mu.Unlock()
	return u.stagedVer
}

// IsDownloading returns true if a download is currently in progress.
func (u *Updater) IsDownloading() bool {
	return u.downloading.Load()
}

// isIntegrityError returns true if the error is related to checksum or signature verification.
func isIntegrityError(err error) bool {
	return errors.Is(err, ErrChecksumMismatch) ||
		errors.Is(err, ErrSignatureInvalid) ||
		errors.Is(err, ErrNoMatchingChecksum)
}
