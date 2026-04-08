//go:build windows

package ipc

import (
	"strings"
	"testing"
)

func TestPipeName_Format(t *testing.T) {
	if !strings.HasPrefix(PipeName, `\\.\pipe\`) {
		t.Errorf("PipeName = %q, want prefix %q", PipeName, `\\.\pipe\`)
	}
	if PipeName != `\\.\pipe\levoile` {
		t.Errorf("PipeName = %q, want %q", PipeName, `\\.\pipe\levoile`)
	}
}

func TestNewPlatformListener_Windows_ReturnsNonNil(t *testing.T) {
	listener := NewPlatformListener()
	if listener == nil {
		t.Fatal("NewPlatformListener returned nil")
	}
}

func TestPlatformListener_Cleanup_NoOp(t *testing.T) {
	pl := newPlatformListener()
	err := pl.Cleanup()
	if err != nil {
		t.Errorf("Cleanup returned error: %v", err)
	}
}

func TestPlatformListener_Listen_AndDialPlatform(t *testing.T) {
	pl := newPlatformListener()
	ln, err := pl.Listen()
	if err != nil {
		t.Fatalf("Listen failed: %v", err)
	}
	defer ln.Close()

	// Verify we can dial the named pipe.
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
