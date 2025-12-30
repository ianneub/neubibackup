//go:build windows

package power

import (
	"log/slog"
	"syscall"
	"unsafe"
)

var (
	user32                  = syscall.NewLazyDLL("user32.dll")
	procRegisterClassExW    = user32.NewProc("RegisterClassExW")
	procCreateWindowExW     = user32.NewProc("CreateWindowExW")
	procDefWindowProcW      = user32.NewProc("DefWindowProcW")
	procGetMessageW         = user32.NewProc("GetMessageW")
	procTranslateMessage    = user32.NewProc("TranslateMessage")
	procDispatchMessageW    = user32.NewProc("DispatchMessageW")
	procDestroyWindow       = user32.NewProc("DestroyWindow")
	procPostQuitMessage     = user32.NewProc("PostQuitMessage")
)

const (
	WM_POWERBROADCAST      = 0x0218
	PBT_APMRESUMEAUTOMATIC = 0x0012
	PBT_APMRESUMESUSPEND   = 0x0007
)

type wndClassExW struct {
	size       uint32
	style      uint32
	wndProc    uintptr
	clsExtra   int32
	wndExtra   int32
	instance   syscall.Handle
	icon       syscall.Handle
	cursor     syscall.Handle
	background syscall.Handle
	menuName   *uint16
	className  *uint16
	iconSm     syscall.Handle
}

type msg struct {
	hwnd    syscall.Handle
	message uint32
	wParam  uintptr
	lParam  uintptr
	time    uint32
	pt      struct{ x, y int32 }
}

var globalWatcher *Watcher

// Start begins monitoring for system wake events on Windows.
func (w *Watcher) Start() {
	globalWatcher = w
	go w.messageLoop()
}

func (w *Watcher) messageLoop() {
	className, _ := syscall.UTF16PtrFromString("NeubiBackupPowerWatcher")

	wc := wndClassExW{
		size:      uint32(unsafe.Sizeof(wndClassExW{})),
		wndProc:   syscall.NewCallback(wndProc),
		className: className,
	}

	procRegisterClassExW.Call(uintptr(unsafe.Pointer(&wc)))

	hwnd, _, _ := procCreateWindowExW.Call(
		0,
		uintptr(unsafe.Pointer(className)),
		0,
		0,
		0, 0, 0, 0,
		0,
		0,
		0,
		0,
	)

	if hwnd == 0 {
		slog.Error("Failed to create power watcher window")
		return
	}

	var m msg
	for {
		select {
		case <-w.stop:
			procDestroyWindow.Call(hwnd)
			procPostQuitMessage.Call(0)
			return
		default:
			ret, _, _ := procGetMessageW.Call(
				uintptr(unsafe.Pointer(&m)),
				0,
				0,
				0,
			)
			if ret == 0 {
				return
			}
			procTranslateMessage.Call(uintptr(unsafe.Pointer(&m)))
			procDispatchMessageW.Call(uintptr(unsafe.Pointer(&m)))
		}
	}
}

func wndProc(hwnd syscall.Handle, msg uint32, wParam, lParam uintptr) uintptr {
	if msg == WM_POWERBROADCAST {
		if wParam == PBT_APMRESUMEAUTOMATIC || wParam == PBT_APMRESUMESUSPEND {
			slog.Info("Wake from sleep detected")
			if globalWatcher != nil && globalWatcher.callback != nil {
				globalWatcher.callback()
			}
		}
	}

	ret, _, _ := procDefWindowProcW.Call(
		uintptr(hwnd),
		uintptr(msg),
		wParam,
		lParam,
	)
	return ret
}
