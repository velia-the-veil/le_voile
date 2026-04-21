package tunnel

import (
	"sync"
	"testing"
	"time"
)

func TestStateManager_InitialState(t *testing.T) {
	sm := NewStateManager()
	if got := sm.Get(); got != StateDisconnected {
		t.Errorf("initial state = %q, want %q", got, StateDisconnected)
	}
}

func TestStateManager_SetGet(t *testing.T) {
	tests := []struct {
		name  string
		state ConnState
	}{
		{"connecting", StateConnecting},
		{"connected", StateConnected},
		{"disconnected", StateDisconnected},
		{"failed", StateFailed},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			sm := NewStateManager()
			sm.Set(tt.state)
			if got := sm.Get(); got != tt.state {
				t.Errorf("Get() = %q, want %q", got, tt.state)
			}
		})
	}
}

func TestStateManager_Updates(t *testing.T) {
	sm := NewStateManager()
	ch := sm.Updates()

	sm.Set(StateConnecting)

	select {
	case got := <-ch:
		if got != StateConnecting {
			t.Errorf("update = %q, want %q", got, StateConnecting)
		}
	case <-time.After(100 * time.Millisecond):
		t.Fatal("timed out waiting for update")
	}
}

func TestStateManager_FailedTransition(t *testing.T) {
	// StateFailed must be accepted by the StateManager and emitted on the
	// updates channel like any other state.
	sm := NewStateManager()
	ch := sm.Updates()

	sm.Set(StateConnecting)
	<-ch // drain

	sm.Set(StateFailed)
	select {
	case got := <-ch:
		if got != StateFailed {
			t.Errorf("update = %q, want %q", got, StateFailed)
		}
	case <-time.After(100 * time.Millisecond):
		t.Fatal("timed out waiting for StateFailed update")
	}

	if got := sm.Get(); got != StateFailed {
		t.Errorf("Get() = %q, want %q", got, StateFailed)
	}

	// Reset path: StateFailed -> StateConnecting should succeed and emit.
	sm.Set(StateConnecting)
	select {
	case got := <-ch:
		if got != StateConnecting {
			t.Errorf("reset update = %q, want %q", got, StateConnecting)
		}
	case <-time.After(100 * time.Millisecond):
		t.Fatal("timed out waiting for StateConnecting update after reset")
	}
}

func TestStateManager_Concurrent(t *testing.T) {
	sm := NewStateManager()
	var wg sync.WaitGroup

	states := []ConnState{StateConnecting, StateConnected, StateDisconnected}

	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			sm.Set(states[idx%len(states)])
			_ = sm.Get()
		}(i)
	}

	wg.Wait()

	got := sm.Get()
	valid := got == StateConnecting || got == StateConnected || got == StateDisconnected
	if !valid {
		t.Errorf("final state = %q, not a valid ConnState", got)
	}
}

// TestStateManager_SetAfterClose locks in the 2026-04-21 fix: Set must not
// panic with "send on closed channel" when called after Close(). The real
// observation came from service shutdown — step 8 first called State().Close()
// and then Client.Disconnect() which internally Set(StateDisconnected),
// aborting the rest of shutdown() (including the WFP deactivate at step 8a)
// and leaving the host with no Internet. The internal current-state update
// MUST still happen so any Get() that follows sees the final state.
func TestStateManager_SetAfterClose(t *testing.T) {
	sm := NewStateManager()
	sm.Close()

	// Must not panic. Previously this was "panic: send on closed channel".
	sm.Set(StateDisconnected)

	if got := sm.Get(); got != StateDisconnected {
		t.Errorf("Get after Close+Set = %q, want %q", got, StateDisconnected)
	}
}

// TestStateManager_CloseIdempotent guards the second defensive change from
// the same 2026-04-21 pass: callers may double-Close in shutdown retry paths,
// and the second call must not re-close the already-closed channel (panic).
func TestStateManager_CloseIdempotent(t *testing.T) {
	sm := NewStateManager()
	sm.Close()
	sm.Close() // must not panic
}
