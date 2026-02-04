package plugins

import (
	"path/filepath"
	"strings"

	"Rewinder/internal/state"
)

type ChromePlugin struct{}

func NewChromePlugin() *ChromePlugin { return &ChromePlugin{} }

func (p *ChromePlugin) ID() string { return "chrome" }

func (p *ChromePlugin) CanHandle(app *state.AppState) bool {
	base := strings.ToLower(filepath.Base(app.ExecutablePath))
	return base == "chrome.exe" || base == "msedge.exe" || base == "brave.exe"
}

func (p *ChromePlugin) Capture(app *state.AppState) error {
	title := ""
	for _, w := range app.Windows {
		if w.IsForeground {
			title = w.Title
			break
		}
	}
	cmd := app.CommandLine
	profile := extractFlagValue(cmd, "--user-data-dir")
	app.PluginData[p.ID()] = map[string]any{
		"activeTitle": title,
		"userDataDir": profile,
		"browser":     strings.ToLower(filepath.Base(app.ExecutablePath)),
	}
	return nil
}

func (p *ChromePlugin) Restore(app *state.AppState) error {
	if app.CommandLine == "" {
		return nil
	}
	if !strings.Contains(app.CommandLine, "--restore-last-session") {
		app.CommandLine = app.CommandLine + " --restore-last-session"
	}
	return nil
}

func extractFlagValue(cmd string, flag string) string {
	i := strings.Index(cmd, flag)
	if i == -1 {
		return ""
	}
	rest := strings.TrimSpace(cmd[i+len(flag):])
	if strings.HasPrefix(rest, "=") {
		rest = strings.TrimSpace(rest[1:])
	}
	if strings.HasPrefix(rest, "\"") {
		rest = rest[1:]
		j := strings.Index(rest, "\"")
		if j == -1 {
			return ""
		}
		return rest[:j]
	}
	for k := 0; k < len(rest); k++ {
		if rest[k] == ' ' || rest[k] == '\t' {
			return rest[:k]
		}
	}
	return rest
}
