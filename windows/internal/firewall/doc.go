//go:build windows

// Package firewall provides an OS-level kill-switch that blocks all outgoing
// traffic except through the TUN interface and the relay IP on port 443/UDP.
//
// The Firewall interface is platform-specific:
//   - Linux: nftables via `nft -f -` shellout (Story 2.6)
//   - Windows: WFP via fwpuclnt.dll kernel-level filters (Story 2.7)
//   - Other: no-op stub returning ErrNotImplemented
//
// # Lifecycle
//
// The firewall must be activated AFTER tun.New() and routing.Setup(), and
// BEFORE tunnel.Connect(). Deactivation follows the reverse order:
//
//	Connect:    elevation.Check → tun.New → routing.Setup → firewall.Activate → tunnel.Connect
//	Disconnect: tunnel.Disconnect → firewall.Deactivate → routing.Teardown → tun.Close
//
// NEVER call Deactivate without an immediate reconnect or definitive shutdown.
// During failover/reconnect, keep the firewall active.
//
// # Crash Persistence
//
// Both nftables (Linux) and WFP (Windows) rules live in the kernel. If the
// service is killed (SIGKILL, Task Manager, panic, OOM), the deny-all rules
// persist, preventing traffic leaks. On restart, CleanupOrphans detects and
// removes stale rules before Activate installs a fresh ruleset.
//
// # Orthogonality
//
// This package has no dependency on other internal/ packages. Orchestration
// (the connect/disconnect lifecycle) is handled by windows/internal/service/.
//
// # Testing
//
// Unit tests use injected command runners (Linux) or mock WFP handles.
// Integration tests require platform-specific build tags and elevated
// privileges.
//
// Create a Firewall via New(logger, options). The Logger parameter is optional
// (nil = silent). Options configures platform-specific behavior such as
// AllowIPv6Leak.
package firewall
