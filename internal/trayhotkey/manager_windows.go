package trayhotkey

import (
	_ "embed"
	"os"
	"path/filepath"
	"sync"
	"time"
	"unsafe"

	"github.com/getlantern/systray"
	"golang.org/x/sys/windows"
)

var trayIcon []byte

type Dependencies struct {
	OnOpenTimeline   func()
	OnPauseTracking  func()
	OnResumeTracking func()
	OnOpenSettings   func()
	OnExit           func()
	OnOverlayHotkey  func()
}

type Manager struct {
	deps Dependencies
	once sync.Once
	stop chan struct{}
}

func (m *Manager) setTrayIcon() {
	if len(trayIcon) > 0 {
		systray.SetIcon(trayIcon)
		return
	}

	exePath, err := os.Executable()
	if err != nil {
		return
	}

	exeDir := filepath.Dir(exePath)
	iconPaths := []string{
		filepath.Join(exeDir, "icon.ico"),
		filepath.Join(exeDir, "..", "icon.ico"),
		filepath.Join(exeDir, "build", "windows", "icon.ico"),
		filepath.Join(exeDir, "..", "build", "windows", "icon.ico"),
	}

	for _, iconPath := range iconPaths {
		iconPath = filepath.Clean(iconPath)
		if _, err := os.Stat(iconPath); err == nil {
			iconData, err := os.ReadFile(iconPath)
			if err != nil {
				continue
			}
			systray.SetIcon(iconData)
			return
		}
	}
}

func NewManager(deps Dependencies) *Manager {
	return &Manager{deps: deps, stop: make(chan struct{})}
}

func (m *Manager) Start() {
	m.once.Do(func() {
		go systray.Run(m.onReady, m.onExit)
		go m.hotkeyLoop()
	})
}

func (m *Manager) Stop() {
	select {
	case <-m.stop:
	default:
		close(m.stop)
	}
	systray.Quit()
}

func (m *Manager) onReady() {
	systray.SetTitle("Rewinder")
	systray.SetTooltip("App Time Machine")

	m.setTrayIcon()

	itemOpen := systray.AddMenuItem("Open Timeline", "Open timeline window")
	itemPause := systray.AddMenuItem("Pause Tracking", "Pause tracking")
	itemResume := systray.AddMenuItem("Resume Tracking", "Resume tracking")
	itemSettings := systray.AddMenuItem("Settings", "Open settings")
	systray.AddSeparator()
	itemExit := systray.AddMenuItem("Exit", "Exit")

	go func() {
		for {
			select {
			case <-m.stop:
				return
			case <-itemOpen.ClickedCh:
				if m.deps.OnOpenTimeline != nil {
					m.deps.OnOpenTimeline()
				}
			case <-itemPause.ClickedCh:
				if m.deps.OnPauseTracking != nil {
					m.deps.OnPauseTracking()
				}
			case <-itemResume.ClickedCh:
				if m.deps.OnResumeTracking != nil {
					m.deps.OnResumeTracking()
				}
			case <-itemSettings.ClickedCh:
				if m.deps.OnOpenSettings != nil {
					m.deps.OnOpenSettings()
				}
			case <-itemExit.ClickedCh:
				if m.deps.OnExit != nil {
					m.deps.OnExit()
				}
				m.Stop()
				return
			}
		}
	}()
}

func (m *Manager) onExit() {}

func (m *Manager) hotkeyLoop() {
	const (
		MOD_ALT     = 0x0001
		MOD_CONTROL = 0x0002
		WM_HOTKEY   = 0x0312
	)
	user32 := windows.NewLazySystemDLL("user32.dll")
	procRegisterHotKey := user32.NewProc("RegisterHotKey")
	procUnregisterHotKey := user32.NewProc("UnregisterHotKey")
	procGetMessageW := user32.NewProc("GetMessageW")

	const hotkeyID = 0xA11
	_, _, _ = procRegisterHotKey.Call(0, hotkeyID, MOD_CONTROL|MOD_ALT, 0x5A)
	defer procUnregisterHotKey.Call(0, hotkeyID)

	var msg MSG
	for {
		select {
		case <-m.stop:
			return
		default:
			r1, _, _ := procGetMessageW.Call(uintptr(unsafe.Pointer(&msg)), 0, 0, 0)
			if int32(r1) <= 0 {
				time.Sleep(50 * time.Millisecond)
				continue
			}
			if msg.Message == WM_HOTKEY && msg.WParam == hotkeyID {
				if m.deps.OnOverlayHotkey != nil {
					m.deps.OnOverlayHotkey()
				}
			}
		}
	}
}

type POINT struct {
	X int32
	Y int32
}

type MSG struct {
	HWnd    uintptr
	Message uint32
	WParam  uintptr
	LParam  uintptr
	Time    uint32
	Pt      POINT
}
