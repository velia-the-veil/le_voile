//go:build windows

package ui

import _ "embed"

//go:embed icons/levoile.ico
var IconDefault []byte

//go:embed icons/connected.ico
var IconConnected []byte

//go:embed icons/connecting.ico
var IconConnecting []byte

//go:embed icons/disconnected.ico
var IconDisconnected []byte

// IconAlert is shown while the service is running an auto-recovery
// sequence (Story 6.3). TODO(design): replace the placeholder (currently
// a copy of connecting.ico) with a dedicated orange warning glyph.
//
//go:embed icons/alert.ico
var IconAlert []byte
