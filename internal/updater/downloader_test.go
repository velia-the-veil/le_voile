package updater

import (
	"bytes"
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"runtime"
	"strings"
	"testing"
	"time"
)

func TestRateLimitedReader(t *testing.T) {
	data := make([]byte, 10000)
	for i := range data {
		data[i] = byte(i % 256)
	}

	// 5000 bytes/sec limit — reading 10000 bytes should take ~1 second (burst covers first 5000)
	reader := newRateLimitedReader(context.Background(), bytes.NewReader(data), 5000)

	start := time.Now()
	buf := make([]byte, 1024) // Small buffer to force multiple reads
	total := 0
	for {
		n, err := reader.Read(buf)
		total += n
		if err != nil {
			break
		}
	}
	elapsed := time.Since(start)

	if total != len(data) {
		t.Errorf("read %d bytes, want %d", total, len(data))
	}

	// With 5000 bytes/sec, 10000 bytes, burst=5000: first 5000 instant, remaining ~1s
	// Allow 20% tolerance: at least 800ms
	if elapsed < 800*time.Millisecond {
		t.Errorf("rate limiting too fast: took %v, expected ~1s", elapsed)
	}
}

func TestRateLimitedReader_ContextCancel(t *testing.T) {
	data := make([]byte, 100000)
	ctx, cancel := context.WithCancel(context.Background())

	reader := newRateLimitedReader(ctx, bytes.NewReader(data), 1000)

	// Cancel immediately
	cancel()

	buf := make([]byte, 10000)
	_, err := reader.Read(buf)
	// First read might succeed (burst), but WaitN should fail
	if err == nil {
		_, err = reader.Read(buf)
	}
	if err == nil {
		t.Error("expected error after context cancellation")
	}
}

func testServerRedirectTransport(srv *httptest.Server) http.RoundTripper {
	origTransport := srv.Client().Transport
	srvURL, _ := url.Parse(srv.URL)
	return roundTripFunc(func(req *http.Request) (*http.Response, error) {
		redirected := *req
		redirected.URL = &url.URL{
			Scheme: srvURL.Scheme,
			Host:   srvURL.Host,
			Path:   req.URL.Path,
		}
		return origTransport.RoundTrip(&redirected)
	})
}

func TestDownloader_Download_Success(t *testing.T) {
	content := []byte("binary content here")

	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write(content)
	}))
	defer srv.Close()

	stagingDir := t.TempDir()
	dl := &Downloader{
		httpClient: srv.Client(),
		rateLimit:  defaultRateLimitBytesPerSec,
		stagingDir: stagingDir,
	}
	dl.httpClient.Transport = testServerRedirectTransport(srv)

	path, err := dl.Download(context.Background(), "https://github.com/velia-the-veil/le_voile/releases/download/v1.0.0/testfile.exe")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read downloaded file: %v", err)
	}
	if !bytes.Equal(got, content) {
		t.Errorf("content mismatch: got %q, want %q", got, content)
	}
}

func TestDownloader_Download_ContextCancel(t *testing.T) {
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(2 * time.Second)
		w.Write([]byte("data"))
	}))
	defer srv.Close()

	stagingDir := t.TempDir()
	dl := &Downloader{
		httpClient: srv.Client(),
		rateLimit:  defaultRateLimitBytesPerSec,
		stagingDir: stagingDir,
	}
	dl.httpClient.Transport = testServerRedirectTransport(srv)

	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	_, err := dl.Download(ctx, "https://github.com/velia-the-veil/le_voile/releases/download/v1.0.0/testfile.exe")
	if err == nil {
		t.Error("expected error from cancelled context")
	}

	// Verify no .tmp file left behind
	entries, _ := os.ReadDir(stagingDir)
	for _, e := range entries {
		if strings.HasSuffix(e.Name(), ".tmp") {
			t.Errorf("leftover .tmp file: %s", e.Name())
		}
	}
}

func TestDownloader_DownloadRelease(t *testing.T) {
	expectedBinary := fmt.Sprintf("le_voile_%s_%s", runtime.GOOS, runtime.GOARCH)
	if runtime.GOOS == "windows" {
		expectedBinary += ".exe"
	}

	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		path := r.URL.Path
		switch {
		case strings.HasSuffix(path, expectedBinary):
			w.Write([]byte("binary"))
		case strings.HasSuffix(path, "checksums.txt.sig"):
			w.Write([]byte("signature data"))
		case strings.HasSuffix(path, "checksums.txt"):
			w.Write([]byte("checksum data"))
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	stagingDir := t.TempDir()
	dl := &Downloader{
		httpClient: srv.Client(),
		rateLimit:  defaultRateLimitBytesPerSec,
		stagingDir: stagingDir,
	}
	dl.httpClient.Transport = testServerRedirectTransport(srv)

	baseURL := "https://github.com/velia-the-veil/le_voile/releases/download/v1.0.0"
	release := &ReleaseInfo{
		Version:          "1.0.0",
		DownloadURL:      baseURL + "/" + expectedBinary,
		ChecksumURL:  baseURL + "/checksums.txt",
		SignatureURL: baseURL + "/checksums.txt.sig",
	}

	staged, err := dl.DownloadRelease(context.Background(), release)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if staged.Version != "1.0.0" {
		t.Errorf("Version = %q, want %q", staged.Version, "1.0.0")
	}

	for _, path := range []string{staged.BinaryPath, staged.ChecksumPath, staged.SignaturePath} {
		if _, err := os.Stat(path); err != nil {
			t.Errorf("file missing: %s", path)
		}
	}
}

func TestValidateDownloadURL(t *testing.T) {
	tests := []struct {
		name    string
		url     string
		wantErr bool
	}{
		{"valid", "https://github.com/velia-the-veil/le_voile/releases/download/v1.0.0/file.exe", false},
		{"http not allowed", "http://github.com/velia-the-veil/le_voile/releases/download/v1.0.0/file.exe", true},
		{"wrong host", "https://evil.com/velia-the-veil/le_voile/releases/download/v1.0.0/file.exe", true},
		{"wrong path", "https://github.com/evil/repo/releases/download/v1.0.0/file.exe", true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateDownloadURL(tt.url)
			if (err != nil) != tt.wantErr {
				t.Errorf("validateDownloadURL(%q) error = %v, wantErr %v", tt.url, err, tt.wantErr)
			}
		})
	}
}
