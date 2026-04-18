package tunnel

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"time"
)

// Sentinel errors for reconnection operations.
var (
	ErrReconnectInProgress = errors.New("tunnel: reconnect already in progress")
	ErrReconnectStopped    = errors.New("tunnel: reconnect stopped")
)

// Reconnection backoff constants.
//
// InitialBackoff starts at 100ms to satisfy NFR12 (reconnect initiation < 1s
// after loss) and grows exponentially up to MaxBackoff (30s plafond).
// After CircuitBreakerThreshold consecutive failures on the current relay,
// the Reconnector trips the circuit breaker: it stops retrying, transitions
// the tunnel to StateFailed, keeps the kill switch active, and invokes the
// optional hook provided via WithCircuitBreakerHook.
//
// If WithFailoverFn is provided, failover is attempted once after
// MaxRetriesBeforeFailover failures (before the circuit breaker trips).
// If failover fails, normal backoff resumes on the current relay and the
// circuit breaker still trips at CircuitBreakerThreshold.
const (
	InitialBackoff           = 100 * time.Millisecond
	BackoffFactor            = 2
	MaxBackoff               = 30 * time.Second
	MaxRetriesBeforeFailover = 3
	CircuitBreakerThreshold  = 5
)

// Enforce the invariant that the circuit breaker fires strictly AFTER a
// failover attempt has been made. A mis-edit that sets CircuitBreakerThreshold
// <= MaxRetriesBeforeFailover would silently disable failover — panic at init
// rather than surprise the operator at runtime.
func init() {
	if CircuitBreakerThreshold <= MaxRetriesBeforeFailover {
		panic("tunnel: CircuitBreakerThreshold must be > MaxRetriesBeforeFailover")
	}
}

// KillSwitchController is the interface the Reconnector uses to control the kill switch.
type KillSwitchController interface {
	Activate(ctx context.Context) error
	Deactivate(ctx context.Context) error
	IsActive() bool
}

// ConnectFunc is a function that attempts to establish the tunnel connection.
type ConnectFunc func(ctx context.Context) error

// ReconnectorOption configures a Reconnector.
type ReconnectorOption func(*Reconnector)

// WithDisconnectFn sets a function called before each reconnection cycle to
// tear down the old QUIC transport and prepare a fresh one. Without this,
// Connect() reuses a potentially dead HTTP/3 transport after auto-disconnect
// triggered by consecutive DoH failures.
func WithDisconnectFn(fn func() error) ReconnectorOption {
	return func(r *Reconnector) {
		r.disconnectFn = fn
	}
}

// WithFailoverFn sets a failover function to call after MaxRetriesBeforeFailover
// consecutive connection failures on the current relay. Tried once only; on
// failure, the Reconnector continues backoff on the current relay until the
// circuit breaker trips at CircuitBreakerThreshold.
func WithFailoverFn(fn func(ctx context.Context) error) ReconnectorOption {
	return func(r *Reconnector) {
		r.failoverFn = fn
	}
}

// WithCircuitBreakerHook registers a callback invoked exactly once when the
// circuit breaker trips (CircuitBreakerThreshold consecutive failures). The
// hook runs before the Reconnector returns control; it is responsible for
// propagating the StateFailed transition and emitting any user-facing alert.
// The kill switch remains active — the hook must NOT deactivate it.
func WithCircuitBreakerHook(fn func(ctx context.Context)) ReconnectorOption {
	return func(r *Reconnector) {
		r.circuitBreakerHook = fn
	}
}

// WithReconnectSuccessHook registers a callback invoked after a successful
// reconnect cycle (after kill switch deactivation). Used by the service to
// auto-restore the OS-level firewall when degraded mode (Story 5.9) was active
// before disconnect. The hook runs under a recover() guard so a panic does not
// take down the reconnect loop. Hook receives the same ctx as handleDisconnect.
func WithReconnectSuccessHook(fn func(ctx context.Context)) ReconnectorOption {
	return func(r *Reconnector) {
		r.reconnectSuccessHook = fn
	}
}

// Reconnector monitors tunnel state changes and automatically reconnects
// with exponential backoff when the connection is lost. It coordinates
// with a KillSwitch that remains active for the full duration of the
// reconnection attempt (including after circuit breaker trip).
type Reconnector struct {
	mu           sync.Mutex
	reconnecting bool
	cancel       context.CancelFunc
	done         chan struct{}

	// failed is set when the circuit breaker has tripped. While true, the
	// Reconnector ignores further StateDisconnected notifications until
	// Reset() is called (typically by a user-initiated Connect via IPC).
	failed atomic.Bool

	connectFn            ConnectFunc
	disconnectFn         func() error
	killSwitch           KillSwitchController
	updates              <-chan ConnState
	failoverFn           func(ctx context.Context) error
	circuitBreakerHook   func(ctx context.Context)
	reconnectSuccessHook func(ctx context.Context)
}

// NewReconnector creates a Reconnector that listens for state changes on
// the updates channel and uses connectFn to re-establish the connection.
func NewReconnector(updates <-chan ConnState, connectFn ConnectFunc, ks KillSwitchController, opts ...ReconnectorOption) *Reconnector {
	r := &Reconnector{
		connectFn:  connectFn,
		killSwitch: ks,
		updates:    updates,
	}
	for _, opt := range opts {
		opt(r)
	}
	return r
}

// Start begins listening for tunnel state changes. When a disconnected
// state is received, it activates the kill switch and starts reconnection
// with exponential backoff. On successful reconnection, the kill switch
// is deactivated.
//
// StateDisconnected notifications are IGNORED while the circuit breaker
// has tripped (Failed() == true). The caller must invoke Reset() — usually
// via a user-initiated Connect through IPC — before auto-reconnect resumes.
//
// Start blocks until ctx is cancelled or Stop is called.
func (r *Reconnector) Start(ctx context.Context) error {
	r.mu.Lock()
	if r.cancel != nil {
		r.mu.Unlock()
		return ErrReconnectInProgress
	}

	ctx, r.cancel = context.WithCancel(ctx)
	r.done = make(chan struct{})
	r.mu.Unlock()

	defer func() {
		r.mu.Lock()
		r.cancel = nil
		r.reconnecting = false
		close(r.done)
		r.mu.Unlock()
	}()

	for {
		select {
		case <-ctx.Done():
			return nil
		case state, ok := <-r.updates:
			if !ok {
				return nil
			}
			if state == StateDisconnected && !r.failed.Load() {
				r.handleDisconnect(ctx)
			}
		}
	}
}

// Stop halts the reconnection loop and waits for it to finish.
func (r *Reconnector) Stop() {
	r.mu.Lock()
	cancel := r.cancel
	done := r.done
	r.mu.Unlock()

	if cancel != nil {
		cancel()
	}
	if done != nil {
		<-done
	}
}

// Reset clears the circuit-breaker "failed" flag so that subsequent
// StateDisconnected notifications resume the reconnect loop from
// InitialBackoff. Call this from a user-initiated reconnect path
// (e.g., IPC Connect) after the user has acknowledged the failure.
func (r *Reconnector) Reset() {
	r.failed.Store(false)
}

// Failed reports whether the circuit breaker has tripped and not yet been
// reset. Used by IPC handlers to surface the failed state to the UI.
func (r *Reconnector) Failed() bool {
	return r.failed.Load()
}

// handleDisconnect activates the kill switch and attempts reconnection
// with exponential backoff. After MaxRetriesBeforeFailover consecutive
// failures, triggers failover once if configured. After
// CircuitBreakerThreshold consecutive failures, trips the circuit breaker:
// the loop exits, the kill switch stays active, and the hook is invoked.
func (r *Reconnector) handleDisconnect(ctx context.Context) {
	r.mu.Lock()
	if r.reconnecting {
		r.mu.Unlock()
		return
	}
	r.reconnecting = true
	r.mu.Unlock()

	defer func() {
		r.mu.Lock()
		r.reconnecting = false
		r.mu.Unlock()
	}()

	// Tear down the old QUIC transport so Connect() gets a fresh one.
	// Without this, auto-disconnect (consecutive DoH failures) leaves a dead
	// transport that Connect() would reuse, causing an infinite cycle.
	if r.disconnectFn != nil {
		r.disconnectFn()
	}

	// Activate kill switch — block DNS during reconnection (NFR5/NFR15).
	// Retry once on failure to maximize leak protection.
	if err := r.killSwitch.Activate(ctx); err != nil {
		select {
		case <-ctx.Done():
			return
		case <-time.After(10 * time.Millisecond):
		}
		r.killSwitch.Activate(ctx)
	}

	backoff := InitialBackoff
	retries := 0
	failoverAttempted := false

	for {
		select {
		case <-ctx.Done():
			return
		case <-time.After(backoff):
		}

		if err := r.connectFn(ctx); err != nil {
			if ctx.Err() != nil {
				return
			}
			retries++

			// Attempt failover ONCE after MaxRetriesBeforeFailover consecutive
			// failures. If failover succeeds, deactivate kill switch and return.
			// If failover fails, continue normal backoff on the current relay.
			if retries >= MaxRetriesBeforeFailover && r.failoverFn != nil && !failoverAttempted {
				failoverAttempted = true
				if failErr := r.failoverFn(ctx); failErr == nil {
					r.deactivateKillSwitch(ctx)
					return
				}
			}

			// Circuit breaker: after CircuitBreakerThreshold failures on the
			// current relay, give up. Kill switch stays ACTIVE — the user
			// must take explicit action to recover.
			if retries >= CircuitBreakerThreshold {
				r.tripCircuitBreaker(ctx)
				return
			}

			backoff = nextBackoff(backoff)
			continue
		}

		// Reconnection successful — deactivate kill switch, then notify hook.
		r.deactivateKillSwitch(ctx)
		r.invokeReconnectSuccessHook(ctx)
		return
	}
}

// invokeReconnectSuccessHook fires the optional reconnect-success hook under a
// recover() guard so a panicking hook does not take down the reconnect loop.
func (r *Reconnector) invokeReconnectSuccessHook(ctx context.Context) {
	if r.reconnectSuccessHook == nil {
		return
	}
	defer func() {
		_ = recover()
	}()
	r.reconnectSuccessHook(ctx)
}

// tripCircuitBreaker sets the failed flag and invokes the optional hook.
// The kill switch is intentionally left active. The hook runs under a
// recover() guard: a panicking hook must not take down the Start goroutine.
func (r *Reconnector) tripCircuitBreaker(ctx context.Context) {
	r.failed.Store(true)
	if r.circuitBreakerHook == nil {
		return
	}
	defer func() {
		_ = recover()
	}()
	r.circuitBreakerHook(ctx)
}

// deactivateKillSwitch deactivates the kill switch with one retry on failure.
func (r *Reconnector) deactivateKillSwitch(ctx context.Context) {
	if err := r.killSwitch.Deactivate(ctx); err != nil {
		select {
		case <-ctx.Done():
			return
		case <-time.After(10 * time.Millisecond):
		}
		r.killSwitch.Deactivate(ctx)
	}
}

// nextBackoff computes the next backoff duration with exponential increase,
// capped at MaxBackoff.
func nextBackoff(current time.Duration) time.Duration {
	next := current * BackoffFactor
	if next > MaxBackoff {
		return MaxBackoff
	}
	return next
}
