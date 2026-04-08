//go:build cgo

package ui

import webview "github.com/webview/webview_go"

func openWebview(addr string) {
	w := webview.New(false)
	if w == nil {
		return
	}
	defer w.Destroy()
	w.SetTitle("Le Voile")
	w.SetSize(420, 540, webview.HintFixed)
	w.Navigate("http://" + addr + "/")
	w.Run()
}
