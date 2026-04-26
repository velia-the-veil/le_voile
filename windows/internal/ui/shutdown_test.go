//go:build windows

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
// Per the 2026-04-20 design decision the shutdown path sends ActionQuit
// so the service also stops and restores the host network config. AC4
// (idempotence) must hold regardless.
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

	// ActionQuit should have been sent exactly once. ActionUIDisconnect is
	// never sent from the shutdown path anymore — see TestShutdown_SendsActionQuit.
	calls := client.getCalls()
	disconnectCount := 0
	quitCount := 0
	for _, c := range calls {
		switch c {
		case ipc.ActionUIDisconnect:
			disconnectCount++
		case ipc.ActionQuit:
			quitCount++
		}
	}
	if quitCount != 1 {
		t.Errorf("expected exactly 1 ActionQuit call, got %d (calls: %v)", quitCount, calls)
	}
	if disconnectCount != 0 {
		t.Errorf("UI shutdown must no longer send ActionUIDisconnect; got %d (calls: %v)", disconnectCount, calls)
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

// TestShutdown_SendsActionQuit locks in the 2026-04-20 design decision:
// ✕ and tray-"Quitter" trigger a full service stop so the kill switch /
// firewall / routing / TUN are restored and the host's internet comes back.
func TestShutdown_SendsActionQuit(t *testing.T) {
	client := &trackingIPCClient{}
	u := &UI{
		api:      &mockSystrayAPI{},
		menuAPI:  &mockSystrayMenuAPI{},
		client:   NewSafeIPCClient(client),
		sysProxy: NewSysProxy("test.example"),
	}

	u.shutdown()

	sawQuit := false
	for _, c := range client.getCalls() {
		if c == ipc.ActionQuit {
			sawQuit = true
			break
		}
	}
	if !sawQuit {
		t.Fatalf("shutdown must send ActionQuit so the service tears down and restores config; calls: %v", client.getCalls())
	}
}

// TestShutdown_RelaunchableState asserts the observable side effects that
// matter for a clean relaunch after a full quit:
//
//  1. shutdownInProgress=true (further IPC errors won't trigger orphan recovery)
//  2. the IPC client is Close()d exactly once (no leaked connections)
//  3. cancel() was invoked (polling goroutine will exit)
//  4. exactly one ActionQuit was sent (so the service actually stops)
//
// Pairs with singleton_linux_test.go TestAcquireSingleton_ReacquireAfterRelease
// which covers the OS-level lock release/reacquire cycle.
func TestShutdown_RelaunchableState(t *testing.T) {
	client := &trackingIPCClient{}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	var cancelCount atomic.Int32
	u := &UI{
		api:      &mockSystrayAPI{},
		menuAPI:  &mockSystrayMenuAPI{},
		client:   NewSafeIPCClient(client),
		sysProxy: NewSysProxy("test.example"),
		cancel: func() {
			cancelCount.Add(1)
			cancel()
		},
	}
	_ = ctx

	u.shutdown()

	if !u.shutdownInProgress.Load() {
		t.Error("shutdownInProgress=false — orphan-recovery guard not armed; relaunched UI could double-restore")
	}
	if n := client.closeN.Load(); n != 1 {
		t.Errorf("client.Close called %d times, want 1 (leaked IPC connection breaks relaunch)", n)
	}
	if n := cancelCount.Load(); n != 1 {
		t.Errorf("cancel() called %d times, want 1 (polling goroutine still running after shutdown)", n)
	}
	disconnects := 0
	quits := 0
	for _, c := range client.getCalls() {
		switch c {
		case ipc.ActionUIDisconnect:
			disconnects++
		case ipc.ActionQuit:
			quits++
		}
	}
	if quits != 1 {
		t.Errorf("ActionQuit sent %d times, want 1", quits)
	}
	if disconnects != 0 {
		t.Errorf("ActionUIDisconnect sent %d times, want 0 (superseded by ActionQuit)", disconnects)
	}
}

// mockSystrayMenuAPI records menu calls for testing.
type mockSystrayMenuAPI struct {
	quitCalled atomic.Bool
}

func (m *mockSystrayMenuAPI) AddMenuItem(_, _ string) *systray.MenuItem { return nil }
func (m *mockSystrayMenuAPI) AddSeparator()                     {}
func (m *mockSystrayMenuAPI) Quit()                             { m.quitCalled.Store(true) }
