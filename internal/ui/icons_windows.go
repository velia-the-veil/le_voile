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
