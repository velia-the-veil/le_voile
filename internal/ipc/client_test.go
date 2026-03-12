package ipc

import (
	"bufio"
	"context"
	"encoding/json"
	"net"
	"testing"
	"time"
)

// testDialClient creates a client that connects via TCP for testing
// (bypassing platform-specific dialPlatform).
func testDialClient(t *testing.T, addr string) *Client {
	t.Helper()
	conn, err := net.DialTimeout("tcp", addr, 2*time.Second)
	if err != nil {
		t.Fatalf("failed to connect: %v", err)
	}
	c := NewClient()
	c.conn = conn
	c.scanner = bufio.NewScanner(conn)
	c.encoder = json.NewEncoder(conn)
	return c
}

func TestClient_SendGetStatus(t *testing.T) {
	_, addr, cancel, errCh := startTestServer(t)
	defer cancel()

	client := testDialClient(t, addr)
	defer client.Close()

	resp, err := client.Send(Request{Action: ActionGetStatus})
	if err != nil {
		t.Fatalf("Send returned error: %v", err)
	}
	if resp.Status != StatusConnected {
		t.Errorf("status = %q, want %q", resp.Status, StatusConnected)
	}
	if resp.IP != "82.1.2.3" {
		t.Errorf("ip = %q, want %q", resp.IP, "82.1.2.3")
	}

	cancel()
	<-errCh
}

func TestClient_SendConnect(t *testing.T) {
	_, addr, cancel, errCh := startTestServer(t)
	defer cancel()

	client := testDialClient(t, addr)
	defer client.Close()

	resp, err := client.Send(Request{Action: ActionConnect})
	if err != nil {
		t.Fatalf("Send returned error: %v", err)
	}
	if resp.Status != StatusConnecting {
		t.Errorf("status = %q, want %q", resp.Status, StatusConnecting)
	}

	cancel()
	<-errCh
}

func TestClient_SendDisconnect(t *testing.T) {
	_, addr, cancel, errCh := startTestServer(t)
	defer cancel()

	client := testDialClient(t, addr)
	defer client.Close()

	resp, err := client.Send(Request{Action: ActionDisconnect})
	if err != nil {
		t.Fatalf("Send returned error: %v", err)
	}
	if resp.Status != StatusDisconnected {
		t.Errorf("status = %q, want %q", resp.Status, StatusDisconnected)
	}

	cancel()
	<-errCh
}

func TestClient_SendContext_Timeout(t *testing.T) {
	_, addr, cancel, errCh := startTestServer(t)
	defer cancel()

	client := testDialClient(t, addr)
	defer client.Close()

	// Very short timeout to trigger deadline exceeded.
	ctx, ctxCancel := context.WithTimeout(context.Background(), 1*time.Nanosecond)
	defer ctxCancel()
	time.Sleep(1 * time.Millisecond)

	_, err := client.SendContext(ctx, Request{Action: ActionGetStatus})
	if err == nil {
		t.Error("expected error from timed-out context, got nil")
	}

	cancel()
	<-errCh
}

func TestClient_NotConnected(t *testing.T) {
	client := NewClient()
	_, err := client.Send(Request{Action: ActionGetStatus})
	if err == nil {
		t.Error("expected error from unconnected client, got nil")
	}
}

func TestClient_Close_NilSafe(t *testing.T) {
	client := NewClient()
	if err := client.Close(); err != nil {
		t.Errorf("Close on unconnected client returned error: %v", err)
	}
}

func TestClient_MultipleRequests(t *testing.T) {
	_, addr, cancel, errCh := startTestServer(t)
	defer cancel()

	client := testDialClient(t, addr)
	defer client.Close()

	resp1, err := client.Send(Request{Action: ActionGetStatus})
	if err != nil {
		t.Fatalf("first Send returned error: %v", err)
	}
	if resp1.Status != StatusConnected {
		t.Errorf("first status = %q, want %q", resp1.Status, StatusConnected)
	}

	resp2, err := client.Send(Request{Action: ActionDisconnect})
	if err != nil {
		t.Fatalf("second Send returned error: %v", err)
	}
	if resp2.Status != StatusDisconnected {
		t.Errorf("second status = %q, want %q", resp2.Status, StatusDisconnected)
	}

	cancel()
	<-errCh
}
