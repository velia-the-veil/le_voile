//go:build windows

package ipchandler

import (
	"testing"
	"time"

	"github.com/velia-the-veil/le_voile/windows/internal/ipc"
	svc "github.com/velia-the-veil/le_voile/windows/internal/service"
)

func TestHandleConnect_NilReconnector(t *testing.T) {
	// Program has no reconnector set — handleConnect should still work.
	prg := svc.NewProgram(svc.Config{
		RelayDomain: "test.dev",
		RelayPubKey: "dGVzdA==",
	})

	// TunnelClient is nil, so we get "service_not_ready".
	resp := Handle(prg, ipc.Request{Action: ipc.ActionConnect}, Options{})
	if resp.Status != ipc.StatusError || resp.Error != "service_not_ready" {
		t.Errorf("expected error/service_not_ready, got %q/%q", resp.Status, resp.Error)
	}
}

func TestHandleDisconnect_NilReconnector(t *testing.T) {
	// Program has no reconnector — handleDisconnect should still work.
	prg := svc.NewProgram(svc.Config{
		RelayDomain: "test.dev",
		RelayPubKey: "dGVzdA==",
	})

	// TunnelClient is nil, so we get StatusDisconnected.
	resp := Handle(prg, ipc.Request{Action: ipc.ActionDisconnect}, Options{})
	if resp.Status != ipc.StatusDisconnected {
		t.Errorf("expected disconnected, got %q", resp.Status)
	}
}

func TestFormatUptime_ZeroDuration(t *testing.T) {
	got := FormatUptime(0)
	want := "0m00s"
	if got != want {
		t.Errorf("FormatUptime(0) = %q, want %q", got, want)
	}
}

func TestFormatUptime_VeryLargeDuration(t *testing.T) {
	// 100 hours
	d := 100*time.Hour + 30*time.Minute + 45*time.Second
	got := FormatUptime(d)
	want := "100h30m"
	if got != want {
		t.Errorf("FormatUptime(%v) = %q, want %q", d, got, want)
	}
}

func TestFormatUptime_ExactHour(t *testing.T) {
	got := FormatUptime(1 * time.Hour)
	want := "1h00m"
	if got != want {
		t.Errorf("FormatUptime(1h) = %q, want %q", got, want)
	}
}

func TestFormatUptime_ExactMinute(t *testing.T) {
	got := FormatUptime(1 * time.Minute)
	want := "1m00s"
	if got != want {
		t.Errorf("FormatUptime(1m) = %q, want %q", got, want)
	}
}

func TestFormatUptime_SubSecond(t *testing.T) {
	// Sub-second should truncate to 0.
	got := FormatUptime(500 * time.Millisecond)
	want := "0m00s"
	if got != want {
		t.Errorf("FormatUptime(500ms) = %q, want %q", got, want)
	}
}

func TestFormatUptime_24Hours(t *testing.T) {
	got := FormatUptime(24*time.Hour + 1*time.Minute)
	want := "24h01m"
	if got != want {
		t.Errorf("FormatUptime(24h1m) = %q, want %q", got, want)
	}
}

func TestHandle_GetStatus_NilTunnel_ReturnsDisconnected(t *testing.T) {
	prg := newTestProgram()
	resp := Handle(prg, ipc.Request{Action: ipc.ActionGetStatus}, Options{})
	if resp.Status != ipc.StatusDisconnected {
		t.Errorf("expected disconnected, got %q", resp.Status)
	}
	// When TunnelClient is nil, uptime and IP should be empty.
	if resp.Uptime != "" {
		t.Errorf("expected empty uptime, got %q", resp.Uptime)
	}
}
