//go:build cgo && windows

package ui

import (
	"os/exec"
	"sync/atomic"
	"unsafe"

	webview "github.com/webview/webview_go"
	"golang.org/x/sys/windows"
)

var (
	user32                   = windows.NewLazySystemDLL("user32.dll")
	procSetWindowPos         = user32.NewProc("SetWindowPos")
	procSystemParametersInfo = user32.NewProc("SystemParametersInfoW")
	procGetWindowLong        = user32.NewProc("GetWindowLongPtrW")
	procSetWindowLong        = user32.NewProc("SetWindowLongPtrW")
	procShowWindow           = user32.NewProc("ShowWindow")
	procSendMessage          = user32.NewProc("SendMessageW")
	procCreateIconFromResourceEx = user32.NewProc("CreateIconFromResourceEx")
)

const (
	spiGetWorkArea  = 0x0030
	swpNoZOrder     = 0x0004
	swpFrameChanged = 0x0020
	wsBorder        = 0x00800000
	wsCaption       = 0x00C00000
	wsThickFrame    = 0x00040000
	swMinimize = 6
	swHide     = 0
	swShow     = 5
)

type rect struct {
	Left, Top, Right, Bottom int32
}

const (
	wmSetIcon    = 0x0080
	iconSmall    = 0
	iconBig      = 1
	lrDefaultColor = 0x00000000
)

// setWindowIcon sets the taskbar/window icon from an embedded ICO file.
func setWindowIcon(hwnd uintptr, icoData []byte) {
	// ICO header: 6 bytes, then 16 bytes per entry. First entry offset at byte 18 (4 bytes LE).
	if len(icoData) < 22 {
		return
	}
	// Read first entry: offset and size
	entrySize := uint32(icoData[14]) | uint32(icoData[15])<<8 | uint32(icoData[16])<<16 | uint32(icoData[17])<<24
	entryOffset := uint32(icoData[18]) | uint32(icoData[19])<<8 | uint32(icoData[20])<<16 | uint32(icoData[21])<<24

	if uint32(len(icoData)) < entryOffset+entrySize {
		return
	}

	bmpData := icoData[entryOffset : entryOffset+entrySize]
	hIcon, _, _ := procCreateIconFromResourceEx.Call(
		uintptr(unsafe.Pointer(&bmpData[0])),
		uintptr(entrySize),
		1,    // fIcon = TRUE
		0x00030000, // version
		32, 32, // desired size
		lrDefaultColor,
	)
	if hIcon != 0 {
		procSendMessage.Call(hwnd, wmSetIcon, iconSmall, hIcon)
		procSendMessage.Call(hwnd, wmSetIcon, iconBig, hIcon)
	}
}

// removeNativeTitlebar strips the Windows caption bar, keeping a thin border.
func removeNativeTitlebar(hwnd uintptr) {
	gwlStyleIdx := uintptr(0xFFFFFFFFFFFFFFF0) // GWL_STYLE = -16 as unsigned
	style, _, _ := procGetWindowLong.Call(hwnd, gwlStyleIdx)
	style &^= wsCaption    // remove title bar + border
	style &^= wsThickFrame // remove resize grip
	style |= wsBorder      // keep thin 1px border
	procSetWindowLong.Call(hwnd, gwlStyleIdx, style)
	// Apply style change: SWP_NOMOVE|SWP_NOSIZE|SWP_NOZORDER|SWP_FRAMECHANGED
	procSetWindowPos.Call(hwnd, 0, 0, 0, 0, 0, swpNoZOrder|swpFrameChanged|0x0001|0x0002)
}

// moveToBottomRight positions the window flush against the right edge and taskbar.
func moveToBottomRight(hwnd uintptr, w, h int) {
	var r rect
	procSystemParametersInfo.Call(spiGetWorkArea, 0, uintptr(unsafe.Pointer(&r)), 0)
	x := int(r.Right) - w
	y := int(r.Bottom) - h
	procSetWindowPos.Call(hwnd, 0, uintptr(x), uintptr(y), uintptr(w), uintptr(h), swpNoZOrder)
}

// openWebview opens the webview window and blocks until it is closed. Returns
// true iff the user requested application quit via ✕ (JS __close). The caller
// translates that into handleQuit.
//
// showCh signals "show + bring to front" (tray left-click on a hidden window,
// or menu "Ouvrir la fenêtre"). hideCh signals "hide to tray" (tray left-click
// on a visible window). reportHidden is invoked on every visibility change so
// the caller can keep its webviewHidden state in sync with handleTrayToggle.
//
// Rationale for ✕ = quit: the frontend shows a "Voulez-vous quitter ?"
// confirmation modal on first close (with a "Ne plus montrer" checkbox
// persisted in localStorage). Destroying then recreating the webview in the
// same process was attempted and produced blank windows on WebView2 — see
// feedback memory webview_lifecycle.
func openWebview(addr string,
	setTerminate func(func()),
	clearTerminate func(),
	showCh <-chan struct{},
	hideCh <-chan struct{},
	reportHidden func(bool),
) bool {
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

	hwnd := uintptr(unsafe.Pointer(w.Window()))

	// Open external links in the default browser.
	w.Bind("__openExternal", func(url string) {
		exec.Command("rundll32", "url.dll,FileProtocolHandler", url).Start()
	})

	// __minimize: hide window to tray (keep webview alive). Dispatched to the
	// webview main thread for consistency with the tray-toggle show/hide
	// goroutines below — Bind callbacks already run on that thread, but going
	// through Dispatch keeps the Win32 call site uniform. (Review L2.)
	w.Bind("__minimize", func() {
		w.Dispatch(func() {
			procShowWindow.Call(hwnd, swHide)
			reportHidden(true)
		})
	})

	// __close: user confirmed quit via the modal → terminate webview; the
	// caller (ui.handleOpenWebview goroutine) sees quitRequested=true and
	// invokes handleQuit for a full app shutdown.
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

	// Remove native titlebar, set icon, and position bottom-right.
	w.Dispatch(func() {
		removeNativeTitlebar(hwnd)
		setWindowIcon(hwnd, IconDefault)
		moveToBottomRight(hwnd, 420, 540)
	})

	// Listen for show / hide signals until the webview event loop exits.
	// A `done` channel closed by the outer defer tears down both goroutines
	// cleanly — prevents the orphan-goroutine-per-open-cycle leak that the
	// previous `for range showCh` pattern had.
	done := make(chan struct{})
	defer close(done)

	go func() {
		for {
			select {
			case <-done:
				return
			case <-showCh:
				if !alive.Load() {
					return
				}
				w.Dispatch(func() {
					procShowWindow.Call(hwnd, swShow)
					// SWP_NOMOVE|SWP_NOSIZE|SWP_SHOWWINDOW.
					procSetWindowPos.Call(hwnd, 0, 0, 0, 0, 0, swpNoZOrder|0x0001|0x0002|0x0040)
					reportHidden(false)
				})
			}
		}
	}()

	go func() {
		for {
			select {
			case <-done:
				return
			case <-hideCh:
				if !alive.Load() {
					return
				}
				w.Dispatch(func() {
					procShowWindow.Call(hwnd, swHide)
					reportHidden(true)
				})
			}
		}
	}()

	w.Navigate("http://" + addr + "/")
	w.Run()
	return quitRequested.Load()
}
