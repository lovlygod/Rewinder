//go:build windows

package state

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"time"
	"unsafe"

	"github.com/StackExchange/wmi"
	"golang.org/x/sys/windows"
)

type CaptureEngine struct {
	wmiCache *wmiProcCache
}

func NewCaptureEngine() *CaptureEngine {
	return &CaptureEngine{wmiCache: newWMIProcCache()}
}

func (c *CaptureEngine) CaptureForeground() (*AppState, error) {
	hwnd := getForegroundWindow()
	if hwnd == 0 {
		return nil, errors.New("no foreground window")
	}
	pid := int(getWindowPID(hwnd))
	if pid <= 0 {
		return nil, errors.New("no pid")
	}

	exe, cmd, wd := c.wmiCache.Lookup(pid)
	if exe == "" {
		exe = queryFullProcessImageName(pid)
	}
	appID := stableAppID(exe)
	class := getWindowClassName(hwnd)

	wins := enumerateWindows(pid, hwnd)
	files := enumerateOpenFilesBestEffort(pid)

	clipHash := hashClipboardTextBestEffort()

	st := &AppState{
		AppID:                 appID,
		PID:                   pid,
		ExecutablePath:        exe,
		CommandLine:           cmd,
		WorkingDir:            wd,
		ForegroundWindowClass: class,
		Windows:               wins,
		OpenFiles:             files,
		ClipboardHash:         clipHash,
		InputState:            InputState{InputLanguage: getInputLanguageTag()},
		Timestamp:             time.Now(),
	}
	return st, nil
}

func stableAppID(exePath string) string {
	if exePath == "" {
		return "unknown"
	}
	base := strings.ToLower(filepath.Base(exePath))
	sum := sha256.Sum256([]byte(strings.ToLower(exePath)))
	return fmt.Sprintf("%s:%s", base, hex.EncodeToString(sum[:8]))
}

type wmiProcCache struct {
	last map[int]cachedProc
}
type cachedProc struct {
	exe string
	cmd string
	wd  string
	at  time.Time
}

func newWMIProcCache() *wmiProcCache { return &wmiProcCache{last: map[int]cachedProc{}} }

func (c *wmiProcCache) Lookup(pid int) (exe string, cmd string, wd string) {
	if v, ok := c.last[pid]; ok && time.Since(v.at) < 30*time.Second {
		return v.exe, v.cmd, v.wd
	}

	type Win32_Process struct {
		ProcessID      uint32
		ExecutablePath *string
		CommandLine    *string
	}
	var dst []Win32_Process
	q := fmt.Sprintf("WHERE ProcessID=%d", pid)
	err := wmi.Query("SELECT ProcessID, ExecutablePath, CommandLine FROM Win32_Process "+q, &dst)
	if err != nil || len(dst) == 0 {
		// Возвращаем пустые значения без сохранения в кэш, если не удалось получить данные
		return "", "", ""
	}
	var p = dst[0]
	if p.ExecutablePath != nil {
		exe = *p.ExecutablePath
	}
	if p.CommandLine != nil {
		cmd = *p.CommandLine
	}
	if exe != "" {
		wd = filepath.Dir(exe)
	}
	c.last[pid] = cachedProc{exe: exe, cmd: cmd, wd: wd, at: time.Now()}
	return exe, cmd, wd
}

// Добавляем функцию для очистки устаревших записей в кэше
func (c *wmiProcCache) Cleanup() {
	now := time.Now()
	for pid, proc := range c.last {
		if now.Sub(proc.at) > 5*time.Minute {
			delete(c.last, pid)
		}
	}
}

var (
	u32                            = windows.NewLazySystemDLL("user32.dll")
	k32                            = windows.NewLazySystemDLL("kernel32.dll")
	procGetForegroundWindow        = u32.NewProc("GetForegroundWindow")
	procGetWindowThreadProcessId   = u32.NewProc("GetWindowThreadProcessId")
	procEnumWindows                = u32.NewProc("EnumWindows")
	procGetWindowRect              = u32.NewProc("GetWindowRect")
	procGetWindowPlacement         = u32.NewProc("GetWindowPlacement")
	procIsIconic                   = u32.NewProc("IsIconic")
	procGetClassNameW              = u32.NewProc("GetClassNameW")
	procGetWindowTextW             = u32.NewProc("GetWindowTextW")
	procGetWindowTextLengthW       = u32.NewProc("GetWindowTextLengthW")
	procGetWindow                  = u32.NewProc("GetWindow")
	procMonitorFromWindow          = u32.NewProc("MonitorFromWindow")
	procGetMonitorInfoW            = u32.NewProc("GetMonitorInfoW")
	procGetKeyboardLayout          = u32.NewProc("GetKeyboardLayout")
	procOpenClipboard              = u32.NewProc("OpenClipboard")
	procCloseClipboard             = u32.NewProc("CloseClipboard")
	procGetClipboardData           = u32.NewProc("GetClipboardData")
	procGlobalLock                 = k32.NewProc("GlobalLock")
	procGlobalUnlock               = k32.NewProc("GlobalUnlock")
	procQueryFullProcessImageNameW = k32.NewProc("QueryFullProcessImageNameW")
	procOpenProcess                = k32.NewProc("OpenProcess")
	procCloseHandle                = k32.NewProc("CloseHandle")
)

const (
	CF_UNICODETEXT                    = 13
	PROCESS_QUERY_LIMITED_INFORMATION = 0x1000
	GW_HWNDPREV                       = 3
	MONITOR_DEFAULTTONEAREST          = 2
	SW_SHOWMINIMIZED                  = 2
	SW_SHOWMAXIMIZED                  = 3
)

type RECT struct {
	Left, Top, Right, Bottom int32
}

type POINT struct {
	X, Y int32
}

type WINDOWPLACEMENT struct {
	Length           uint32
	Flags            uint32
	ShowCmd          uint32
	PtMinPosition    POINT
	PtMaxPosition    POINT
	RcNormalPosition RECT
}

type MONITORINFOEXW struct {
	CbSize    uint32
	RcMonitor RECT
	RcWork    RECT
	DwFlags   uint32
	SzDevice  [32]uint16
}

func getForegroundWindow() uintptr {
	r1, _, _ := procGetForegroundWindow.Call()
	return r1
}

func getWindowPID(hwnd uintptr) uint32 {
	var pid uint32
	_, _, _ = procGetWindowThreadProcessId.Call(hwnd, uintptr(unsafe.Pointer(&pid)))
	return pid
}

func queryFullProcessImageName(pid int) string {
	h, _, _ := procOpenProcess.Call(PROCESS_QUERY_LIMITED_INFORMATION, 0, uintptr(uint32(pid)))
	if h == 0 {
		return ""
	}
	defer procCloseHandle.Call(h)
	buf := make([]uint16, 32768)
	sz := uint32(len(buf))
	_, _, _ = procQueryFullProcessImageNameW.Call(h, 0, uintptr(unsafe.Pointer(&buf[0])), uintptr(unsafe.Pointer(&sz)))
	if sz == 0 {
		return ""
	}
	return windows.UTF16ToString(buf[:sz])
}

func getWindowClassName(hwnd uintptr) string {
	buf := make([]uint16, 256)
	r1, _, _ := procGetClassNameW.Call(hwnd, uintptr(unsafe.Pointer(&buf[0])), uintptr(len(buf)))
	if r1 == 0 {
		return ""
	}
	return windows.UTF16ToString(buf[:r1])
}

func getWindowTitle(hwnd uintptr) string {
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

func enumerateWindows(pid int, foreground uintptr) []WindowState {
	var out []WindowState
	cb := windows.NewCallback(func(hwnd uintptr, lParam uintptr) uintptr {
		if int(getWindowPID(hwnd)) != pid {
			return 1
		}
		var r RECT
		_, _, _ = procGetWindowRect.Call(hwnd, uintptr(unsafe.Pointer(&r)))

		var wp WINDOWPLACEMENT
		wp.Length = uint32(unsafe.Sizeof(wp))
		_, _, _ = procGetWindowPlacement.Call(hwnd, uintptr(unsafe.Pointer(&wp)))

		isMin := wp.ShowCmd == SW_SHOWMINIMIZED
		isMax := wp.ShowCmd == SW_SHOWMAXIMIZED

		// Ограничиваем подсчет Z-порядка, чтобы избежать чрезмерной нагрузки
		z := 0
		cur := hwnd
		for iterations := 0; iterations < 100; iterations++ { // Ограничиваем количество итераций
			prev, _, _ := procGetWindow.Call(cur, GW_HWNDPREV)
			if prev == 0 {
				break
			}
			z++
			cur = prev
		}

		monID := monitorIDFromWindow(hwnd)
		out = append(out, WindowState{
			HWND:         hwnd,
			Rect:         Rect{Left: r.Left, Top: r.Top, Right: r.Right, Bottom: r.Bottom},
			MonitorID:    monID,
			ZOrder:       z,
			IsForeground: hwnd == foreground,
			IsMinimized:  isMin,
			IsMaximized:  isMax,
			ClassName:    getWindowClassName(hwnd),
			Title:        getWindowTitle(hwnd),
		})
		return 1
	})
	_, _, _ = procEnumWindows.Call(cb, 0)
	return out
}

func monitorIDFromWindow(hwnd uintptr) string {
	hmon, _, _ := procMonitorFromWindow.Call(hwnd, MONITOR_DEFAULTTONEAREST)
	if hmon == 0 {
		return ""
	}
	var mi MONITORINFOEXW
	mi.CbSize = uint32(unsafe.Sizeof(mi))
	r1, _, _ := procGetMonitorInfoW.Call(hmon, uintptr(unsafe.Pointer(&mi)))
	if r1 == 0 {
		return ""
	}
	return windows.UTF16ToString(mi.SzDevice[:])
}

func getInputLanguageTag() string {
	hkl, _, _ := procGetKeyboardLayout.Call(0)
	langID := uint16(hkl & 0xffff)
	return fmt.Sprintf("0x%04x", langID)
}

func hashClipboardTextBestEffort() string {
	r1, _, _ := procOpenClipboard.Call(0)
	if r1 == 0 {
		return ""
	}
	defer procCloseClipboard.Call()
	h, _, _ := procGetClipboardData.Call(CF_UNICODETEXT)
	if h == 0 {
		return ""
	}
	ptr, _, _ := procGlobalLock.Call(h)
	if ptr == 0 {
		return ""
	}
	defer procGlobalUnlock.Call(h)
	u16 := (*[1 << 20]uint16)(unsafe.Pointer(ptr)) // ~1M
	var b []uint16
	for i := 0; i < len(u16) && i < 8192; i++ {
		if u16[i] == 0 {
			break
		}
		b = append(b, u16[i])
	}
	if len(b) == 0 {
		return ""
	}
	s := windows.UTF16ToString(b)
	sum := sha256.Sum256([]byte(s))
	return hex.EncodeToString(sum[:16])
}
