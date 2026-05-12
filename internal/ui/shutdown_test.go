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
// Story 5.8: the shutdown sequence sends ActionUIDisconnect (notification),
// not ActionQuit (full service stop). AC4 (idempotence) must hold.
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

	// ActionUIDisconnect should have been sent exactly once. ActionQuit must
	// NEVER be sent by the UI shutdown path — see TestShutdown_DoesNotSendActionQuit.
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
	if disconnectCount != 1 {
		t.Errorf("expected exactly 1 ActionUIDisconnect call, got %d (calls: %v)", disconnectCount, calls)
	}
	if quitCount != 0 {
		t.Errorf("Story 5.8 AC1/AC3: UI shutdown MUST NOT send ActionQuit (would stop the service); got %d (calls: %v)", quitCount, calls)
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
	disconnectCount := 0
	for _, c := range calls {
		if c == ipc.ActionUIDisconnect {
			disconnectCount++
		}
	}
	if disconnectCount != 1 {
		t.Errorf("expected 1 ActionUIDisconnect, got %d", disconnectCount)
	}
}

// TestShutdown_DoesNotSendActionQuit explicitly guards Story 5.8 AC1/AC3:
// the UI shutdown sequence MUST NOT send ActionQuit under any path, because
// ActionQuit triggers a full service stop (tunnel down, kill switch down).
// Regression test for the pre-5.8 behaviour where "Quitter" killed the VPN.
func TestShutdown_DoesNotSendActionQuit(t *testing.T) {
	client := &trackingIPCClient{}
	u := &UI{
		api:      &mockSystrayAPI{},
		menuAPI:  &mockSystrayMenuAPI{},
		client:   NewSafeIPCClient(client),
		sysProxy: NewSysProxy("test.example"),
	}

	u.shutdown()

	for _, c := range client.getCalls() {
		if c == ipc.ActionQuit {
			t.Fatalf("Story 5.8 regression: UI shutdown sent ActionQuit — this would stop the service and drop the tunnel. Only ActionUIDisconnect is allowed. Calls: %v", client.getCalls())
		}
	}
}

// TestShutdown_RelaunchableState guards Story 5.8 AC4: after shutdown the UI
// must leave no state that would prevent a fresh UI instance from starting
// and reconnecting to the still-live service. We assert the observable
// side effects that matter for a clean relaunch:
//
//  1. shutdownInProgress=true (further IPC errors won't trigger orphan recovery)
//  2. the IPC client is Close()d exactly once (no leaked connections)
//  3. cancel() was invoked (polling goroutine will exit)
//  4. exactly one ActionUIDisconnect was sent (idempotence + service-lifecycle untouched)
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
	if disconnects != 1 {
		t.Errorf("ActionUIDisconnect sent %d times, want 1", disconnects)
	}
	if quits != 0 {
		t.Errorf("ActionQuit sent %d times, want 0 (AC1/AC3)", quits)
	}
}

// mockSystrayMenuAPI records menu calls for testing.
type mockSystrayMenuAPI struct {
	quitCalled atomic.Bool
}

func (m *mockSystrayMenuAPI) AddMenuItem(_, _ string) *systray.MenuItem { return nil }
func (m *mockSystrayMenuAPI) AddSeparator()                     {}
func (m *mockSystrayMenuAPI) Quit()                             { m.quitCalled.Store(true) }
