package state

import "time"

type Rect struct {
	Left   int32 `json:"left"`
	Top    int32 `json:"top"`
	Right  int32 `json:"right"`
	Bottom int32 `json:"bottom"`
}

type WindowState struct {
	HWND           uintptr `json:"hwnd"`
	Rect           Rect    `json:"rect"`
	MonitorID      string  `json:"monitorID"`
	ZOrder         int     `json:"zOrder"`
	IsForeground   bool    `json:"isForeground"`
	IsMinimized    bool    `json:"isMinimized"`
	IsMaximized    bool    `json:"isMaximized"`
	VirtualDesktop string  `json:"virtualDesktop,omitempty"`
	ClassName      string  `json:"className,omitempty"`
	Title          string  `json:"title,omitempty"`
}

type FileRef struct {
	Path string `json:"path"`
}

type InputState struct {
	InputLanguage string `json:"inputLanguage,omitempty"`
}

type AppState struct {
	AppID          string `json:"appID"`
	PID            int    `json:"pid"`
	ExecutablePath string `json:"executablePath"`
	CommandLine    string `json:"commandLine,omitempty"`
	WorkingDir     string `json:"workingDir,omitempty"`

	ForegroundWindowClass string        `json:"foregroundWindowClass,omitempty"`
	Windows               []WindowState `json:"windows"`
	OpenFiles             []FileRef     `json:"openFiles"`
	ClipboardHash         string        `json:"clipboardHash,omitempty"`
	PluginData            map[string]any `json:"pluginData,omitempty"`
	InputState            InputState    `json:"inputState"`
	Timestamp             time.Time     `json:"timestamp"`
}

