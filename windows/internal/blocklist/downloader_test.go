//go:build windows

package blocklist

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestDownload_Success(t *testing.T) {
	expected := "0.0.0.0 ads.example.com\n0.0.0.0 tracker.io\n"
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(expected))
	}))
	defer srv.Close()

	client := &http.Client{}
	data, err := downloadFrom(context.Background(), client, srv.URL)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if string(data) != expected {
		t.Errorf("unexpected content: %q", string(data))
	}
}

func TestDownload_HTTPError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	defer srv.Close()

	client := &http.Client{}
	_, err := downloadFrom(context.Background(), client, srv.URL)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !strings.Contains(err.Error(), "blocklist: download:") {
		t.Errorf("error should contain 'blocklist: download:', got: %v", err)
	}
}

func TestDownload_ContextCancelled(t *testing.T) {
	ready := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		close(ready)
		// Block until client cancels.
		<-r.Context().Done()
	}))
	defer srv.Close()

	ctx, cancel := context.WithCancel(context.Background())
	client := &http.Client{}

	done := make(chan error, 1)
	go func() {
		_, err := downloadFrom(ctx, client, srv.URL)
		done <- err
	}()

	<-ready
	cancel()

	err := <-done
	if err == nil {
		t.Fatal("expected error after context cancellation, got nil")
	}
}
