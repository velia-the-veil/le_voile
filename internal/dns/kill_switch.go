package dns

import (
	"context"
	"errors"
	"fmt"
	"sync"
)

// Sentinel errors for kill switch operations.
var (
	ErrKillSwitchAlreadyActive   = errors.New("dns: kill switch already active")
	ErrKillSwitchAlreadyInactive = errors.New("dns: kill switch already inactive")
)

// KillSwitch blocks all DNS queries when the tunnel connection is lost
// by stopping the local DNS proxy while keeping the system resolver
// pointed at 127.0.0.1. When the proxy is stopped, DNS queries to
// 127.0.0.1:53 fail, effectively blocking all DNS resolution.
type KillSwitch struct {
	mu            sync.RWMutex
	active        bool
	dnsMgr        DNSManager
	stopProxy     func()
	startProxy    func(ctx context.Context) error
	forceResolver func(ctx context.Context, addr string) error
}

// NewKillSwitch creates a KillSwitch that controls DNS blocking via the
// given DNSManager and proxy lifecycle callbacks.
//
// stopProxy is called to cancel the DNS proxy (blocking all DNS queries).
// startProxy is called to restart the DNS proxy (restoring DNS resolution).
func NewKillSwitch(dnsMgr DNSManager, stopProxy func(), startProxy func(ctx context.Context) error) *KillSwitch {
	return &KillSwitch{
		dnsMgr:     dnsMgr,
		stopProxy:  stopProxy,
		startProxy: startProxy,
	}
}

// SetForceResolver sets an optional function that forces the system resolver
// to a given address without overwriting the saved original DNS. This is used
// during activation to defensively verify the resolver when the original has
// already been saved by DNSManager.SetResolver.
func (ks *KillSwitch) SetForceResolver(fn func(ctx context.Context, addr string) error) {
	ks.forceResolver = fn
}

// Activate enables the kill switch: stops the DNS proxy so that all
// DNS queries are blocked, and defensively ensures the system resolver
// points to 127.0.0.1.
//
// Returns ErrKillSwitchAlreadyActive if the kill switch is already engaged.
func (ks *KillSwitch) Activate(ctx context.Context) error {
	ks.mu.Lock()
	defer ks.mu.Unlock()

	if ks.active {
		return ErrKillSwitchAlreadyActive
	}

	// Stop proxy first — blocks DNS queries immediately.
	ks.stopProxy()

	// Defensively ensure resolver points to 127.0.0.1.
	if ks.dnsMgr.OriginalResolver() == "" {
		// First activation — save original DNS via manager.
		if err := ks.dnsMgr.SetResolver(ctx, "127.0.0.1"); err != nil {
			return fmt.Errorf("dns: kill_switch: activate: %w", err)
		}
	} else if ks.forceResolver != nil {
		// Original already saved — force resolver without overwriting it.
		// Protects against external resolver changes between watchdog checks.
		if err := ks.forceResolver(ctx, "127.0.0.1"); err != nil {
			return fmt.Errorf("dns: kill_switch: activate: force resolver: %w", err)
		}
	}

	ks.active = true
	return nil
}

// Deactivate disables the kill switch: restarts the DNS proxy so that
// DNS queries are forwarded through the tunnel again. The system resolver
// remains at 127.0.0.1 (pointing to the now-running local proxy).
//
// Returns ErrKillSwitchAlreadyInactive if the kill switch is not engaged.
func (ks *KillSwitch) Deactivate(ctx context.Context) error {
	ks.mu.Lock()
	defer ks.mu.Unlock()

	if !ks.active {
		return ErrKillSwitchAlreadyInactive
	}

	if err := ks.startProxy(ctx); err != nil {
		return fmt.Errorf("dns: kill_switch: deactivate: %w", err)
	}

	ks.active = false
	return nil
}

// IsActive returns true if the kill switch is currently engaged
// (DNS queries are being blocked).
func (ks *KillSwitch) IsActive() bool {
	ks.mu.RLock()
	defer ks.mu.RUnlock()
	return ks.active
}
