package tunnel

import (
	"context"
	"errors"
	"sync"
	"time"
)

// Sentinel errors for reconnection operations.
var (
	ErrReconnectInProgress = errors.New("tunnel: reconnect already in progress")
	ErrReconnectStopped    = errors.New("tunnel: reconnect stopped")
)

// Reconnection backoff constants.
const (
	InitialBackoff          = 1 * time.Second
	BackoffFactor           = 2
	MaxBackoff              = 30 * time.Second
	MaxRetriesBeforeFailover = 3
)

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

// WithFailoverFn sets a failover function to call after MaxRetriesBeforeFailover
// consecutive connection failures on the current relay.
func WithFailoverFn(fn func(ctx context.Context) error) ReconnectorOption {
	return func(r *Reconnector) {
		r.failoverFn = fn
	}
}

// Reconnector monitors tunnel state changes and automatically reconnects
// with exponential backoff when the connection is lost. It coordinates
// with a KillSwitch to block DNS during reconnection.
type Reconnector struct {
	mu           sync.Mutex
	reconnecting bool
	cancel       context.CancelFunc
	done         chan struct{}

	connectFn  ConnectFunc
	killSwitch KillSwitchController
	updates    <-chan ConnState
	failoverFn func(ctx context.Context) error
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
			if state == StateDisconnected {
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

// handleDisconnect activates the kill switch and attempts reconnection
// with exponential backoff. After MaxRetriesBeforeFailover consecutive
// failures, triggers failover to an alternative relay if configured.
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

	// Activate kill switch — block DNS during reconnection (NFR5).
	// Retry once on failure to maximize DNS leak protection.
	if err := r.killSwitch.Activate(ctx); err != nil {
		// Best-effort retry after short delay — covers transient failures.
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

			// Attempt failover ONCE after MaxRetriesBeforeFailover consecutive failures.
			// If failover fails, continue normal backoff without re-triggering failover.
			if retries >= MaxRetriesBeforeFailover && r.failoverFn != nil && !failoverAttempted {
				failoverAttempted = true
				if failErr := r.failoverFn(ctx); failErr == nil {
					// Failover succeeded — relay changed and connected.
					r.deactivateKillSwitch(ctx)
					return
				}
				// Failover failed — continue backoff on current relay.
			}

			backoff = nextBackoff(backoff)
			continue
		}

		// Reconnection successful — deactivate kill switch.
		r.deactivateKillSwitch(ctx)
		return
	}
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
