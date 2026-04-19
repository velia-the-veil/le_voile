package updater

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"regexp"
	"strings"
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
	cycleTimeout              = 10 * time.Minute
)

// UpdaterConfig holds configuration for the Updater.
type UpdaterConfig struct {
	Owner                string
	Repo                 string
	PubKeyBase64         string
	StagingDir           string
	CheckInterval        time.Duration
	RateLimitBytesPerSec int64
	// PackageManaged indicates the current binary was installed by a system
	// package manager (dpkg/rpm/pacman). When true, CheckAndDownload skips
	// the download entirely and returns ErrPackageManaged so we don't burn
	// bandwidth on a payload the system will refuse to install.
	PackageManaged bool
	// Logger receives structured `service: updater: <action> ...` lines
	// (Story 8.1 AC11). nil ⇒ io.Discard, so callers who don't care about
	// observability stay backward-compatible. The service wires its
	// `serviceStderr` writer here so kardianos/service forwards every line
	// to syslog (Linux) / Event Log (Windows).
	Logger io.Writer
}

// Updater orchestrates periodic update checking, downloading, and verification.
type Updater struct {
	checker        *Checker
	downloader     *Downloader
	verifier       *Verifier
	stagingDir     string
	checkInterval  time.Duration
	packageManaged bool
	onUpdateReady  func(version string)
	logger         io.Writer
	// homeDir caches os.UserHomeDir() at construction so sanitizePII does not
	// pay a syscall/env lookup per emitted log line. Empty when resolution
	// fails (non-fatal — regex-based path scrubbing still runs).
	homeDir string

	downloading atomic.Bool
	mu          sync.Mutex
	stagedVer   string
	cycleMu     sync.Mutex // serializes CheckAndDownload calls (protects failed version file)
}

// NewUpdater creates an Updater from the given configuration.
// Returns an error if Owner or Repo are empty or contain invalid characters.
func NewUpdater(cfg UpdaterConfig) (*Updater, error) {
	if cfg.Owner == "" || cfg.Repo == "" {
		return nil, fmt.Errorf("updater: owner and repo are required")
	}

	checker, err := NewChecker(cfg.Owner, cfg.Repo)
	if err != nil {
		return nil, fmt.Errorf("updater: %w", err)
	}

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

	logger := cfg.Logger
	if logger == nil {
		logger = io.Discard
	}

	home, _ := os.UserHomeDir() // best-effort; empty string is safe downstream

	return &Updater{
		checker:        checker,
		downloader:     dl,
		verifier:       verifier,
		stagingDir:     cfg.StagingDir,
		checkInterval:  interval,
		packageManaged: cfg.PackageManaged,
		logger:         logger,
		homeDir:        home,
	}, nil
}

// logf emits one canonical `service: updater: ...` line. Story 8.1 AC11.
// Callers should avoid passing IPs, full paths, user names, or payload bytes.
// Story 8.2 NFR22a hardening: args are passed through sanitizePII which
// scrubs user-home path segments that can leak in OS-wrapped errors
// (e.g. "open /home/{user}/.config/levoile/.../failed_version.txt:
// permission denied"). This is a defense-in-depth layer — the intent
// remains that callers stick to action verbs + versions + durations.
func (u *Updater) logf(format string, args ...any) {
	if u.logger == nil {
		return
	}
	scrubbed := make([]any, len(args))
	for i, a := range args {
		scrubbed[i] = sanitizePII(a, u.homeDir)
	}
	fmt.Fprintf(u.logger, "service: updater: "+format+"\n", scrubbed...)
}

// homeUserRE matches user-home paths that carry a username segment.
// Linux/macOS: /home/<user> , /Users/<user>
// Windows:     C:\Users\<user> , C:/Users/<user>
var homeUserRE = regexp.MustCompile(`(?:/home/|/Users/|(?i:[A-Z]:[\\/]Users[\\/]))[^\s/\\]+`)

// rootHomeRE matches the literal /root home directory (service-mode probe
// leak). No trailing username to strip — /root is itself the home.
var rootHomeRE = regexp.MustCompile(`/root\b`)

// sanitizePII scrubs user-identifying path segments from a log arg.
// Errors and strings are pattern-substituted; other types are passed through.
// The replacement collapses the user directory to "$HOME" so subsequent log
// readers see a deterministic, non-identifying placeholder.
//
// WARNING: for error inputs, the return is a fresh errors.New() — the
// original error chain is NOT preserved. errors.Is / errors.As will not
// traverse into the sanitized value. This is safe for the current caller
// (logf consumes the result only for fmt.Fprintf and discards it) but any
// refactor that propagates a sanitized error back to callers must reinstate
// an Unwrap()-capable wrapper.
func sanitizePII(a any, homeDir string) any {
	switch v := a.(type) {
	case error:
		if v == nil {
			return v
		}
		return errors.New(scrubHome(v.Error(), homeDir))
	case string:
		return scrubHome(v, homeDir)
	default:
		return a
	}
}

// scrubHome collapses /home/<user>, /Users/<user>, /root, and
// %USERPROFILE% / C:\Users\<user> variants down to "$HOME". homeDir, when
// non-empty, is substituted literally as a last-pass safety net (picks up
// non-standard locations like snap-confined or rootless-container dirs).
func scrubHome(s, homeDir string) string {
	if s == "" {
		return s
	}
	// ReplaceAllLiteralString — not ReplaceAllString — so `$HOME` is emitted
	// verbatim rather than interpreted as a regexp variable reference.
	out := homeUserRE.ReplaceAllLiteralString(s, "$HOME")
	out = rootHomeRE.ReplaceAllLiteralString(out, "$HOME")
	if homeDir != "" && strings.Contains(out, homeDir) {
		out = strings.ReplaceAll(out, homeDir, "$HOME")
	}
	return out
}

// SetOnUpdateReady sets the callback invoked when a verified update is staged.
func (u *Updater) SetOnUpdateReady(fn func(version string)) {
	u.onUpdateReady = fn
}

// seedMaxSeenVersion writes the currently-running version to the persisted
// anti-downgrade marker when no prior marker exists, or when the running
// version is strictly higher than the stored one. Never lowers the marker.
func (u *Updater) seedMaxSeenVersion() {
	current := CurrentVersion()
	if current == "" || current == "dev" {
		return
	}
	stored, err := ReadMaxSeenVersion(u.stagingDir)
	if err != nil {
		u.logf("seed max-seen-version read err=%v", err)
		return
	}
	if stored != "" && compareVersions(current, stored) <= 0 {
		return
	}
	if err := WriteMaxSeenVersion(u.stagingDir, current); err != nil {
		u.logf("seed max-seen-version write err=%v", err)
	}
}

// Start begins the periodic update check loop.
// It waits initialDelay before the first check, then checks every checkInterval.
// The loop is interruptible via context cancellation.
func (u *Updater) Start(ctx context.Context) error {
	if u.packageManaged {
		// Package-managed install: auto-update is authoritative for dpkg/rpm/pacman,
		// not for us. Don't run the periodic loop at all.
		return ErrPackageManaged
	}

	// Seed the anti-downgrade baseline on first run. Monotonic: only written
	// when the current binary's version is strictly higher than what's
	// already persisted. Best-effort — a failure here still lets the cycle
	// proceed, it just means a legitimate fresh install hasn't yet marked
	// its baseline and an attacker would need a different vector.
	u.seedMaxSeenVersion()

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
		cycleCtx, cycleCancel := context.WithTimeout(ctx, cycleTimeout)
		_, err := u.CheckAndDownload(cycleCtx)
		cycleCancel()
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

	cycleStart := time.Now()
	u.logf("cycle start")

	if u.packageManaged {
		// Short-circuit before the HTTP call — no point consulting GitHub or
		// spending bandwidth when the eventual install will be rejected.
		u.logf("skip package-managed install")
		return nil, ErrPackageManaged
	}

	release, err := u.checker.CheckLatest(ctx)
	if err != nil {
		u.logf("check failed err=%v", err)
		return nil, fmt.Errorf("updater: check and download: %w", err)
	}
	if release == nil {
		u.logf("up to date duration_ms=%d", time.Since(cycleStart).Milliseconds())
		return nil, nil // up to date
	}

	// Anti-downgrade gate: refuse any release older than the highest version
	// this client has ever installed. Defends against a leaked signing key
	// being used to push clients back to a vulnerable prior release.
	maxSeen, err := ReadMaxSeenVersion(u.stagingDir)
	if err != nil {
		u.logf("read max-seen-version err=%v", err)
		return nil, fmt.Errorf("updater: check and download: read max seen: %w", err)
	}
	if maxSeen != "" && compareVersions(release.Version, maxSeen) < 0 {
		u.logf("skip downgrade candidate=%s max_seen=%s", release.Version, maxSeen)
		return nil, ErrDowngradeRejected
	}

	// Check if this version previously failed (rollback occurred)
	failedVer, err := ReadFailedVersion(u.stagingDir)
	if err != nil {
		u.logf("read failed-version err=%v", err)
		return nil, fmt.Errorf("updater: check and download: read failed version: %w", err)
	}
	if failedVer != "" {
		if release.Version == failedVer {
			// Same version that failed — skip silently from the user's
			// perspective, but still log so operators investigating a stuck
			// rollout can see the skip.
			u.logf("skip rollback-marked version=%s", release.Version)
			return nil, nil
		}
		// New release available — clear the failed version marker
		if err := ClearFailedVersion(u.stagingDir); err != nil {
			u.logf("clear failed-version err=%v", err)
			return nil, fmt.Errorf("updater: check and download: clear failed version: %w", err)
		}
	}

	u.downloading.Store(true)
	defer u.downloading.Store(false)

	u.logf("download start version=%s", release.Version)
	dlStart := time.Now()
	staged, err := u.downloader.DownloadRelease(ctx, release)
	if err != nil {
		u.logf("download failed version=%s err=%v", release.Version, err)
		return nil, fmt.Errorf("updater: check and download: %w", err)
	}
	u.logf("download done version=%s duration_ms=%d", staged.Version, time.Since(dlStart).Milliseconds())

	if err := u.verifier.VerifyStagedUpdate(staged); err != nil {
		u.logf("verify failed version=%s err=%v", staged.Version, err)
		return nil, fmt.Errorf("updater: check and download: verify: %w", err)
	}
	u.logf("verify ok version=%s", staged.Version)

	// Persist staged version for the installer to read at next startup
	if err := writeStagedVersion(u.downloader.stagingDir, staged.Version); err != nil {
		u.logf("persist staged-version err=%v", err)
		return nil, fmt.Errorf("updater: check and download: %w", err)
	}

	u.mu.Lock()
	u.stagedVer = staged.Version
	u.mu.Unlock()

	if u.onUpdateReady != nil {
		u.onUpdateReady(staged.Version)
	}

	u.logf("update ready version=%s duration_ms=%d", staged.Version, time.Since(cycleStart).Milliseconds())
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
