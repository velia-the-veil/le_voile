//go:build cgo

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

// openWebview opens the webview window. Returns true if user requested quit.
// showCh receives signals to show the window when hidden (from tray menu).
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

	hwnd := uintptr(unsafe.Pointer(w.Window()))

	// Open external links in the default browser.
	w.Bind("__openExternal", func(url string) {
		exec.Command("rundll32", "url.dll,FileProtocolHandler", url).Start()
	})

	// __minimize: hide window to tray (keep webview alive).
	w.Bind("__minimize", func() {
		procShowWindow.Call(hwnd, swHide)
	})

	// __close: quit the entire application.
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

	// Listen for show signals from tray menu.
	go func() {
		for range showCh {
			if alive.Load() {
				w.Dispatch(func() {
					procShowWindow.Call(hwnd, swShow)
					// Bring to foreground.
					procSetWindowPos.Call(hwnd, 0, 0, 0, 0, 0, swpNoZOrder|0x0001|0x0002|0x0040)
				})
			}
		}
	}()

	w.Navigate("http://" + addr + "/")
	w.Run()
	return quitRequested.Load()
}
