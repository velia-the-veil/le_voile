//go:build !windows && !linux

package anomaly

// NewLogger on unsupported platforms returns the stderr fallback. The
// project builds and ships only for windows/linux; this stub keeps the
// package importable for portable test runs (e.g. darwin CI).
func NewLogger() Logger {
	return newStderrLogger(nil)
}
