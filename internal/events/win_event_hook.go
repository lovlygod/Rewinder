package events

import (
	"time"
	"unsafe"

	"golang.org/x/sys/windows"
)

type winEventHook struct {
	emit func(SystemEvent)

	hHook windows.Handle
	cb    uintptr
}

func newWinEventHook(emit func(SystemEvent)) (*winEventHook, error) {
	w := &winEventHook{emit: emit}
	w.cb = windows.NewCallback(w.callback)

	h, err := setWinEventHook(
		EVENT_SYSTEM_FOREGROUND,
		EVENT_OBJECT_LOCATIONCHANGE,
		0,
		w.cb,
		0,
		0,
		WINEVENT_OUTOFCONTEXT|WINEVENT_SKIPOWNPROCESS,
	)
	if err != nil {
		return nil, err
	}
	w.hHook = h
	return w, nil
}

func (w *winEventHook) Run(stopCh <-chan struct{}) {
	var msg MSG
	for {
		select {
		case <-stopCh:
			if w.hHook != 0 {
				_ = unhookWinEvent(w.hHook)
			}
			return
		default:
			ret, _ := peekMessage(&msg, 0, 0, 0, PM_REMOVE)
			if ret {
				translateMessage(&msg)
				dispatchMessage(&msg)
			} else {
				time.Sleep(10 * time.Millisecond)
			}
		}
	}
}

func (w *winEventHook) callback(hWinEventHook windows.Handle, event uint32, hwnd uintptr, idObject int32, idChild int32, dwEventThread uint32, dwmsEventTime uint32) uintptr {
	_ = hWinEventHook
	_ = idChild
	_ = dwEventThread

	if idObject != OBJID_WINDOW {
		return 0
	}

	ts := time.Now().UTC().UnixMilli()
	pid := int(getWindowPID(hwnd))
	switch event {
	case EVENT_SYSTEM_FOREGROUND:
		w.emit(SystemEvent{Type: EventForegroundChanged, Timestamp: ts, PID: pid, HWND: hwnd})
	case EVENT_OBJECT_LOCATIONCHANGE:
		w.emit(SystemEvent{Type: EventWindowMoved, Timestamp: ts, PID: pid, HWND: hwnd})
	case EVENT_OBJECT_DESTROY:
		w.emit(SystemEvent{Type: EventWindowDestroyed, Timestamp: ts, PID: pid, HWND: hwnd})
	case EVENT_OBJECT_SHOW:
		w.emit(SystemEvent{Type: EventWindowShown, Timestamp: ts, PID: pid, HWND: hwnd})
	case EVENT_OBJECT_HIDE:
		w.emit(SystemEvent{Type: EventWindowHidden, Timestamp: ts, PID: pid, HWND: hwnd})
	}
	return 0
}

const (
	EVENT_SYSTEM_FOREGROUND     = 0x0003
	EVENT_OBJECT_SHOW           = 0x8002
	EVENT_OBJECT_HIDE           = 0x8003
	EVENT_OBJECT_DESTROY        = 0x8001
	EVENT_OBJECT_LOCATIONCHANGE = 0x800B
	OBJID_WINDOW                = 0
	WINEVENT_OUTOFCONTEXT       = 0x0000
	WINEVENT_SKIPOWNPROCESS     = 0x0002
	PM_REMOVE                   = 0x0001
)

var (
	user32                 = windows.NewLazySystemDLL("user32.dll")
	procSetWinEventHook    = user32.NewProc("SetWinEventHook")
	procUnhookWinEvent     = user32.NewProc("UnhookWinEvent")
	procGetWindowThreadPID = user32.NewProc("GetWindowThreadProcessId")
	procPeekMessageW       = user32.NewProc("PeekMessageW")
	procTranslateMessage   = user32.NewProc("TranslateMessage")
	procDispatchMessageW   = user32.NewProc("DispatchMessageW")
)

func setWinEventHook(eventMin, eventMax uint32, hmodWinEventHook windows.Handle, pfnWinEventProc uintptr, idProcess, idThread uint32, dwFlags uint32) (windows.Handle, error) {
	r1, _, e1 := procSetWinEventHook.Call(
		uintptr(eventMin),
		uintptr(eventMax),
		uintptr(hmodWinEventHook),
		pfnWinEventProc,
		uintptr(idProcess),
		uintptr(idThread),
		uintptr(dwFlags),
	)
	if r1 == 0 {
		return 0, e1
	}
	return windows.Handle(r1), nil
}

func unhookWinEvent(h windows.Handle) error {
	r1, _, e1 := procUnhookWinEvent.Call(uintptr(h))
	if r1 == 0 {
		return e1
	}
	return nil
}

func getWindowPID(hwnd uintptr) uint32 {
	var pid uint32
	_, _, _ = procGetWindowThreadPID.Call(hwnd, uintptr(unsafe.Pointer(&pid)))
	return pid
}

func peekMessage(msg *MSG, hwnd uintptr, msgFilterMin, msgFilterMax uint32, removeMsg uint32) (bool, error) {
	r1, _, e1 := procPeekMessageW.Call(
		uintptr(unsafe.Pointer(msg)),
		hwnd,
		uintptr(msgFilterMin),
		uintptr(msgFilterMax),
		uintptr(removeMsg),
	)
	if r1 == 0 {
		return false, nil
	}
	return true, e1
}

func translateMessage(msg *MSG) {
	_, _, _ = procTranslateMessage.Call(uintptr(unsafe.Pointer(msg)))
}

func dispatchMessage(msg *MSG) {
	_, _, _ = procDispatchMessageW.Call(uintptr(unsafe.Pointer(msg)))
}
