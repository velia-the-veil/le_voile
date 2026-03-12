package registry

import (
	"context"
	"errors"
	"fmt"
	"sync"
)

// ErrNoAlternativeRelay is returned when no alternative relay is available for failover.
var ErrNoAlternativeRelay = errors.New("registry: failover: no alternative relay available")

// RelayUpdater is the interface for updating the tunnel client's relay target.
// Implemented by tunnel.Client.
type RelayUpdater interface {
	UpdateRelay(domain string, pubKeyBase64 string) error
}

// FailoverManager handles automatic failover to the next relay in the latency ranking.
type FailoverManager struct {
	discoverer     *Discoverer
	tunnelUpdater  RelayUpdater
	connectFn      func(ctx context.Context) error
	mu             sync.RWMutex
	currentRelayID string
}

// NewFailoverManager creates a failover manager.
func NewFailoverManager(discoverer *Discoverer, updater RelayUpdater, connectFn func(ctx context.Context) error) *FailoverManager {
	return &FailoverManager{
		discoverer:    discoverer,
		tunnelUpdater: updater,
		connectFn:     connectFn,
	}
}

// HandleFailover attempts to switch to the next available relay and reconnect.
// Returns nil on success, ErrNoAlternativeRelay if no other relay is available.
func (fm *FailoverManager) HandleFailover(ctx context.Context) error {
	fm.mu.Lock()
	defer fm.mu.Unlock()

	relays := fm.discoverer.Relays()
	if len(relays) <= 1 {
		return ErrNoAlternativeRelay
	}

	// Find the current relay and save its coordinates for potential restoration.
	currentIdx := -1
	var originalDomain, originalPubKey string
	for i, r := range relays {
		if r.ID == fm.currentRelayID {
			currentIdx = i
			originalDomain = r.Domain
			originalPubKey = r.PublicKey
			break
		}
	}

	// Try each relay after the current one in the ranking.
	// Use <= to cover all relays when currentIdx == -1 (current relay not
	// found in list after a refresh). The ID check below skips the current.
	for offset := 1; offset <= len(relays); offset++ {
		nextIdx := (currentIdx + offset) % len(relays)
		next := relays[nextIdx]

		if next.ID == fm.currentRelayID {
			continue // Skip the current (failed) relay.
		}

		if err := fm.tunnelUpdater.UpdateRelay(next.Domain, next.PublicKey); err != nil {
			continue // This relay's key is invalid — try next.
		}

		if err := fm.connectFn(ctx); err != nil {
			continue // Connection failed — try next.
		}

		// Failover successful.
		fm.currentRelayID = next.ID
		return nil
	}

	// All alternatives failed — restore the original relay coordinates so that
	// the Reconnector's subsequent backoff retries target the original relay,
	// not the last alternative that was tried.
	if originalDomain != "" {
		_ = fm.tunnelUpdater.UpdateRelay(originalDomain, originalPubKey)
	}

	return fmt.Errorf("%w: all alternatives failed", ErrNoAlternativeRelay)
}

// SetCurrentRelay sets the currently active relay ID.
func (fm *FailoverManager) SetCurrentRelay(relayID string) {
	fm.mu.Lock()
	defer fm.mu.Unlock()
	fm.currentRelayID = relayID
}

// CurrentRelayID returns the currently active relay ID.
func (fm *FailoverManager) CurrentRelayID() string {
	fm.mu.RLock()
	defer fm.mu.RUnlock()
	return fm.currentRelayID
}
