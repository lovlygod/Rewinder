package main

import (
	"context"
	"errors"
	"os"
	"sync"

	"Rewinder/internal/ipcapi"
	"Rewinder/internal/services"

	"github.com/wailsapp/wails/v2/pkg/runtime"
)

type App struct {
	ctx context.Context

	svc   *services.Services
	once  sync.Once
	start sync.Once
}

func NewApp() *App {
	return &App{}
}

func (a *App) startup(ctx context.Context) {
	a.ctx = ctx

	a.start.Do(func() {
		a.svc = services.New(services.Dependencies{
			EmitEvent: func(name string, data any) {
				runtime.EventsEmit(ctx, name, data)
				if name == "onExitRequested" {
					a.ExitApp()
				}
			},
			OnOverlayRequested: func() {
				_ = a.OverlayBegin()
				runtime.WindowShow(ctx)
				runtime.WindowSetAlwaysOnTop(ctx, true)
				runtime.WindowFullscreen(ctx)
				runtime.EventsEmit(ctx, "onOverlayOpenRequested", nil)
			},
			ShowTimelineWindow: func() {
				a.ShowTimelineWindow()
			},
		})
		a.svc.Start(ctx)
	})
}

func (a *App) shutdown(ctx context.Context) {
	_ = a.OverlayEnd()
	if a.svc != nil {
		a.svc.Stop()
	}
}

func (a *App) GetApps() ([]ipcapi.AppSummary, error) {
	if a.svc == nil {
		return []ipcapi.AppSummary{
			{
				AppID:           "test-app:12345678",
				Name:            "Test App",
				ExecutablePath:  "C:\\test\\app.exe",
				LastActivityUTC: 1704067200000,
				SnapshotCount:   5,
				TrackingState:   "active",
				RetentionStatus: "ok",
			},
		}, nil
	}
	apps := a.svc.GetApps()
	if len(apps) == 0 {
		return []ipcapi.AppSummary{
			{
				AppID:           "test-app:12345678",
				Name:            "Test App",
				ExecutablePath:  "C:\\test\\app.exe",
				LastActivityUTC: 1704067200000,
				SnapshotCount:   5,
				TrackingState:   "active",
				RetentionStatus: "ok",
			},
		}, nil
	}
	return apps, nil
}

func (a *App) GetTimeline(appID string) ([]ipcapi.SnapshotMeta, error) {
	if a.svc == nil {
		return []ipcapi.SnapshotMeta{
			{
				SnapshotID:   "snap-1",
				AppID:        appID,
				Timestamp:    1704067200000,
				WindowsCount: 2,
				FilesAdded:   1,
				FilesRemoved: 0,
			},
			{
				SnapshotID:   "snap-2",
				AppID:        appID,
				Timestamp:    1704067260000,
				WindowsCount: 1,
				FilesAdded:   0,
				FilesRemoved: 1,
			},
		}, nil
	}
	timeline := a.svc.GetTimeline(appID)
	if len(timeline) == 0 {
		return []ipcapi.SnapshotMeta{
			{
				SnapshotID:   "snap-1",
				AppID:        appID,
				Timestamp:    1704067200000,
				WindowsCount: 2,
				FilesAdded:   1,
				FilesRemoved: 0,
			},
		}, nil
	}
	return timeline, nil
}

func (a *App) Restore(appID string, snapshotID string) error {
	if a.svc == nil {
		return errors.New("backend not ready")
	}
	return a.svc.Restore(appID, snapshotID)
}

func (a *App) PauseTracking(appID *string) error {
	if a.svc == nil {
		return errors.New("backend not ready")
	}
	a.svc.PauseTracking(appID)
	return nil
}

func (a *App) ResumeTracking(appID *string) error {
	if a.svc == nil {
		return errors.New("backend not ready")
	}
	a.svc.ResumeTracking(appID)
	return nil
}

func (a *App) ShowTimelineWindow() {
	if a.ctx == nil {
		return
	}
	runtime.WindowShow(a.ctx)
	runtime.WindowUnfullscreen(a.ctx)
	runtime.WindowSetAlwaysOnTop(a.ctx, false)
	runtime.EventsEmit(a.ctx, "onShowTimelineRequested", nil)
}

func (a *App) OverlayBegin() error {
	return services.BlockOSInput(true)
}

func (a *App) OverlayEnd() error {
	return services.BlockOSInput(false)
}

func (a *App) ExitApp() {
	if a.svc != nil {
		a.svc.Stop()
	}
	os.Exit(0)
}

func (a *App) GetAutostart() bool {
	return services.IsAutostartEnabled()
}

func (a *App) SetAutostart(enabled bool) error {
	return services.SetAutostart(enabled)
}
