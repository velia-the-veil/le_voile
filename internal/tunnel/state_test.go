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
