//go:build cgo && linux

package ui

import (
	"os/exec"
	"sync/atomic"

	webview "github.com/webview/webview_go"
)

// openWebview opens the webview window on Linux (WebKitGTK). Returns true if
// the user clicked the close button (quit requested).
//
// Compared to the Windows implementation, this variant intentionally omits the
// native titlebar manipulation and bottom-right positioning — those rely on
// Win32 APIs. On Linux, the GTK window manager handles placement and chrome;
// fine-tuned styling can be layered in a follow-up story if needed.
func openWebview(addr string, setTerminate func(func()), clearTerminate func(), showCh <-chan struct{}) bool {
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

	// showCh signals from tray menu to raise the window. On WebKitGTK the
	// window is already visible; drain the channel to prevent blocking senders.
	go func() {
		for range showCh {
		}
	}()

	w.Navigate("http://" + addr + "/")
	w.Run()
	return quitRequested.Load()
}
