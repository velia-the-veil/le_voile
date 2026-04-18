//go:build !cgo

package ui

// openWebview is a no-op when CGo is disabled (webview/webview requires CGo).
// Signature mirrors the cgo builds so ui.handleOpenWebview compiles everywhere.
func openWebview(_ string,
	_ func(func()),
	_ func(),
	_ <-chan struct{},
	_ <-chan struct{},
	_ func(bool),
) bool {
	return false
}
