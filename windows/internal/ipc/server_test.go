//go:build windows

package ipc

import (
	"bufio"
	"context"
	"encoding/json"
	"net"
	"testing"
	"time"
)

// testListener uses TCP for testing.
type testListener struct {
	ln net.Listener
}

func newTestListener(t *testing.T) (*testListener, string) {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("failed to create test listener: %v", err)
	}
	return &testListener{ln: ln}, ln.Addr().String()
}

func (tl *testListener) Listen() (net.Listener, error) {
	return tl.ln, nil
}

func (tl *testListener) Cleanup() error {
	return nil
}

func testHandler(req Request) Response {
	switch req.Action {
	case ActionGetStatus:
		return Response{Status: StatusConnected, IP: "82.1.2.3", Uptime: "1h30m"}
	case ActionConnect:
		return Response{Status: StatusConnecting}
	case ActionDisconnect:
		return Response{Status: StatusDisconnected}
	default:
		return Response{Status: StatusError, Error: "unknown_action"}
	}
}

// testConn wraps a connection with a persistent scanner for reading responses.
type testConn struct {
	conn    net.Conn
	scanner *bufio.Scanner
	encoder *json.Encoder
}

func dialTest(t *testing.T, addr string) *testConn {
	t.Helper()
	conn, err := net.DialTimeout("tcp", addr, 2*time.Second)
	if err != nil {
		t.Fatalf("failed to connect: %v", err)
	}
	conn.SetDeadline(time.Now().Add(5 * time.Second))
	return &testConn{
		conn:    conn,
		scanner: bufio.NewScanner(conn),
		encoder: json.NewEncoder(conn),
	}
}

func (tc *testConn) sendAndReceive(t *testing.T, req Request) Response {
	t.Helper()
	if err := tc.encoder.Encode(req); err != nil {
		t.Fatalf("failed to send request: %v", err)
	}
	if !tc.scanner.Scan() {
		t.Fatalf("no response received: %v", tc.scanner.Err())
	}
	var resp Response
	if err := json.Unmarshal(tc.scanner.Bytes(), &resp); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}
	return resp
}

func (tc *testConn) close() {
	tc.conn.Close()
}

func startTestServer(t *testing.T) (*Server, string, context.CancelFunc, chan error) {
	t.Helper()
	tl, addr := newTestListener(t)
	srv := NewServer(tl)
	srv.SetHandler(testHandler)

	ctx, cancel := context.WithCancel(context.Background())
	errCh := make(chan error, 1)
	go func() { errCh <- srv.Start(ctx) }()
	time.Sleep(50 * time.Millisecond)

	return srv, addr, cancel, errCh
}

func TestServer_HandleGetStatus(t *testing.T) {
	_, addr, cancel, errCh := startTestServer(t)
	defer cancel()

	tc := dialTest(t, addr)
	defer tc.close()

	resp := tc.sendAndReceive(t, Request{Action: ActionGetStatus})
	if resp.Status != StatusConnected {
		t.Errorf("status = %q, want %q", resp.Status, StatusConnected)
	}
	if resp.IP != "82.1.2.3" {
		t.Errorf("ip = %q, want %q", resp.IP, "82.1.2.3")
	}
	if resp.Uptime != "1h30m" {
		t.Errorf("uptime = %q, want %q", resp.Uptime, "1h30m")
	}

	cancel()
	<-errCh
}

func TestServer_HandleConnect(t *testing.T) {
	_, addr, cancel, errCh := startTestServer(t)
	defer cancel()

	tc := dialTest(t, addr)
	defer tc.close()

	resp := tc.sendAndReceive(t, Request{Action: ActionConnect})
	if resp.Status != StatusConnecting {
		t.Errorf("status = %q, want %q", resp.Status, StatusConnecting)
	}

	cancel()
	<-errCh
}

func TestServer_HandleDisconnect(t *testing.T) {
	_, addr, cancel, errCh := startTestServer(t)
	defer cancel()

	tc := dialTest(t, addr)
	defer tc.close()

	resp := tc.sendAndReceive(t, Request{Action: ActionDisconnect})
	if resp.Status != StatusDisconnected {
		t.Errorf("status = %q, want %q", resp.Status, StatusDisconnected)
	}

	cancel()
	<-errCh
}

func TestServer_InvalidJSON(t *testing.T) {
	_, addr, cancel, errCh := startTestServer(t)
	defer cancel()

	tc := dialTest(t, addr)
	defer tc.close()

	// Send invalid JSON.
	tc.conn.Write([]byte("not-json\n"))

	if !tc.scanner.Scan() {
		t.Fatalf("no response received: %v", tc.scanner.Err())
	}
	var resp Response
	if err := json.Unmarshal(tc.scanner.Bytes(), &resp); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}
	if resp.Status != StatusError {
		t.Errorf("status = %q, want %q", resp.Status, StatusError)
	}
	if resp.Error != "invalid_json" {
		t.Errorf("error = %q, want %q", resp.Error, "invalid_json")
	}

	cancel()
	<-errCh
}

func TestServer_UnknownAction(t *testing.T) {
	_, addr, cancel, errCh := startTestServer(t)
	defer cancel()

	tc := dialTest(t, addr)
	defer tc.close()

	resp := tc.sendAndReceive(t, Request{Action: "unknown"})
	if resp.Status != StatusError {
		t.Errorf("status = %q, want %q", resp.Status, StatusError)
	}
	if resp.Error != "unknown_action" {
		t.Errorf("error = %q, want %q", resp.Error, "unknown_action")
	}

	cancel()
	<-errCh
}

func TestServer_Stop(t *testing.T) {
	srv, _, cancel, errCh := startTestServer(t)
	defer cancel()

	srv.Stop()

	select {
	case err := <-errCh:
		if err != nil {
			t.Errorf("Start returned unexpected error: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("server did not stop within 2 seconds")
	}
}

func TestServer_MultipleMessages(t *testing.T) {
	_, addr, cancel, errCh := startTestServer(t)
	defer cancel()

	tc := dialTest(t, addr)
	defer tc.close()

	resp1 := tc.sendAndReceive(t, Request{Action: ActionGetStatus})
	if resp1.Status != StatusConnected {
		t.Errorf("first response status = %q, want %q", resp1.Status, StatusConnected)
	}

	resp2 := tc.sendAndReceive(t, Request{Action: ActionDisconnect})
	if resp2.Status != StatusDisconnected {
		t.Errorf("second response status = %q, want %q", resp2.Status, StatusDisconnected)
	}

	cancel()
	<-errCh
}
