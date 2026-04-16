package tray

import _ "embed"

//go:embed levoile.ico
var IconDefault []byte

//go:embed connected.ico
var IconConnected []byte

//go:embed connecting.ico
var IconConnecting []byte

//go:embed disconnected.ico
var IconDisconnected []byte

// IconCaptive reuses the connecting (orange/warning) icon for captive portal
// state. Replace with a dedicated asset when available. Defensive copy to
// avoid shared mutation if either slice is ever modified.
var IconCaptive []byte

func init() {
	IconCaptive = append([]byte(nil), IconConnecting...)
}
