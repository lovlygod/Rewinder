package events

import (
	"time"
	"unsafe"

	"golang.org/x/sys/windows"
)

type clipboardListener struct {
	emit func(SystemEvent)

	hwnd uintptr
}

func newClipboardListener(emit func(SystemEvent)) (*clipboardListener, error) {
	return &clipboardListener{emit: emit}, nil
}

func (c *clipboardListener) Run(stopCh <-chan struct{}) {
	clsName, _ := windows.UTF16PtrFromString("AppTimeMachineClipboardListener")
	hInstance := getModuleHandle()

	wndProc := windows.NewCallback(func(hwnd uintptr, msg uint32, wParam, lParam uintptr) uintptr {
		switch msg {
		case WM_CLIPBOARDUPDATE:
			c.emit(SystemEvent{Type: EventClipboardChanged, Timestamp: time.Now().UTC().UnixMilli()})
			return 0
		case WM_DESTROY:
			postQuitMessage(0)
			return 0
		default:
			return defWindowProc(hwnd, msg, wParam, lParam)
		}
	})

	var wc WNDCLASSEXW
	wc.CbSize = uint32(unsafe.Sizeof(wc))
	wc.LpfnWndProc = wndProc
	wc.HInstance = hInstance
	wc.LpszClassName = clsName
	_, _ = registerClassEx(&wc)

	hwnd := createWindowEx(
		0,
		clsName,
		clsName,
		0,
		0, 0, 0, 0,
		HWND_MESSAGE,
		0,
		hInstance,
		0,
	)
	if hwnd == 0 {
		return
	}
	c.hwnd = hwnd

	_ = addClipboardFormatListener(hwnd)

	var msg MSG
	for {
		select {
		case <-stopCh:
			_ = removeClipboardFormatListener(hwnd)
			_ = destroyWindow(hwnd)
			return
		default:
			ret, _ := getMessage(&msg, 0, 0, 0)
			if !ret {
				return
			}
			translateMessage(&msg)
			dispatchMessage(&msg)
		}
	}
}

const (
	WM_DESTROY                 = 0x0002
	WM_CLIPBOARDUPDATE         = 0x031D
	HWND_MESSAGE       uintptr = ^uintptr(2)
)

type WNDCLASSEXW struct {
	CbSize        uint32
	Style         uint32
	LpfnWndProc   uintptr
	CbClsExtra    int32
	CbWndExtra    int32
	HInstance     windows.Handle
	HIcon         windows.Handle
	HCursor       windows.Handle
	HbrBackground windows.Handle
	LpszMenuName  *uint16
	LpszClassName *uint16
	HIconSm       windows.Handle
}

var (
	procRegisterClassExW              = user32.NewProc("RegisterClassExW")
	procCreateWindowExW               = user32.NewProc("CreateWindowExW")
	procDefWindowProcW                = user32.NewProc("DefWindowProcW")
	procGetMessageW                   = user32.NewProc("GetMessageW")
	procPostQuitMessage               = user32.NewProc("PostQuitMessage")
	procDestroyWindow                 = user32.NewProc("DestroyWindow")
	procAddClipboardFormatListener    = user32.NewProc("AddClipboardFormatListener")
	procRemoveClipboardFormatListener = user32.NewProc("RemoveClipboardFormatListener")
)

func registerClassEx(wcx *WNDCLASSEXW) (uint16, error) {
	r1, _, e1 := procRegisterClassExW.Call(uintptr(unsafe.Pointer(wcx)))
	return uint16(r1), e1
}

func createWindowEx(exStyle uint32, className, windowName *uint16, style uint32, x, y, w, h int32, parent uintptr, menu uintptr, instance windows.Handle, param uintptr) uintptr {
	r1, _, _ := procCreateWindowExW.Call(
		uintptr(exStyle),
		uintptr(unsafe.Pointer(className)),
		uintptr(unsafe.Pointer(windowName)),
		uintptr(style),
		uintptr(x),
		uintptr(y),
		uintptr(w),
		uintptr(h),
		parent,
		menu,
		uintptr(instance),
		param,
	)
	return r1
}

func defWindowProc(hwnd uintptr, msg uint32, wParam, lParam uintptr) uintptr {
	r1, _, _ := procDefWindowProcW.Call(hwnd, uintptr(msg), wParam, lParam)
	return r1
}

func getMessage(msg *MSG, hwnd uintptr, min, max uint32) (bool, error) {
	r1, _, e1 := procGetMessageW.Call(uintptr(unsafe.Pointer(msg)), hwnd, uintptr(min), uintptr(max))
	if int32(r1) == -1 {
		return false, e1
	}
	return r1 != 0, nil
}

func postQuitMessage(code int32) {
	_, _, _ = procPostQuitMessage.Call(uintptr(code))
}

func destroyWindow(hwnd uintptr) error {
	r1, _, e1 := procDestroyWindow.Call(hwnd)
	if r1 == 0 {
		return e1
	}
	return nil
}

func addClipboardFormatListener(hwnd uintptr) error {
	r1, _, e1 := procAddClipboardFormatListener.Call(hwnd)
	if r1 == 0 {
		return e1
	}
	return nil
}

func removeClipboardFormatListener(hwnd uintptr) error {
	r1, _, e1 := procRemoveClipboardFormatListener.Call(hwnd)
	if r1 == 0 {
		return e1
	}
	return nil
}

var (
	k32                  = windows.NewLazySystemDLL("kernel32.dll")
	procGetModuleHandleW = k32.NewProc("GetModuleHandleW")
)

func getModuleHandle() windows.Handle {
	r1, _, _ := procGetModuleHandleW.Call(0)
	return windows.Handle(r1)
}
