package services

import (
	"context"
	"sync"

	"Rewinder/internal/events"
	"Rewinder/internal/ipcapi"
	"Rewinder/internal/plugins"
	"Rewinder/internal/policy"
	"Rewinder/internal/restore"
	"Rewinder/internal/snapshot"
	"Rewinder/internal/state"
	"Rewinder/internal/trayhotkey"
)

type Dependencies struct {
	EmitEvent          func(name string, data any)
	OnOverlayRequested func()
	ShowTimelineWindow func()
}

type Services struct {
	deps Dependencies

	cfgMu sync.RWMutex
	cfg   *policy.Config

	ev  *events.Bus
	cap *state.CaptureEngine
	ss  *snapshot.Engine
	rs  *restore.Engine
	th  *trayhotkey.Manager
	pl  *plugins.Registry

	trackingMu sync.RWMutex
	trackingOn bool
	appPaused  map[string]bool
	stopOnce   sync.Once
	stopCh     chan struct{}
}

func New(deps Dependencies) *Services {
	cfg := policy.DefaultConfig()
	bus := events.NewBus(1024)
	ss := snapshot.NewEngine(snapshot.EngineConfig{
		MaxSnapshotsPerApp: cfg.ResourceLimits.MaxSnapshotsPerApp,
		MaxRAMBytes:        cfg.ResourceLimits.MaxRAMBytes,
		MaxDiskBytes:       cfg.ResourceLimits.MaxDiskBytes,
		Retention:          cfg.Retention,
		StorageDir:         cfg.StorageDir,
	})

	return &Services{
		deps:       deps,
		cfg:        cfg,
		ev:         bus,
		cap:        state.NewCaptureEngine(),
		ss:         ss,
		rs:         restore.NewEngine(),
		pl:         plugins.DefaultRegistry(),
		trackingOn: true,
		appPaused:  map[string]bool{},
		stopCh:     make(chan struct{}),
	}
}

func (s *Services) Start(ctx context.Context) {
	s.th = trayhotkey.NewManager(trayhotkey.Dependencies{
		OnOpenTimeline: func() {
			s.deps.EmitEvent("onShowTimelineRequested", nil)
			if s.deps.ShowTimelineWindow != nil {
				s.deps.ShowTimelineWindow()
			}
		},
		OnPauseTracking: func() {
			s.PauseTracking(nil)
		},
		OnResumeTracking: func() {
			s.ResumeTracking(nil)
		},
		OnOpenSettings: func() {
			s.deps.EmitEvent("onShowSettingsRequested", nil)
		},
		OnExit: func() {
			s.Stop()
			s.deps.EmitEvent("onExitRequested", nil)
		},
		OnOverlayHotkey: func() {
			if s.deps.OnOverlayRequested != nil {
				s.deps.OnOverlayRequested()
			}
		},
	})
	s.th.Start()

	go func() {
		_ = s.ev.StartWindowsSources()
	}()

	go s.captureLoop()

	s.deps.EmitEvent("onTrackingStateChanged", ipcapi.TrackingStateChangedEvent{
		AppID:  nil,
		State:  "active",
		Reason: "",
		AtUTC:  ipcapi.NowUTC(),
	})
}

func (s *Services) Stop() {
	s.stopOnce.Do(func() {
		close(s.stopCh)
		if s.th != nil {
			s.th.Stop()
		}
		if s.ev != nil {
			s.ev.Stop()
		}
		if s.ss != nil {
			_ = s.ss.Close()
		}
	})
}

func (s *Services) PauseTracking(appID *string) {
	s.trackingMu.Lock()
	defer s.trackingMu.Unlock()
	if appID == nil {
		s.trackingOn = false
		s.deps.EmitEvent("onTrackingStateChanged", ipcapi.TrackingStateChangedEvent{
			AppID:  nil,
			State:  "paused",
			Reason: "paused by user",
			AtUTC:  ipcapi.NowUTC(),
		})
		return
	}
	s.appPaused[*appID] = true
	s.deps.EmitEvent("onTrackingStateChanged", ipcapi.TrackingStateChangedEvent{
		AppID:  appID,
		State:  "paused",
		Reason: "paused by user",
		AtUTC:  ipcapi.NowUTC(),
	})
}

func (s *Services) ResumeTracking(appID *string) {
	s.trackingMu.Lock()
	defer s.trackingMu.Unlock()
	if appID == nil {
		s.trackingOn = true
		s.deps.EmitEvent("onTrackingStateChanged", ipcapi.TrackingStateChangedEvent{
			AppID:  nil,
			State:  "active",
			Reason: "resumed by user",
			AtUTC:  ipcapi.NowUTC(),
		})
		return
	}
	delete(s.appPaused, *appID)
	s.deps.EmitEvent("onTrackingStateChanged", ipcapi.TrackingStateChangedEvent{
		AppID:  appID,
		State:  "active",
		Reason: "resumed by user",
		AtUTC:  ipcapi.NowUTC(),
	})
}

func (s *Services) GetApps() []ipcapi.AppSummary {
	return s.ss.GetApps()
}

func (s *Services) GetTimeline(appID string) []ipcapi.SnapshotMeta {
	return s.ss.GetTimeline(appID)
}

func (s *Services) Restore(appID string, snapshotID string) error {
	s.deps.EmitEvent("onRestoreProgress", ipcapi.RestoreProgressEvent{
		AppID:      appID,
		SnapshotID: snapshotID,
		Stage:      "resolve",
		Percent:    5,
		Message:    "Resolving snapshot",
	})
	snap, full, err := s.ss.ResolveSnapshot(appID, snapshotID)
	if err != nil {
		s.deps.EmitEvent("onRestoreError", ipcapi.RestoreErrorEvent{AppID: appID, SnapshotID: snapshotID, Error: err.Error()})
		return err
	}

	progress := func(stage string, percent int, msg string) {
		s.deps.EmitEvent("onRestoreProgress", ipcapi.RestoreProgressEvent{
			AppID:      appID,
			SnapshotID: snapshotID,
			Stage:      stage,
			Percent:    percent,
			Message:    msg,
		})
	}

	err = s.rs.RestoreSnapshot(progress, snap, full)
	if err != nil {
		s.deps.EmitEvent("onRestoreError", ipcapi.RestoreErrorEvent{AppID: appID, SnapshotID: snapshotID, Error: err.Error()})
		return err
	}
	progress("done", 100, "Restore completed")
	return nil
}

func (s *Services) captureLoop() {
	for {
		select {
		case <-s.stopCh:
			return
		case ev := <-s.ev.Events():
			s.handleSystemEvent(ev)
		}
	}
}

func (s *Services) shouldTrack(appID string, exePath string, windowClass string) bool {
	s.trackingMu.RLock()
	on := s.trackingOn
	paused := s.appPaused[appID]
	s.trackingMu.RUnlock()
	if !on || paused {
		return false
	}
	s.cfgMu.RLock()
	defer s.cfgMu.RUnlock()
	return s.cfg.Rules.Allow(exePath, windowClass)
}

func (s *Services) handleSystemEvent(ev events.SystemEvent) {
	switch ev.Type {
	case events.EventForegroundChanged, events.EventWindowMoved, events.EventWindowShown, events.EventWindowHidden, events.EventProcessStarted, events.EventProcessExited:
		s.captureForeground()
	case events.EventClipboardChanged:
		s.captureForeground()
	default:
	}
}

func (s *Services) captureForeground() {
	app, err := s.cap.CaptureForeground()
	if err != nil {
		return
	}
	if s.pl != nil {
		s.pl.Capture(app)
	}
	if !s.shouldTrack(app.AppID, app.ExecutablePath, app.ForegroundWindowClass) {
		return
	}
	meta, err := s.ss.Ingest(app)
	if err != nil {
		return
	}
	if meta != nil {
		s.deps.EmitEvent("onSnapshotCreated", ipcapi.SnapshotCreatedEvent{
			AppID:      app.AppID,
			Snapshot:   *meta,
			OccurredAt: ipcapi.NowUTC(),
		})
	}
}
