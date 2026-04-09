package ui

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"

	"fyne.io/systray"
	"github.com/velia-the-veil/le_voile/internal/ipc"
)

// trackingIPCClient counts SendContext calls and records actions.
type trackingIPCClient struct {
	mu       sync.Mutex
	calls    []string
	sendErr  error
	resp     ipc.Response
	closeN   atomic.Int32
}

func (m *trackingIPCClient) Connect() error { return nil }
func (m *trackingIPCClient) Close() error {
	m.closeN.Add(1)
	return nil
}
func (m *trackingIPCClient) SendContext(_ context.Context, req ipc.Request) (ipc.Response, error) {
	m.mu.Lock()
	m.calls = append(m.calls, req.Action)
	m.mu.Unlock()
	return m.resp, m.sendErr
}

func (m *trackingIPCClient) getCalls() []string {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([]string, len(m.calls))
	copy(out, m.calls)
	return out
}

// TestShutdown_Idempotent verifies that calling shutdown() concurrently
// from multiple goroutines results in exactly one execution (sync.Once).
func TestShutdown_Idempotent(t *testing.T) {
	client := &trackingIPCClient{}
	u := &UI{
		api:      &mockSystrayAPI{},
		menuAPI:  &mockSystrayMenuAPI{},
		client:   NewSafeIPCClient(client),
		sysProxy: NewSysProxy("test.example"),
	}

	var wg sync.WaitGroup
	const goroutines = 10
	wg.Add(goroutines)
	for i := 0; i < goroutines; i++ {
		go func() {
			defer wg.Done()
			u.shutdown()
		}()
	}
	wg.Wait()

	// ActionQuit should have been sent exactly once.
	calls := client.getCalls()
	quitCount := 0
	for _, c := range calls {
		if c == ipc.ActionQuit {
			quitCount++
		}
	}
	if quitCount != 1 {
		t.Errorf("expected exactly 1 ActionQuit call, got %d (calls: %v)", quitCount, calls)
	}

	// shutdownInProgress must be true.
	if !u.shutdownInProgress.Load() {
		t.Error("expected shutdownInProgress=true after shutdown")
	}

	// Close should have been called exactly once.
	if n := client.closeN.Load(); n != 1 {
		t.Errorf("expected 1 Close() call, got %d", n)
	}
}

// TestShutdown_CallsWebviewTerminate verifies that shutdown() calls the
// webview terminate callback exactly once when a webview is open.
func TestShutdown_CallsWebviewTerminate(t *testing.T) {
	client := &trackingIPCClient{}
	var terminateCalls atomic.Int32

	u := &UI{
		api:     &mockSystrayAPI{},
		menuAPI: &mockSystrayMenuAPI{},
		client:  NewSafeIPCClient(client),
	}
	// Simulate an open webview by setting the terminate callback.
	u.webviewTerminate = func() {
		terminateCalls.Add(1)
	}

	u.shutdown()

	if n := terminateCalls.Load(); n != 1 {
		t.Errorf("expected webviewTerminate called 1 time, got %d", n)
	}

	// After shutdown, webviewTerminate must be nil.
	u.mu.Lock()
	isNil := u.webviewTerminate == nil
	u.mu.Unlock()
	if !isNil {
		t.Error("expected webviewTerminate=nil after shutdown")
	}
}

// TestShutdown_NoWebview verifies shutdown works when no webview is open.
func TestShutdown_NoWebview(t *testing.T) {
	client := &trackingIPCClient{}
	u := &UI{
		api:     &mockSystrayAPI{},
		menuAPI: &mockSystrayMenuAPI{},
		client:  NewSafeIPCClient(client),
	}

	// No webviewTerminate set — should not panic.
	u.shutdown()

	calls := client.getCalls()
	quitCount := 0
	for _, c := range calls {
		if c == ipc.ActionQuit {
			quitCount++
		}
	}
	if quitCount != 1 {
		t.Errorf("expected 1 ActionQuit, got %d", quitCount)
	}
}

// mockSystrayMenuAPI records menu calls for testing.
type mockSystrayMenuAPI struct {
	quitCalled atomic.Bool
}

func (m *mockSystrayMenuAPI) AddMenuItem(_, _ string) *systray.MenuItem { return nil }
func (m *mockSystrayMenuAPI) AddSeparator()                     {}
func (m *mockSystrayMenuAPI) Quit()                             { m.quitCalled.Store(true) }
