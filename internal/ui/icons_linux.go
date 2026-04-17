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
