package plugins

import (
	"path/filepath"
	"regexp"
	"strings"

	"Rewinder/internal/state"
)

type VSCodePlugin struct{}

func NewVSCodePlugin() *VSCodePlugin { return &VSCodePlugin{} }

func (p *VSCodePlugin) ID() string { return "vscode" }

func (p *VSCodePlugin) CanHandle(app *state.AppState) bool {
	base := strings.ToLower(filepath.Base(app.ExecutablePath))
	return base == "code.exe" || base == "code - insiders.exe" || strings.Contains(base, "code")
}

func (p *VSCodePlugin) Capture(app *state.AppState) error {
	cmd := app.CommandLine
	if cmd == "" {
		return nil
	}
	paths := extractWindowsPaths(cmd)
	if len(paths) == 0 {
		return nil
	}
	app.PluginData[p.ID()] = map[string]any{
		"paths": paths,
	}
	return nil
}

func (p *VSCodePlugin) Restore(app *state.AppState) error {
	v, ok := app.PluginData[p.ID()].(map[string]any)
	if !ok {
		return nil
	}
	raw, ok := v["paths"].([]string)
	if ok && len(raw) > 0 && app.CommandLine == "" && app.ExecutablePath != "" {
		// Construct command line: "Code.exe" <paths...>
		parts := []string{quote(app.ExecutablePath)}
		for _, p := range raw {
			parts = append(parts, quote(p))
		}
		app.CommandLine = strings.Join(parts, " ")
	}
	return nil
}

var reWinPath = regexp.MustCompile(`(?i)([a-z]:\\[^"'\s]+)`)

func extractWindowsPaths(s string) []string {
	m := reWinPath.FindAllString(s, -1)
	seen := map[string]struct{}{}
	var out []string
	for _, p := range m {
		p = filepath.Clean(p)
		low := strings.ToLower(p)
		if _, ok := seen[low]; ok {
			continue
		}
		seen[low] = struct{}{}
		out = append(out, p)
	}
	return out
}

func quote(s string) string {
	if strings.ContainsAny(s, " \t") {
		return `"` + s + `"`
	}
	return s
}
