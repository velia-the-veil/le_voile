// Package tunnel manages the encrypted QUIC tunnel connection.
package tunnel

import "sync"

// ConnState represents the current state of the tunnel connection.
type ConnState string

const (
	StateConnected    ConnState = "connected"
	StateConnecting   ConnState = "connecting"
	StateDisconnected ConnState = "disconnected"
)

// StateManager provides thread-safe tunnel state management with change notifications.
type StateManager struct {
	mu      sync.RWMutex
	current ConnState
	updates chan ConnState
}

// NewStateManager creates a StateManager with initial state disconnected.
// The updates channel is buffered to reduce the chance of dropped state
// transitions between rapid state changes.
func NewStateManager() *StateManager {
	return &StateManager{
		current: StateDisconnected,
		updates: make(chan ConnState, 4),
	}
}

// Set updates the current state and sends a non-blocking notification on the updates channel.
// The send is done under the lock to prevent concurrent Set calls from racing on the
// drain-and-retry, which could drop critical transitions like StateDisconnected.
func (sm *StateManager) Set(state ConnState) {
	sm.mu.Lock()
	defer sm.mu.Unlock()
	sm.current = state

	select {
	case sm.updates <- state:
	default:
		// Channel full — drain the oldest entry and retry.
		select {
		case <-sm.updates:
		default:
		}
		sm.updates <- state
	}
}

// Get returns the current connection state.
func (sm *StateManager) Get() ConnState {
	sm.mu.RLock()
	defer sm.mu.RUnlock()
	return sm.current
}

// Updates returns a read-only channel that receives state change notifications.
func (sm *StateManager) Updates() <-chan ConnState {
	return sm.updates
}

// Close closes the updates channel so that range loops over Updates() terminate.
func (sm *StateManager) Close() {
	close(sm.updates)
}
