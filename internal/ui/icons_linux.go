//go:build linux

package ui

import _ "embed"

//go:embed icons/levoile.png
var IconDefault []byte

//go:embed icons/connected.png
var IconConnected []byte

//go:embed icons/connecting.png
var IconConnecting []byte

//go:embed icons/disconnected.png
var IconDisconnected []byte

// IconAlert is shown while the service is running an auto-recovery
// sequence (Story 6.3). TODO(design): replace the placeholder (currently
// a copy of connecting.png) with a dedicated orange warning glyph.
//
//go:embed icons/alert.png
var IconAlert []byte
