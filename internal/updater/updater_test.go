package updater

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"fmt"
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
	sigBase64 := base64.StdEncoding.EncodeToString(sig)

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
			w.Write([]byte(sigBase64))
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
			w.Write([]byte(base64.StdEncoding.EncodeToString(make([]byte, 64))))
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
