//go:build windows

package watchdog

// mtuMatches returns true on Windows regardless of the actual NDIS MTU:
// the Wintun driver exposes a fixed 65535 max-frame size to the NDIS
// layer, while the service configures a logical tunnel MTU (1420 by
// default) that lives only in the framing path. Comparing the two
// produced a permanent mismatch. The interface-present + FlagUp
// checks in Check() still cover the legitimate tampering cases on
// Windows (adapter removed or forcibly disabled).
func mtuMatches(_, _ int) bool { return true }
