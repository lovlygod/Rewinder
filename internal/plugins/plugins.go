package plugins

import "Rewinder/internal/state"

type AppPlugin interface {
	ID() string
	CanHandle(app *state.AppState) bool
	Capture(app *state.AppState) error
	Restore(app *state.AppState) error
}

type Registry struct {
	list []AppPlugin
}

func DefaultRegistry() *Registry {
	return &Registry{
		list: []AppPlugin{
			NewVSCodePlugin(),
			NewChromePlugin(),
			NewOfficePlugin(),
		},
	}
}

func (r *Registry) Capture(app *state.AppState) {
	if app.PluginData == nil {
		app.PluginData = map[string]any{}
	}
	for _, p := range r.list {
		if !p.CanHandle(app) {
			continue
		}
		_ = p.Capture(app)
	}
}

func (r *Registry) Restore(app *state.AppState) {
	for _, p := range r.list {
		if !p.CanHandle(app) {
			continue
		}
		_ = p.Restore(app)
	}
}
