//go:build !cgo

package ui

// openWebview is a no-op when CGo is disabled (webview/webview requires CGo).
func openWebview(_ string, _ func(func()), _ func()) bool { return false }
