// R-T8 (2026-05-05) — Active heartbeat probe via /health.
//
// Use case : on cellular / mobile networks the QUIC connection can become a
// "zombie" — the client keeps writing packets but the server never receives
// them (CGNAT pool rotation, intra-LTE cell handoff with no network type
// change, mid-stream NAT translation drop). quic-go's MaxIdleTimeout (90s)
// does NOT fire because :
//   - The application keeps writing → the connection is not idle.
//   - quic-go's idle timer measures "time since last packet RECEIVED", but
//     KeepAlivePeriod (10s) sends client-side PINGs that don't reset the
//     server's view, so when the server stops responding the timer just
//     keeps going. In practice we saw 90+ minutes of zombie state on Free
//     Mobile 4G LTE before the user noticed.
//
// The heartbeat probes the relay's `/health` endpoint every 5s with a 2s
// timeout. After 2 consecutive failures the client transitions to
// StateDisconnected, allowing the upstream Reconnector (Android service or
// internal/tunnel/reconnect.go on desktop) to re-establish.
//
// 5s + 2 fails = 10-15s detection floor. Acceptable trade-off : faster
// would generate more probe traffic ; slower would let the user wait
// longer before reconnect kicks in.
//
// On Android, R-T8 also wires QUIC Connection Migration (MigrateToFD) which
// eliminates the visible coupure for cases where the underlying network
// change IS observable (Wi-Fi <-> LTE, network detach/attach). The
// heartbeat is the safety net for invisible cases (CGNAT, intra-LTE).

package tunnel

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"time"
)

const (
	heartbeatInterval               = 5 * time.Second
	heartbeatTimeout                = 2 * time.Second
	maxConsecutiveHeartbeatFailures = 2
)

// startHeartbeat spawns the heartbeat goroutine. Idempotent : if a heartbeat
// is already running, the existing one is left in place. Stopped via
// stopHeartbeat or implicitly when ResetTransport is called (which also
// invokes stopHeartbeat).
//
// `parentCtx` is the connection's lifecycle context : when it is cancelled
// (Disconnect, app shutdown) the goroutine exits cleanly without flipping
// state.
//
// Failures are tracked in a local counter (NOT consecutiveFailures, which
// is reserved for the DoH path) — heartbeat and DoH should fail
// independently and they trip the same StateDisconnected exit, so the
// state transition is atomic via state.Set.
func (c *Client) startHeartbeat(parentCtx context.Context) {
	c.heartbeatMu.Lock()
	if c.heartbeatStop != nil {
		c.heartbeatMu.Unlock()
		return // already running
	}
	stop := make(chan struct{})
	c.heartbeatStop = stop
	c.heartbeatMu.Unlock()

	go c.heartbeatLoop(parentCtx, stop)
}

// stopHeartbeat signals the heartbeat goroutine to exit and waits at most
// briefly for it to acknowledge. Idempotent — safe to call from multiple
// sites (Disconnect, ResetTransport, error paths) without coordination.
func (c *Client) stopHeartbeat() {
	c.heartbeatMu.Lock()
	stop := c.heartbeatStop
	c.heartbeatStop = nil
	c.heartbeatMu.Unlock()

	if stop != nil {
		close(stop)
	}
}

// heartbeatLoop runs the periodic /health probe. Returns when :
//   - parentCtx is cancelled (graceful shutdown), OR
//   - stop is closed (stopHeartbeat called), OR
//   - 2 consecutive probe failures triggered StateDisconnected (probe was
//     hitting a dead connection).
func (c *Client) heartbeatLoop(parentCtx context.Context, stop <-chan struct{}) {
	ticker := time.NewTicker(heartbeatInterval)
	defer ticker.Stop()

	consecutiveFailures := 0

	for {
		select {
		case <-parentCtx.Done():
			return
		case <-stop:
			return
		case <-ticker.C:
			// Skip probing if we're not connected — saves bandwidth and
			// prevents flagging a transient Connecting state as a failure.
			if c.state.Get() != StateConnected {
				consecutiveFailures = 0
				continue
			}

			err := c.pingHealth(parentCtx)
			if err != nil {
				consecutiveFailures++
				if consecutiveFailures >= maxConsecutiveHeartbeatFailures && c.state.Get() == StateConnected {
					// Trip the disconnect — wake everyone up.
					//
					// 1. state.Set(Disconnected) updates internal state (read by Get).
					// 2. emitStatus("disconnected") notifies the Kotlin status
					//    callback so LeVoileVpnService observes Disconnected and
					//    triggers its auto-reconnect path (R-T8 backoff). Without
					//    this call the UI keeps showing "Connected" forever and
					//    the tunnel stays zombie until quic-go's MaxIdleTimeout
					//    (90s) finally fires.
					// 3. Force-close the captured *quic.Conn — wakes up the pump
					//    goroutine which is blocked on RoundTrip / stream Read.
					//    Pump returns with error, runGomobilePump's deferred
					//    emitStatus("disconnected") runs (idempotent — Kotlin
					//    side dedupes consecutive identical states).
					c.state.Set(StateDisconnected)
					emitStatus("disconnected", "")

					c.quicMu.RLock()
					qc := c.quicConn
					c.quicMu.RUnlock()
					if qc != nil {
						// Application error code 0 = "heartbeat trip".
						// Close gracefully — the pump exits cleanly via its
						// stream read loop returning an error.
						_ = qc.CloseWithError(0, "heartbeat-trip")
					}
					return
				}
				continue
			}

			// Success — reset counter.
			consecutiveFailures = 0
		}
	}
}

// pingHealth issues a single GET /health request bounded by heartbeatTimeout.
// Returns nil on HTTP 200, an error otherwise (transport failure, non-200,
// timeout). The relay's HealthHandler (relay/relay/health.go) returns 200
// with a JSON body — we don't parse the body, presence of 200 is enough.
func (c *Client) pingHealth(parentCtx context.Context) error {
	probeCtx, cancel := context.WithTimeout(parentCtx, heartbeatTimeout)
	defer cancel()

	url := c.relayURL("/health")
	req, err := http.NewRequestWithContext(probeCtx, http.MethodGet, url, nil)
	if err != nil {
		return fmt.Errorf("tunnel: heartbeat: build request: %w", err)
	}

	resp, err := c.getHTTPClient().Do(req)
	if err != nil {
		return fmt.Errorf("tunnel: heartbeat: do: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("tunnel: heartbeat: status %d", resp.StatusCode)
	}
	return nil
}

// errHeartbeatStopped is returned by tests to confirm the heartbeat exited
// cleanly without tripping a state change. Not used in production code paths.
//
//nolint:unused // exposed for tests in heartbeat_test.go
var errHeartbeatStopped = errors.New("tunnel: heartbeat: stopped")
