//go:build windows

package service

import (
	"context"
	"crypto/subtle"
	"errors"
	"fmt"
	"net"
	"time"

	"github.com/velia-the-veil/le_voile/internal/firewall"
)

// Kill-switch mode constants exposed via IPC.
const (
	// KillSwitchModeNormal = OS-level firewall active (default safe state).
	KillSwitchModeNormal = "normal"
	// KillSwitchModeDegraded = firewall disabled, traffic in clear (Story 5.9).
	KillSwitchModeDegraded = "degraded"
)

// Sentinel errors returned by SetKillSwitchMode.
var (
	// ErrKillSwitchInvalidMode is returned when an unknown mode value is passed.
	ErrKillSwitchInvalidMode = errors.New("service: killswitch: invalid mode")
	// ErrKillSwitchCaptiveActive is returned when the user attempts to switch
	// modes while the captive portal lockdown is active. The captive flow owns
	// the firewall in that state — refuse to override.
	ErrKillSwitchCaptiveActive = errors.New("captive_portal_active")
	// ErrKillSwitchNotConnected is returned when restoring (mode=normal) is
	// requested but no relay IP / TUN device is available — e.g. the tunnel
	// has never been up since boot. Caller should reconnect first.
	ErrKillSwitchNotConnected = errors.New("tunnel_not_connected")
	// ErrCtlAuthFailed is returned when a levoile-ctl request presents an
	// invalid or missing machine-local token.
	ErrCtlAuthFailed = errors.New("auth_failed")
)

// KillSwitchMode reports the current kill-switch mode.
// Returns KillSwitchModeNormal when the OS firewall is enabled in config,
// KillSwitchModeDegraded otherwise.
func (p *Program) KillSwitchMode() string {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.config.FirewallEnabled {
		return KillSwitchModeNormal
	}
	return KillSwitchModeDegraded
}

// SetKillSwitchMode toggles the OS-level firewall at runtime. Story 5.9.
//
// mode must be KillSwitchModeNormal or KillSwitchModeDegraded.
// source is a free-form tag included in audit logs ("ui", "ctl", "auto-reconnect").
//
// Atomicity: this method only owns the firewall transition and the in-memory
// config flip. The TOML persistence step is delegated to the killSwitchPersist
// callback (set via SetKillSwitchPersister). If the callback fails, the
// in-memory state is rolled back to its previous value AND the firewall
// transition is reverted, so the service stays internally consistent.
//
// Refuses with ErrKillSwitchCaptiveActive when the captive portal lockdown
// is active — that flow owns the firewall and a degraded-mode override
// would create a leak window with the wrong ruleset.
func (p *Program) SetKillSwitchMode(ctx context.Context, mode, source string) error {
	if mode != KillSwitchModeNormal && mode != KillSwitchModeDegraded {
		return fmt.Errorf("%w: %q", ErrKillSwitchInvalidMode, mode)
	}
	if p.captivePortal.Load() {
		return ErrKillSwitchCaptiveActive
	}

	p.mu.Lock()
	previous := p.config.FirewallEnabled
	desired := mode == KillSwitchModeNormal
	if previous == desired {
		// No-op fast path: nothing to change. Still log for auditability.
		p.mu.Unlock()
		fmt.Fprintf(serviceStderr, "service: killswitch_mode=%s source=%s ts=%s noop=true\n",
			mode, source, time.Now().UTC().Format(time.RFC3339))
		return nil
	}

	// Apply firewall transition first (it may reveal hard failures like
	// nft missing), then flip the in-memory flag, then persist.
	if err := p.applyFirewallForModeLocked(ctx, desired); err != nil {
		p.mu.Unlock()
		return err
	}
	p.config.FirewallEnabled = desired
	p.mu.Unlock()

	// Persist to TOML outside p.mu to avoid blocking other callers on disk I/O.
	// On failure, roll back: re-flip the flag and reverse the firewall transition.
	// If the firewall rollback ALSO fails, the actual OS state is now indeterminate
	// — we log loudly so operators can investigate (Story 5.9 M1 fix).
	if persist := p.killSwitchPersist; persist != nil {
		if err := persist(desired); err != nil {
			p.mu.Lock()
			if rbErr := p.applyFirewallForModeLocked(ctx, previous); rbErr != nil {
				fmt.Fprintf(serviceStderr,
					"service: killswitch rollback firewall failed (state INDETERMINATE): persist_err=%v rollback_err=%v\n",
					err, rbErr)
			}
			p.config.FirewallEnabled = previous
			p.mu.Unlock()
			return fmt.Errorf("service: killswitch persist: %w", err)
		}
	}

	fmt.Fprintf(serviceStderr, "service: killswitch_mode=%s source=%s ts=%s\n",
		mode, source, time.Now().UTC().Format(time.RFC3339))
	return nil
}

// applyFirewallForModeLocked applies the firewall side-effect of a mode
// transition. Caller MUST hold p.mu.
//
//   - desired=true (normal mode):  Activate ModeFull on the existing TUN+relayIP,
//     creating the firewall manager if it hasn't been created yet.
//   - desired=false (degraded):    Deactivate the firewall (idempotent).
func (p *Program) applyFirewallForModeLocked(ctx context.Context, desired bool) error {
	if !desired {
		// Switching to degraded: tear down firewall.
		fw := p.firewallMgr
		if fw == nil {
			return nil // already absent
		}
		if err := fw.Deactivate(ctx); err != nil {
			return fmt.Errorf("service: killswitch deactivate: %w", err)
		}
		return nil
	}

	// Switching to normal: need TUN + resolved relay IP, otherwise we can't
	// build the kill-switch ruleset (it allows TUN + relay:443 only).
	if p.tunDev == nil {
		return ErrKillSwitchNotConnected
	}
	relayIP := p.resolvedRelayIP()
	if relayIP == nil {
		return ErrKillSwitchNotConnected
	}

	fw := p.firewallMgr
	if fw == nil {
		fwLog := &serviceLogger{}
		fw = firewallFactory(fwLog, firewall.Options{AllowIPv6Leak: p.config.AllowIPv6Leak})
	}
	if err := fw.Activate(ctx, firewall.ActivateParams{
		Mode:    firewall.ModeFull,
		RelayIP: relayIP,
		TunName: p.tunDev.Name(),
	}); err != nil {
		return fmt.Errorf("service: killswitch activate: %w", err)
	}
	p.firewallMgr = fw
	return nil
}

// SetKillSwitchPersister registers the callback that persists the
// enable_killswitch flag to the TOML config file. Called from cmd/client,
// where the config-path discovery lives. Pass nil to disable persistence.
func (p *Program) SetKillSwitchPersister(fn func(enabled bool) error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.killSwitchPersist = fn
}

// MaybeRestoreKillSwitch is invoked after a successful tunnel reconnect.
// When degraded mode is active and the captive portal is not, it restores
// the OS firewall (Story 5.9 AC4 — auto-restoration).
//
// Best-effort: errors are logged but do not propagate (the tunnel is up,
// returning an error here would be confusing for the caller).
func (p *Program) MaybeRestoreKillSwitch(ctx context.Context, source string) {
	if p.captivePortal.Load() {
		return
	}
	if p.KillSwitchMode() == KillSwitchModeNormal {
		return
	}
	if err := p.SetKillSwitchMode(ctx, KillSwitchModeNormal, source); err != nil {
		// ErrKillSwitchNotConnected is benign here (tunnel-restart race).
		if errors.Is(err, ErrKillSwitchNotConnected) {
			return
		}
		fmt.Fprintf(serviceStderr, "service: killswitch auto-restore failed: %v\n", err)
	}
}

// SetCtlToken installs the machine-local token used to authenticate
// levoile-ctl IPC requests. Token is copied so callers may zero their buffer.
// Pass nil/empty to disable ctl auth (UI paths remain operational).
func (p *Program) SetCtlToken(token []byte) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if len(token) == 0 {
		p.ctlToken = nil
		return
	}
	p.ctlToken = append([]byte(nil), token...)
}

// VerifyCtlToken returns true when the provided token matches the configured
// ctl token in constant time (NFR9c). Returns false when no token is set
// (callers must reject ctl-only paths in that case via a separate check).
func (p *Program) VerifyCtlToken(provided string) bool {
	p.mu.Lock()
	expected := p.ctlToken
	p.mu.Unlock()
	if len(expected) == 0 || len(provided) == 0 {
		return false
	}
	return subtle.ConstantTimeCompare(expected, []byte(provided)) == 1
}

// HasCtlToken reports whether a ctl token is currently configured.
func (p *Program) HasCtlToken() bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	return len(p.ctlToken) > 0
}

// InjectFirewallForTest pre-populates the firewall manager + resolved relay
// IP + a stub TUN device so cross-package tests can exercise SetKillSwitchMode
// without spinning up the full Program.run() pipeline. Test-only.
func (p *Program) InjectFirewallForTest(fw firewall.Firewall, relayIP net.IP) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.firewallMgr = fw
	p.firewallRelayIP.Store(relayIP)
	p.tunDev = &injectedTunForTest{}
}

// injectedTunForTest is a minimal tun.Device satisfying the interface for tests
// that just need a non-nil device with a name. Read/Write/Close are no-ops.
type injectedTunForTest struct{}

func (injectedTunForTest) Name() string                    { return "levoile0" }
func (injectedTunForTest) MTU() int                        { return 1420 }
func (injectedTunForTest) Read(_ []byte) (int, error)      { return 0, nil }
func (injectedTunForTest) Write(_ []byte) (int, error)     { return 0, nil }
func (injectedTunForTest) Close() error                    { return nil }

// ForceCaptivePortalForTest sets the captive-portal flag from cross-package
// tests that need to exercise the AC7 refusal path without spinning up the
// captive watcher. Test-only.
func (p *Program) ForceCaptivePortalForTest(active bool) {
	p.captivePortal.Store(active)
}
