//go:build linux

package firewall

import (
	"context"
	"errors"
	"net"
)

// Sentinel errors for firewall operations.
var (
	// ErrNftablesUnavailable is returned when the nft binary or nf_tables
	// kernel module is missing or non-functional.
	ErrNftablesUnavailable = errors.New("firewall: nftables kernel module unavailable, cannot start Le Voile")

	// ErrNotImplemented is returned by platform stubs that are not yet
	// implemented (e.g. macOS).
	ErrNotImplemented = errors.New("firewall: not implemented on this platform")

	// ErrWFPUnavailable is returned when fwpuclnt.dll cannot be loaded.
	ErrWFPUnavailable = errors.New("firewall: WFP unavailable (fwpuclnt.dll not found)")

	// ErrNotElevated is returned when the process lacks LocalSystem
	// privileges required for WFP operations.
	ErrNotElevated = errors.New("firewall: WFP requires LocalSystem (install via kardianos/service)")
)

// Mode selects the firewall ruleset to apply.
type Mode int

const (
	// ModeFull is the standard kill-switch: deny all except TUN + relay IP:443.
	ModeFull Mode = iota
	// ModeCaptive allows only traffic to the LAN gateway (for Wi-Fi captive
	// portal authentication) + loopback. No TUN, no relay.
	ModeCaptive
)

// String returns a human-readable name for the mode.
func (m Mode) String() string {
	switch m {
	case ModeFull:
		return "full"
	case ModeCaptive:
		return "captive"
	default:
		return "unknown"
	}
}

// Options configures the firewall implementation.
type Options struct {
	// AllowIPv6Leak when true skips IPv6 BLOCK filters, letting IPv6 traffic
	// bypass the kill switch. See Story 2.9 for user-facing toggle.
	AllowIPv6Leak bool
}

// ActivateParams groups all parameters for Activate. Using a struct avoids
// a growing parameter list as modes require different inputs.
type ActivateParams struct {
	// Mode selects the ruleset (ModeFull or ModeCaptive).
	Mode Mode
	// RelayIP is the relay's IPv4 address. Required for ModeFull, ignored for ModeCaptive.
	RelayIP net.IP
	// TunName is the TUN interface name. Required for ModeFull, ignored for ModeCaptive.
	TunName string
	// LanGateway is the local network gateway IP. Required for ModeCaptive, ignored for ModeFull.
	LanGateway net.IP
}

// Firewall manages OS-level kill-switch rules.
// Implementations are platform-specific (nftables on Linux, WFP on Windows).
type Firewall interface {
	// Activate installs deny-all rules based on the given mode.
	//   ModeFull:    allows only TUN + relay IP:443 + loopback
	//   ModeCaptive: allows only LAN gateway + loopback (for portal auth)
	// Idempotent: flushes and replaces any pre-existing ruleset atomically.
	Activate(ctx context.Context, params ActivateParams) error

	// Deactivate removes all kill-switch rules.
	// Idempotent: returns nil even if no rules are active.
	Deactivate(ctx context.Context) error

	// IsActive queries the kernel to check whether the kill-switch ruleset
	// is currently loaded. Returns (false, nil) when rules are absent,
	// (false, err) on shell/permission errors.
	IsActive(ctx context.Context) (bool, error)

	// CleanupOrphans removes any orphan rules left by a previous crash.
	// Returns the number of rules removed. On Linux this is a no-op because
	// nftables Activate replaces atomically.
	CleanupOrphans(ctx context.Context) (int, error)

	// SetIPv6Policy updates the IPv6 leak setting at runtime.
	// When allow is true, IPv6 traffic bypasses the kill switch.
	// When allow is false, IPv6 is blocked (safe default).
	// Returns ErrNotImplemented on platforms without support.
	// The firewall must be active (Activate called) before calling this.
	SetIPv6Policy(ctx context.Context, allow bool) error

	// AlteredCh returns a channel that receives a value when the firewall
	// watchdog detects external tampering (e.g. third-party AV removing
	// rules). Returns nil on platforms without a watchdog.
	AlteredCh() <-chan struct{}
}

// Logger abstracts structured logging. If nil, the firewall stays silent.
type Logger interface {
	Infof(format string, args ...any)
	Warnf(format string, args ...any)
	Errorf(format string, args ...any)
	Debugf(format string, args ...any)
}
