package updater

import (
	"context"
	"crypto/sha256"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/velia-the-veil/le_voile/internal/crypto"
)

// testUpdaterEnv sets up a complete test environment with mock server and keys.
type testUpdaterEnv struct {
	server     *httptest.Server
	pub        string
	stagingDir string
	binaryName string
}

func setupTestUpdaterEnv(t *testing.T, binaryContent string, version string) *testUpdaterEnv {
	t.Helper()

	pub, priv, err := crypto.GenerateKeyPair()
	if err != nil {
		t.Fatalf("generate key pair: %v", err)
	}
	pubBase64 := crypto.ExportPublicKeyBase64(pub)

	binaryName := fmt.Sprintf("le_voile_%s_%s", runtime.GOOS, runtime.GOARCH)
	if runtime.GOOS == "windows" {
		binaryName += ".exe"
	}

	hash := fmt.Sprintf("%x", sha256.Sum256([]byte(binaryContent)))
	checksumContent := fmt.Sprintf("%s  %s\n", hash, binaryName)

	sig, err := crypto.Sign(priv, []byte(checksumContent))
	if err != nil {
		t.Fatalf("sign: %v", err)
	}
	sigBytes := sig

	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		path := r.URL.Path
		switch {
		case strings.Contains(path, "releases/latest"):
			fmt.Fprintf(w, `{
				"tag_name": "v%s",
				"published_at": "2026-03-10T12:00:00Z",
				"assets": [
					{"name": %q, "browser_download_url": "https://github.com/velia-the-veil/le_voile/releases/download/v%s/%s"},
					{"name": "checksums.txt", "browser_download_url": "https://github.com/velia-the-veil/le_voile/releases/download/v%s/checksums.txt"},
					{"name": "checksums.txt.sig", "browser_download_url": "https://github.com/velia-the-veil/le_voile/releases/download/v%s/checksums.txt.sig"}
				]
			}`, version, binaryName, version, binaryName, version, version)
		case strings.HasSuffix(path, binaryName):
			w.Write([]byte(binaryContent))
		case strings.HasSuffix(path, "checksums.txt.sig"):
			w.Write(sigBytes)
		case strings.HasSuffix(path, "checksums.txt"):
			w.Write([]byte(checksumContent))
		default:
			http.NotFound(w, r)
		}
	}))

	return &testUpdaterEnv{
		server:     srv,
		pub:        pubBase64,
		stagingDir: t.TempDir(),
		binaryName: binaryName,
	}
}

func (e *testUpdaterEnv) newUpdater(t *testing.T, currentVersion string) *Updater {
	t.Helper()

	orig := Version
	Version = currentVersion
	t.Cleanup(func() { Version = orig })

	upd, err := NewUpdater(UpdaterConfig{
		Owner:                "velia-the-veil",
		Repo:                 "le_voile",
		PubKeyBase64:         e.pub,
		StagingDir:           e.stagingDir,
		CheckInterval:        100 * time.Millisecond,
		RateLimitBytesPerSec: 0, // no limit for tests
	})
	if err != nil {
		t.Fatalf("new updater: %v", err)
	}

	// Redirect HTTP requests to test server
	srvURL, _ := url.Parse(e.server.URL)
	transport := e.server.Client().Transport
	upd.checker.httpClient.Transport = roundTripFunc(func(req *http.Request) (*http.Response, error) {
		req.URL.Scheme = srvURL.Scheme
		req.URL.Host = srvURL.Host
		return transport.RoundTrip(req)
	})
	upd.downloader.httpClient.Transport = roundTripFunc(func(req *http.Request) (*http.Response, error) {
		req.URL.Scheme = srvURL.Scheme
		req.URL.Host = srvURL.Host
		return transport.RoundTrip(req)
	})

	return upd
}

// newUpdaterWithLogger mirrors newUpdater but routes structured log output to
// the caller-provided writer. Used by Story 8.1 AC11 logging tests.
func (e *testUpdaterEnv) newUpdaterWithLogger(t *testing.T, currentVersion string, logger io.Writer) *Updater {
	t.Helper()

	orig := Version
	Version = currentVersion
	t.Cleanup(func() { Version = orig })

	upd, err := NewUpdater(UpdaterConfig{
		Owner:                "velia-the-veil",
		Repo:                 "le_voile",
		PubKeyBase64:         e.pub,
		StagingDir:           e.stagingDir,
		CheckInterval:        100 * time.Millisecond,
		RateLimitBytesPerSec: 0,
		Logger:               logger,
	})
	if err != nil {
		t.Fatalf("new updater: %v", err)
	}
	srvURL, _ := url.Parse(e.server.URL)
	transport := e.server.Client().Transport
	upd.checker.httpClient.Transport = roundTripFunc(func(req *http.Request) (*http.Response, error) {
		req.URL.Scheme = srvURL.Scheme
		req.URL.Host = srvURL.Host
		return transport.RoundTrip(req)
	})
	upd.downloader.httpClient.Transport = roundTripFunc(func(req *http.Request) (*http.Response, error) {
		req.URL.Scheme = srvURL.Scheme
		req.URL.Host = srvURL.Host
		return transport.RoundTrip(req)
	})
	return upd
}

func TestUpdater_CheckAndDownload_NewVersion(t *testing.T) {
	env := setupTestUpdaterEnv(t, "binary content v2", "2.0.0")
	defer env.server.Close()

	upd := env.newUpdater(t, "1.0.0")

	staged, err := upd.CheckAndDownload(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if staged == nil {
		t.Fatal("expected staged update, got nil")
	}
	if staged.Version != "2.0.0" {
		t.Errorf("Version = %q, want %q", staged.Version, "2.0.0")
	}

	// Verify files exist
	for _, p := range []string{staged.BinaryPath, staged.ChecksumPath, staged.SignaturePath} {
		if _, err := os.Stat(p); err != nil {
			t.Errorf("staged file missing: %s", p)
		}
	}

	if upd.StagedVersion() != "2.0.0" {
		t.Errorf("StagedVersion() = %q, want %q", upd.StagedVersion(), "2.0.0")
	}
}

func TestUpdater_CheckAndDownload_UpToDate(t *testing.T) {
	env := setupTestUpdaterEnv(t, "binary content", "1.0.0")
	defer env.server.Close()

	upd := env.newUpdater(t, "1.0.0")

	staged, err := upd.CheckAndDownload(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if staged != nil {
		t.Errorf("expected nil (up to date), got version %s", staged.Version)
	}
}

func TestUpdater_CheckAndDownload_Callback(t *testing.T) {
	env := setupTestUpdaterEnv(t, "binary content v3", "3.0.0")
	defer env.server.Close()

	upd := env.newUpdater(t, "1.0.0")

	var callbackVersion string
	upd.SetOnUpdateReady(func(version string) {
		callbackVersion = version
	})

	_, err := upd.CheckAndDownload(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if callbackVersion != "3.0.0" {
		t.Errorf("callback version = %q, want %q", callbackVersion, "3.0.0")
	}
}

func TestUpdater_Start_ContextCancel(t *testing.T) {
	env := setupTestUpdaterEnv(t, "binary", "1.0.0")
	defer env.server.Close()

	upd := env.newUpdater(t, "1.0.0")

	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()

	err := upd.Start(ctx)
	if err == nil || err != context.DeadlineExceeded {
		t.Errorf("expected context.DeadlineExceeded, got %v", err)
	}
}

func TestUpdater_CheckAndDownload_WritesStagedVersion(t *testing.T) {
	env := setupTestUpdaterEnv(t, "binary content v2", "2.0.0")
	defer env.server.Close()

	upd := env.newUpdater(t, "1.0.0")

	staged, err := upd.CheckAndDownload(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if staged == nil {
		t.Fatal("expected staged update, got nil")
	}

	// Verify staged_version.txt was written
	versionPath := filepath.Join(env.stagingDir, "staged_version.txt")
	data, err := os.ReadFile(versionPath)
	if err != nil {
		t.Fatalf("staged_version.txt not found: %v", err)
	}
	if string(data) != "2.0.0" {
		t.Errorf("staged_version.txt = %q, want %q", string(data), "2.0.0")
	}
}

func TestUpdater_SkipsFailedVersion(t *testing.T) {
	env := setupTestUpdaterEnv(t, "binary content v2", "2.0.0")
	defer env.server.Close()

	upd := env.newUpdater(t, "1.0.0")

	// Write failed version marker for the same version
	if err := WriteFailedVersion(env.stagingDir, "2.0.0"); err != nil {
		t.Fatalf("WriteFailedVersion: %v", err)
	}

	staged, err := upd.CheckAndDownload(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if staged != nil {
		t.Errorf("expected nil (skipped failed version), got version %s", staged.Version)
	}

	// Verify failed_version.txt still exists (not cleared)
	failedVer, err := ReadFailedVersion(env.stagingDir)
	if err != nil {
		t.Fatalf("ReadFailedVersion: %v", err)
	}
	if failedVer != "2.0.0" {
		t.Errorf("failed version should still be %q, got %q", "2.0.0", failedVer)
	}
}

// TestSanitizePII_ScrubsUserHome is the NFR22a regression suite for the
// log sanitizer introduced in Story 8.2. Every input that could leak a
// user-identifying path segment from an OS-wrapped error must come out with
// "$HOME" instead of the original username.
func TestSanitizePII_ScrubsUserHome(t *testing.T) {
	cases := []struct {
		name string
		in   any
		want string
	}{
		{
			"linux /home path in error",
			fmt.Errorf("open /home/akerimus/.config/levoile/updates/failed_version.txt: permission denied"),
			"open $HOME/.config/levoile/updates/failed_version.txt: permission denied",
		},
		{
			"macOS /Users path in string",
			"open /Users/alice/Library/Application Support/levoile: denied",
			"open $HOME/Library/Application Support/levoile: denied",
		},
		{
			"windows C:\\Users path in error",
			fmt.Errorf(`open C:\Users\bob\AppData\Local\LeVoile\updates\failed_version.txt: The system cannot find the file specified.`),
			`open $HOME\AppData\Local\LeVoile\updates\failed_version.txt: The system cannot find the file specified.`,
		},
		{
			"root home (service mode probe leak)",
			"open /root/.config/levoile: permission denied",
			"open $HOME/.config/levoile: permission denied",
		},
		{
			"no user path, untouched",
			"cycle start",
			"cycle start",
		},
		{
			"version string, untouched",
			"version=2.0.0",
			"version=2.0.0",
		},
		{
			"system path /var/lib not touched",
			"open /var/lib/levoile/updates/failed_version.txt: permission denied",
			"open /var/lib/levoile/updates/failed_version.txt: permission denied",
		},
		{
			"nil error safe",
			error(nil),
			"<nil>", // fmt later renders as <nil>; we verify no panic
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			// Pass empty homeDir — the regex alone must cover these cases.
			// The cached-homeDir path is exercised by TestUpdater_Logger_ScrubsPII.
			got := sanitizePII(tc.in, "")
			// For error types compare via Error(); for strings direct; nil passes through.
			switch v := got.(type) {
			case error:
				if v == nil {
					return // nothing to verify
				}
				if v.Error() != tc.want {
					t.Errorf("sanitizePII(%v) = %q, want %q", tc.in, v.Error(), tc.want)
				}
			case string:
				if v != tc.want {
					t.Errorf("sanitizePII(%v) = %q, want %q", tc.in, v, tc.want)
				}
			default:
				if tc.in == nil {
					return
				}
				t.Errorf("unexpected sanitized type %T", got)
			}
		})
	}
}

// TestSanitizePII_HomeDirFallback exercises the secondary-pass literal
// substitution via the cached homeDir arg. Covers non-standard home layouts
// the regex alone cannot match (snap-confined dirs, rootless containers,
// custom /mnt/homes layouts). Story 8.2 L1 cache-at-construction fix.
func TestSanitizePII_HomeDirFallback(t *testing.T) {
	out := sanitizePII("open /mnt/nas/homes/akerimus/cfg: denied", "/mnt/nas/homes/akerimus")
	got, ok := out.(string)
	if !ok {
		t.Fatalf("expected string, got %T", out)
	}
	if !strings.Contains(got, "$HOME") {
		t.Errorf("expected $HOME placeholder from homeDir fallback, got: %q", got)
	}
	if strings.Contains(got, "akerimus") {
		t.Errorf("PII leak despite homeDir fallback: %q", got)
	}
}

// TestUpdater_Logger_ScrubsPII verifies end-to-end that the Logger writer
// receives sanitized lines: a simulated filesystem error containing a
// user-home path must emerge with "$HOME" in the log buffer, never the
// original username. Guards against accidental reversion of logf's
// sanitizePII wrapping.
func TestUpdater_Logger_ScrubsPII(t *testing.T) {
	var buf strings.Builder

	env := setupTestUpdaterEnv(t, "payload", "1.0.0")
	defer env.server.Close()

	upd := env.newUpdaterWithLogger(t, "0.9.0", &buf)
	// Inject an error whose message contains a synthetic user-home path by
	// calling logf directly — this is the surface the package-internal call
	// sites feed. (Crafting a real os.ReadFile error with a controlled path
	// would require root or a fake FS.)
	upd.logf("read failed-version err=%v",
		fmt.Errorf("open /home/test-user/.config/levoile/updates/failed_version.txt: permission denied"))

	got := buf.String()
	if strings.Contains(got, "/home/test-user") {
		t.Errorf("PII leak: logger output contains /home/test-user\nline=%q", got)
	}
	if !strings.Contains(got, "$HOME") {
		t.Errorf("expected $HOME placeholder, got: %q", got)
	}
}

// TestUpdater_PackageManaged_ShortCircuits confirms that when PackageManaged=true
// the updater refuses to contact GitHub or download anything, returning
// ErrPackageManaged directly. Avoids burning bandwidth on a payload the system
// will refuse to install.
func TestUpdater_PackageManaged_ShortCircuits(t *testing.T) {
	env := setupTestUpdaterEnv(t, "binary content v2", "2.0.0")
	defer env.server.Close()

	// Build updater with PackageManaged=true. We intentionally don't redirect
	// HTTP transport here — if the short-circuit fails the real GitHub host
	// would be hit, which is itself a defect.
	orig := Version
	Version = "1.0.0"
	t.Cleanup(func() { Version = orig })

	upd, err := NewUpdater(UpdaterConfig{
		Owner:          "velia-the-veil",
		Repo:           "le_voile",
		PubKeyBase64:   env.pub,
		StagingDir:     env.stagingDir,
		CheckInterval:  100 * time.Millisecond,
		PackageManaged: true,
	})
	if err != nil {
		t.Fatalf("new updater: %v", err)
	}

	staged, err := upd.CheckAndDownload(context.Background())
	if err != ErrPackageManaged {
		t.Errorf("expected ErrPackageManaged, got %v", err)
	}
	if staged != nil {
		t.Errorf("expected no staged update, got %+v", staged)
	}

	// Start() should also bail out immediately with ErrPackageManaged.
	if err := upd.Start(context.Background()); err != ErrPackageManaged {
		t.Errorf("Start: expected ErrPackageManaged, got %v", err)
	}
}

func TestUpdater_ClearsFailedVersion_NewRelease(t *testing.T) {
	env := setupTestUpdaterEnv(t, "binary content v3", "3.0.0")
	defer env.server.Close()

	upd := env.newUpdater(t, "1.0.0")

	// Write failed version marker for a DIFFERENT (old) version
	if err := WriteFailedVersion(env.stagingDir, "2.0.0"); err != nil {
		t.Fatalf("WriteFailedVersion: %v", err)
	}

	staged, err := upd.CheckAndDownload(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if staged == nil {
		t.Fatal("expected staged update for new release, got nil")
	}
	if staged.Version != "3.0.0" {
		t.Errorf("Version = %q, want %q", staged.Version, "3.0.0")
	}

	// Verify failed_version.txt was cleared
	failedVer, err := ReadFailedVersion(env.stagingDir)
	if err != nil {
		t.Fatalf("ReadFailedVersion: %v", err)
	}
	if failedVer != "" {
		t.Errorf("failed version should be cleared, got %q", failedVer)
	}
}

func TestUpdater_CheckAndDownload_VerificationFail(t *testing.T) {
	// Create an environment with a binary whose checksum won't match
	pub, _, err := crypto.GenerateKeyPair()
	if err != nil {
		t.Fatalf("generate key pair: %v", err)
	}
	pubBase64 := crypto.ExportPublicKeyBase64(pub)

	binaryName := fmt.Sprintf("le_voile_%s_%s", runtime.GOOS, runtime.GOARCH)
	if runtime.GOOS == "windows" {
		binaryName += ".exe"
	}

	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		path := r.URL.Path
		switch {
		case strings.Contains(path, "releases/latest"):
			fmt.Fprintf(w, `{
				"tag_name": "v2.0.0",
				"published_at": "2026-03-10T12:00:00Z",
				"assets": [
					{"name": %q, "browser_download_url": "https://github.com/velia-the-veil/le_voile/releases/download/v2.0.0/%s"},
					{"name": "checksums.txt", "browser_download_url": "https://github.com/velia-the-veil/le_voile/releases/download/v2.0.0/checksums.txt"},
					{"name": "checksums.txt.sig", "browser_download_url": "https://github.com/velia-the-veil/le_voile/releases/download/v2.0.0/checksums.txt.sig"}
				]
			}`, binaryName, binaryName)
		case strings.HasSuffix(path, binaryName):
			w.Write([]byte("the binary"))
		case strings.HasSuffix(path, "checksums.txt.sig"):
			w.Write(make([]byte, 64))
		case strings.HasSuffix(path, "checksums.txt"):
			// Wrong checksum
			w.Write([]byte("0000000000000000000000000000000000000000000000000000000000000000  " + binaryName + "\n"))
		}
	}))
	defer srv.Close()

	orig := Version
	Version = "1.0.0"
	defer func() { Version = orig }()

	stagingDir := t.TempDir()
	upd, err := NewUpdater(UpdaterConfig{
		Owner:        "velia-the-veil",
		Repo:         "le_voile",
		PubKeyBase64: pubBase64,
		StagingDir:   stagingDir,
	})
	if err != nil {
		t.Fatalf("new updater: %v", err)
	}

	srvURL, _ := url.Parse(srv.URL)
	transport := srv.Client().Transport
	redirect := roundTripFunc(func(req *http.Request) (*http.Response, error) {
		req.URL.Scheme = srvURL.Scheme
		req.URL.Host = srvURL.Host
		return transport.RoundTrip(req)
	})
	upd.checker.httpClient.Transport = redirect
	upd.downloader.httpClient.Transport = redirect

	_, err = upd.CheckAndDownload(context.Background())
	if err == nil {
		t.Error("expected error from verification failure")
	}

	// Files should be cleaned up
	entries, _ := os.ReadDir(stagingDir)
	if len(entries) != 0 {
		t.Errorf("expected staging dir to be empty after verification failure, got %d files", len(entries))
	}
}

func TestNewUpdater_EmptyOwnerRepo(t *testing.T) {
	_, err := NewUpdater(UpdaterConfig{
		Owner:        "",
		Repo:         "le_voile",
		PubKeyBase64: "dGVzdA==",
		StagingDir:   t.TempDir(),
	})
	if err == nil {
		t.Error("expected error for empty Owner")
	}

	_, err = NewUpdater(UpdaterConfig{
		Owner:        "velia-the-veil",
		Repo:         "",
		PubKeyBase64: "dGVzdA==",
		StagingDir:   t.TempDir(),
	})
	if err == nil {
		t.Error("expected error for empty Repo")
	}
}
