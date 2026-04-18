//go:build !windows && !linux

package ui

// Icon byte slices are empty on platforms without a targeted systray build
// (darwin, BSDs). The build chain doesn't target these for the UI binary, but
// keeping the symbols defined lets internal/ui be consumed by downstream
// packages that may compile against other GOOS values.
var (
	IconDefault      []byte
	IconConnected    []byte
	IconConnecting   []byte
	IconDisconnected []byte
	// IconAlert mirrors the windows/linux slice for parity. Story 6.3.
	IconAlert []byte
)
