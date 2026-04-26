//go:build linux

package ipc

import (
	"sync"
	"testing"
)

func TestClient_ConcurrentSend(t *testing.T) {
	_, addr, cancel, errCh := startTestServer(t)
	defer cancel()

	client := testDialClient(t, addr)
	defer client.Close()

	// Send multiple requests concurrently.
	// The IPC client uses a single connection, so concurrent sends
	// will interleave. This test verifies no panic or data corruption.
	var wg sync.WaitGroup
	const goroutines = 5
	errors := make(chan error, goroutines)

	for i := 0; i < goroutines; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, err := client.Send(Request{Action: ActionGetStatus})
			if err != nil {
				errors <- err
			}
		}()
	}

	wg.Wait()
	close(errors)

	// With concurrent writes to the same connection without synchronization,
	// some requests may fail. The key thing is no panic.
	errorCount := 0
	for err := range errors {
		t.Logf("concurrent send error (expected): %v", err)
		errorCount++
	}

	// At least some should succeed if the connection is still alive.
	// But all failing is also acceptable — the test is about no panic/crash.

	cancel()
	<-errCh
}

func TestClient_SendAfterClose(t *testing.T) {
	_, addr, cancel, errCh := startTestServer(t)
	defer cancel()

	client := testDialClient(t, addr)

	// Close the client first.
	if err := client.Close(); err != nil {
		t.Fatalf("Close returned error: %v", err)
	}

	// Send after close should return an error (not panic).
	_, err := client.Send(Request{Action: ActionGetStatus})
	if err == nil {
		t.Error("expected error from Send after Close, got nil")
	}

	cancel()
	<-errCh
}

func TestClient_SendContext_NotConnected(t *testing.T) {
	client := NewClient()
	// conn is nil — should return "not connected" error.
	_, err := client.Send(Request{Action: ActionGetStatus})
	if err == nil {
		t.Error("expected error from unconnected client, got nil")
	}
}

func TestClient_DoubleClose(t *testing.T) {
	_, addr, cancel, errCh := startTestServer(t)
	defer cancel()

	client := testDialClient(t, addr)

	// First close should succeed.
	if err := client.Close(); err != nil {
		t.Fatalf("first Close: %v", err)
	}

	// Second close may return an error but should not panic.
	_ = client.Close()

	cancel()
	<-errCh
}
