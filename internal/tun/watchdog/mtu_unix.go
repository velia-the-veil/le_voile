//go:build linux

package watchdog

// mtuMatches does a strict equality comparison on Unix — the TUN driver
// respects the MTU set at creation time, so any deviation indicates
// either tampering or a misconfigured recreation.
func mtuMatches(actual, expected int) bool { return actual == expected }
