//go:build linux || darwin

package ipc

import (
	"os"
	"strings"
	"testing"
)

func TestSocketPath_Format(t *testing.T) {
	if !strings.HasPrefix(SocketPath, "/") {
		t.Errorf("SocketPath = %q, want absolute path starting with /", SocketPath)
	}
	if !strings.HasSuffix(SocketPath, ".sock") {
		t.Errorf("SocketPath = %q, want .sock suffix", SocketPath)
	}
	if SocketPath != "/tmp/levoile.sock" {
		t.Errorf("SocketPath = %q, want %q", SocketPath, "/tmp/levoile.sock")
	}
}

func TestNewPlatformListener_Unix_ReturnsNonNil(t *testing.T) {
	listener := NewPlatformListener()
	if listener == nil {
		t.Fatal("NewPlatformListener returned nil")
	}
}

func TestPlatformListener_Listen_CreatesSocket(t *testing.T) {
	// Clean up any existing socket first.
	os.Remove(SocketPath)

	pl := newPlatformListener()
	ln, err := pl.Listen()
	if err != nil {
		t.Fatalf("Listen failed: %v", err)
	}
	defer ln.Close()
	defer os.Remove(SocketPath)

	// Verify socket file exists.
	info, err := os.Stat(SocketPath)
	if err != nil {
		t.Fatalf("socket file does not exist: %v", err)
	}
	// On Linux/macOS, socket files have a special mode.
	if info.Mode()&os.ModeSocket == 0 {
		t.Errorf("expected socket file mode, got %v", info.Mode())
	}
}

func TestPlatformListener_DialPlatform(t *testing.T) {
	os.Remove(SocketPath)

	pl := newPlatformListener()
	ln, err := pl.Listen()
	if err != nil {
		t.Fatalf("Listen failed: %v", err)
	}
	defer ln.Close()
	defer os.Remove(SocketPath)

	// Accept in background.
	go func() {
		conn, err := ln.Accept()
		if err != nil {
			return
		}
		conn.Close()
	}()

	conn, err := dialPlatform()
	if err != nil {
		t.Fatalf("dialPlatform failed: %v", err)
	}
	conn.Close()
}

func TestPlatformListener_Cleanup_RemovesSocket(t *testing.T) {
	os.Remove(SocketPath)

	pl := newPlatformListener()
	ln, err := pl.Listen()
	if err != nil {
		t.Fatalf("Listen failed: %v", err)
	}
	ln.Close()

	// Socket file should exist.
	if _, err := os.Stat(SocketPath); os.IsNotExist(err) {
		t.Fatal("socket file should exist before cleanup")
	}

	err = pl.Cleanup()
	if err != nil {
		t.Errorf("Cleanup returned error: %v", err)
	}

	// Socket file should be gone.
	if _, err := os.Stat(SocketPath); !os.IsNotExist(err) {
		t.Error("socket file should not exist after cleanup")
	}
}

func TestPlatformListener_Listen_RemovesStaleSock(t *testing.T) {
	// Create a stale socket file.
	os.Remove(SocketPath)
	if err := os.WriteFile(SocketPath, []byte("stale"), 0644); err != nil {
		t.Fatalf("create stale file: %v", err)
	}

	pl := newPlatformListener()
	ln, err := pl.Listen()
	if err != nil {
		t.Fatalf("Listen should succeed even with stale socket: %v", err)
	}
	ln.Close()
	os.Remove(SocketPath)
}
