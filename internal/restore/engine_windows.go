package restore

import (
	"errors"
	"fmt"
	"time"
	"unsafe"

	"Rewinder/internal/plugins"
	"Rewinder/internal/snapshot"
	"Rewinder/internal/state"

	"golang.org/x/sys/windows"
)

type ProgressFn func(stage string, percent int, msg string)

type Engine struct{}

func NewEngine() *Engine { return &Engine{} }

func (e *Engine) RestoreSnapshot(progress ProgressFn, snap *snapshot.Snapshot, full *snapshot.FullSnapshot) error {
	if full == nil {
		return errors.New("no snapshot state")
	}
	app := full.App
	plugins.DefaultRegistry().Restore(&app)
	progress("ensure_process", 15, "Ensuring process exists")

	pid := app.PID
	if pid <= 0 || !processExists(pid) {
		npid, err := relaunch(app.ExecutablePath, app.CommandLine, app.WorkingDir)
		if err != nil {
			return err
		}
		pid = npid
		progress("wait_windows", 35, "Waiting for main window")
		_ = waitForAnyWindow(pid, 5*time.Second)
	}

	progress("restore_windows", 60, "Restoring window positions")
	if err := restoreWindows(pid, app.Windows); err != nil {
		return err
	}

	progress("restore_focus", 90, "Restoring focus")
	restoreFocus(pid, app.Windows)
	return nil
}

func processExists(pid int) bool {
	h, err := windows.OpenProcess(windows.PROCESS_QUERY_LIMITED_INFORMATION, false, uint32(pid))
	if err != nil {
		return false
	}
	_ = windows.CloseHandle(h)
	return true
}

func relaunch(exePath, cmdLine, workingDir string) (int, error) {
	if exePath == "" && cmdLine == "" {
		return 0, errors.New("missing executable path/command line")
	}

	app := exePath
	args := cmdLine
	if args == "" {
		args = fmt.Sprintf("\"%s\"", exePath)
	}

	var si windows.StartupInfo
	var pi windows.ProcessInformation
	si.Cb = uint32(unsafe.Sizeof(si))

	var wd *uint16
	if workingDir != "" {
		wd, _ = windows.UTF16PtrFromString(workingDir)
	}
	cl, _ := windows.UTF16PtrFromString(args)
	var appPtr *uint16
	if app != "" {
		appPtr, _ = windows.UTF16PtrFromString(app)
	}
	err := windows.CreateProcess(appPtr, cl, nil, nil, false, 0, nil, wd, &si, &pi)
	if err != nil {
		return 0, err
	}
	_ = windows.CloseHandle(pi.Thread)
	_ = windows.CloseHandle(pi.Process)
	return int(pi.ProcessId), nil
}

func waitForAnyWindow(pid int, timeout time.Duration) bool {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if hasAnyWindow(pid) {
			return true
		}
		time.Sleep(100 * time.Millisecond)
	}
	return false
}

func hasAnyWindow(pid int) bool {
	found := false
	cb := windows.NewCallback(func(hwnd uintptr, lParam uintptr) uintptr {
		if int(getWindowPID(hwnd)) == pid {
			found = true
			return 0
		}
		return 1
	})
	_, _, _ = procEnumWindows.Call(cb, 0)
	return found
}

func restoreWindows(pid int, windowsSaved []state.WindowState) error {
	current := enumerateWindowsForPID(pid)
	if len(current) == 0 {
		return nil
	}

	for _, sw := range windowsSaved {
		target := sw.HWND
		if target != 0 && isWindow(target) && int(getWindowPID(target)) == pid {
			applyWindowState(target, sw)
			continue
		}
		best := findBestWindow(current, sw)
		if best != 0 {
			applyWindowState(best, sw)
		}
	}

	sortByZ(windowsSaved)
	for _, sw := range windowsSaved {
		h := sw.HWND
		if h != 0 && isWindow(h) && int(getWindowPID(h)) == pid {
			_, _, _ = procBringWindowToTop.Call(h)
		}
	}

	return nil
}

func restoreFocus(pid int, saved []state.WindowState) {
	for _, sw := range saved {
		if sw.IsForeground {
			h := sw.HWND
			if h != 0 && isWindow(h) && int(getWindowPID(h)) == pid {
				_, _, _ = procSetForegroundWindow.Call(h)
				return
			}
		}
	}
	for _, h := range enumerateWindowsForPID(pid) {
		_, _, _ = procSetForegroundWindow.Call(h)
		return
	}
}

func enumerateWindowsForPID(pid int) []uintptr {
	var out []uintptr
	cb := windows.NewCallback(func(hwnd uintptr, lParam uintptr) uintptr {
		if int(getWindowPID(hwnd)) == pid {
			out = append(out, hwnd)
		}
		return 1
	})
	_, _, _ = procEnumWindows.Call(cb, 0)
	return out
}

func findBestWindow(current []uintptr, sw state.WindowState) uintptr {
	for _, h := range current {
		if getClassName(h) == sw.ClassName && getTitle(h) == sw.Title {
			return h
		}
	}
	for _, h := range current {
		if getClassName(h) == sw.ClassName {
			return h
		}
	}
	if len(current) > 0 {
		return current[0]
	}
	return 0
}

func applyWindowState(hwnd uintptr, sw state.WindowState) {
	if sw.IsMinimized {
		_, _, _ = procShowWindow.Call(hwnd, SW_SHOWMINIMIZED)
	} else if sw.IsMaximized {
		_, _, _ = procShowWindow.Call(hwnd, SW_SHOWMAXIMIZED)
	} else {
		_, _, _ = procShowWindow.Call(hwnd, SW_SHOWNORMAL)
	}

	r := sw.Rect
	_, _, _ = procMoveWindow.Call(hwnd, uintptr(r.Left), uintptr(r.Top), uintptr(r.Right-r.Left), uintptr(r.Bottom-r.Top), 1)
}

func sortByZ(ws []state.WindowState) {
	for i := 1; i < len(ws); i++ {
		j := i
		for j > 0 && ws[j-1].ZOrder > ws[j].ZOrder {
			ws[j-1], ws[j] = ws[j], ws[j-1]
			j--
		}
	}
}

var (
	user32                       = windows.NewLazySystemDLL("user32.dll")
	procEnumWindows              = user32.NewProc("EnumWindows")
	procGetWindowThreadProcessId = user32.NewProc("GetWindowThreadProcessId")
	procIsWindow                 = user32.NewProc("IsWindow")
	procMoveWindow               = user32.NewProc("MoveWindow")
	procShowWindow               = user32.NewProc("ShowWindow")
	procSetForegroundWindow      = user32.NewProc("SetForegroundWindow")
	procBringWindowToTop         = user32.NewProc("BringWindowToTop")
	procGetClassNameW            = user32.NewProc("GetClassNameW")
	procGetWindowTextW           = user32.NewProc("GetWindowTextW")
	procGetWindowTextLengthW     = user32.NewProc("GetWindowTextLengthW")
)

const (
	SW_SHOWNORMAL    = 1
	SW_SHOWMINIMIZED = 2
	SW_SHOWMAXIMIZED = 3
)

func getWindowPID(hwnd uintptr) uint32 {
	var pid uint32
	_, _, _ = procGetWindowThreadProcessId.Call(hwnd, uintptr(unsafe.Pointer(&pid)))
	return pid
}

func isWindow(hwnd uintptr) bool {
	r1, _, _ := procIsWindow.Call(hwnd)
	return r1 != 0
}

func getClassName(hwnd uintptr) string {
	buf := make([]uint16, 256)
	r1, _, _ := procGetClassNameW.Call(hwnd, uintptr(unsafe.Pointer(&buf[0])), uintptr(len(buf)))
	if r1 == 0 {
		return ""
	}
	return windows.UTF16ToString(buf[:r1])
}

func getTitle(hwnd uintptr) string {
	r1, _, _ := procGetWindowTextLengthW.Call(hwnd)
	n := int(r1)
	if n <= 0 {
		return ""
	}
	buf := make([]uint16, n+1)
	r2, _, _ := procGetWindowTextW.Call(hwnd, uintptr(unsafe.Pointer(&buf[0])), uintptr(len(buf)))
	if r2 == 0 {
		return ""
	}
	return windows.UTF16ToString(buf[:r2])
}
