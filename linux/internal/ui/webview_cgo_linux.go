//go:build cgo && linux

package ui

import (
	"os/exec"
	"sync/atomic"

	webview "github.com/webview/webview_go"
)

// openWebview opens the webview window on Linux (WebKitGTK). Returns true iff
// the user requested application quit via ✕ (JS __close).
//
// Compared to the Windows implementation, this variant intentionally omits
// native titlebar manipulation and bottom-right positioning — those rely on
// Win32 APIs. On Linux, the GTK window manager handles placement and chrome.
//
// showCh / hideCh / reportHidden are accepted for signature parity with the
// Windows build. On Linux they are ignored: WebKitGTK has no portable
// equivalent to ShowWindow(SW_HIDE)/SW_SHOW, and libayatana-appindicator
// rarely delivers SetOnTapped reliably anyway — users interact with the
// window via its own titlebar and the tray context menu.
func openWebview(addr string,
	setTerminate func(func()),
	clearTerminate func(),
	showCh <-chan struct{},
	hideCh <-chan struct{},
	reportHidden func(bool),
) bool {
	_, _, _ = showCh, hideCh, reportHidden

	w := webview.New(false)
	if w == nil {
		return false
	}
	var alive atomic.Bool
	alive.Store(true)
	setTerminate(func() {
		if alive.Load() {
			w.Terminate()
		}
	})
	defer func() {
		alive.Store(false)
		clearTerminate()
		w.Destroy()
	}()
	w.SetTitle("Le Voile")
	w.SetSize(420, 540, webview.HintFixed)

	// Open external links in the default browser via xdg-open.
	w.Bind("__openExternal", func(url string) {
		exec.Command("xdg-open", url).Start()
	})

	// __minimize / __close bindings kept for frontend compatibility with Windows.
	// On Linux, WM handles minimize natively; __minimize becomes a no-op.
	w.Bind("__minimize", func() {})

	var quitRequested atomic.Bool
	w.Bind("__close", func() {
		quitRequested.Store(true)
		if alive.Load() {
			w.Terminate()
		}
	})

	w.Init(`document.addEventListener('click', function(e) {
		var a = e.target.closest('a[target="_blank"], a[href^="http"]');
		if (!a) return;
		var href = a.getAttribute('href');
		if (href && !href.startsWith(location.origin)) {
			e.preventDefault();
			__openExternal(href);
		}
	}, true);`)

	w.Navigate("http://" + addr + "/")
	w.Run()
	return quitRequested.Load()
}
