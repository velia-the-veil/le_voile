//go:build linux

package dns

import (
	"context"
	"errors"
	"os/exec"
)

// Sentinel errors for DNS manager operations.
var (
	ErrResolverNotSet    = errors.New("dns: resolver not set")
	ErrSetResolverFailed = errors.New("dns: failed to set resolver")
	ErrRestoreFailed     = errors.New("dns: failed to restore resolver")
	ErrNoActiveInterface = errors.New("dns: no active network interface found")
)

// DefaultListenAddr is the default address for the local DNS proxy (IPv4).
const DefaultListenAddr = "127.0.0.1:53"

// DefaultListenAddrV6 is the IPv6 loopback address for the local DNS proxy.
const DefaultListenAddrV6 = "[::1]:53"

// DNSManager manages the system DNS resolver configuration.
type DNSManager interface {
	// SetResolver changes the system DNS resolver on all active interfaces
	// to point to the specified address (e.g., "127.0.0.1").
	SetResolver(ctx context.Context, addr string) error

	// RestoreResolver restores the system DNS resolver to its original value
	// saved during the last call to SetResolver.
	RestoreResolver(ctx context.Context) error

	// OriginalResolver returns the saved original resolver address,
	// or an empty string if SetResolver has not been called.
	OriginalResolver() string
}

// commandRunner allows mocking OS command execution in tests.
type commandRunner func(ctx context.Context, name string, args ...string) ([]byte, error)

// defaultRunner executes a real OS command.
// On Windows, init() in cmd_windows.go replaces this with hiddenRunner
// to prevent console window flashes.
var defaultRunner commandRunner = func(ctx context.Context, name string, args ...string) ([]byte, error) {
	return exec.CommandContext(ctx, name, args...).CombinedOutput()
}
