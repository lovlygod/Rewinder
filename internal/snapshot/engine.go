package snapshot

import (
	"bytes"
	"compress/gzip"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"Rewinder/internal/ipcapi"
	"Rewinder/internal/state"

	"github.com/google/uuid"
)

type EngineConfig struct {
	MaxSnapshotsPerApp int
	MaxRAMBytes        int64
	MaxDiskBytes       int64
	Retention          time.Duration
	StorageDir         string
}

type Engine struct {
	cfg EngineConfig

	mu   sync.RWMutex
	apps map[string]*appTimeline
}

type appTimeline struct {
	appID string
	exe   string
	name  string

	lastActivity time.Time

	snapshots []Snapshot
	ramBytes  int64
}

type Snapshot struct {
	SnapshotID     string     `json:"snapshotID"`
	AppID          string     `json:"appID"`
	BaseSnapshotID *string    `json:"baseSnapshotID,omitempty"`
	Delta          StateDelta `json:"delta"`
	Timestamp      time.Time  `json:"timestamp"`

	Spilled bool   `json:"spilled"`
	DiskRef string `json:"diskRef,omitempty"`
}

type StateDelta struct {
	WindowsChanged   bool            `json:"windowsChanged"`
	WindowDiffs      []WindowDiff    `json:"windowDiffs,omitempty"`
	FilesAdded       []state.FileRef `json:"filesAdded,omitempty"`
	FilesRemoved     []state.FileRef `json:"filesRemoved,omitempty"`
	ClipboardChanged bool            `json:"clipboardChanged"`
	PluginChanged    bool            `json:"pluginChanged"`
	PluginData       map[string]any  `json:"pluginData,omitempty"`
}

type WindowDiff struct {
	HWND   uintptr            `json:"hwnd"`
	Before *state.WindowState `json:"before,omitempty"`
	After  *state.WindowState `json:"after,omitempty"`
}

type FullSnapshot struct {
	App state.AppState `json:"app"`
}

func NewEngine(cfg EngineConfig) *Engine {
	if cfg.MaxSnapshotsPerApp <= 0 {
		cfg.MaxSnapshotsPerApp = 500
	}
	if cfg.Retention <= 0 {
		cfg.Retention = 24 * time.Hour
	}
	_ = os.MkdirAll(cfg.StorageDir, 0o755)
	return &Engine{cfg: cfg, apps: map[string]*appTimeline{}}
}

func (e *Engine) Close() error { return nil }

func (e *Engine) GetApps() []ipcapi.AppSummary {
	fmt.Printf("[DEBUG] GetApps called\n")
	e.mu.RLock()
	defer e.mu.RUnlock()
	var out []ipcapi.AppSummary
	for _, tl := range e.apps {
		out = append(out, ipcapi.AppSummary{
			AppID:           tl.appID,
			Name:            tl.name,
			ExecutablePath:  tl.exe,
			LastActivityUTC: tl.lastActivity.UTC().UnixMilli(),
			SnapshotCount:   len(tl.snapshots),
			TrackingState:   "active",
			RetentionStatus: "ok",
		})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].LastActivityUTC > out[j].LastActivityUTC })
	fmt.Printf("[DEBUG] Returning %d apps\n", len(out))
	return out
}

func (e *Engine) GetTimeline(appID string) []ipcapi.SnapshotMeta {
	e.mu.RLock()
	defer e.mu.RUnlock()
	tl := e.apps[appID]
	if tl == nil {
		return nil
	}
	out := make([]ipcapi.SnapshotMeta, 0, len(tl.snapshots))
	for _, s := range tl.snapshots {
		out = append(out, ipcapi.SnapshotMeta{
			SnapshotID:   s.SnapshotID,
			AppID:        s.AppID,
			Timestamp:    s.Timestamp.UTC().UnixMilli(),
			WindowsCount: len(s.Delta.WindowDiffs),
			FilesAdded:   len(s.Delta.FilesAdded),
			FilesRemoved: len(s.Delta.FilesRemoved),
		})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Timestamp > out[j].Timestamp })
	return out
}

func (e *Engine) Ingest(app *state.AppState) (*ipcapi.SnapshotMeta, error) {
	fmt.Printf("[DEBUG] Ingest called for app %s (PID: %d)\n", app.AppID, app.PID)
	e.mu.Lock()
	defer e.mu.Unlock()

	tl := e.apps[app.AppID]
	if tl == nil {
		tl = &appTimeline{
			appID: app.AppID,
			exe:   app.ExecutablePath,
			name:  filepath.Base(app.ExecutablePath),
		}
		e.apps[app.AppID] = tl
		fmt.Printf("[DEBUG] New app timeline created for %s\n", app.AppID)
	}
	tl.lastActivity = app.Timestamp
	if app.ExecutablePath != "" {
		tl.exe = app.ExecutablePath
		tl.name = filepath.Base(app.ExecutablePath)
	}

	e.applyRetentionLocked(tl)

	var base *FullSnapshot
	if len(tl.snapshots) == 0 {
		fmt.Printf("[DEBUG] First snapshot for app %s - creating\n", app.AppID)
		base = &FullSnapshot{App: *app}
	} else {
		_, full, err := e.resolveSnapshotLocked(tl, tl.snapshots[len(tl.snapshots)-1].SnapshotID)
		if err == nil {
			base = full
		} else {
			base = &FullSnapshot{App: *app}
		}
	}

	delta := diffStates(&base.App, app)
	fmt.Printf("[DEBUG] Delta: windows=%v, clipboard=%v, files+=%d, files-=%d, plugin=%v\n",
		delta.WindowsChanged, delta.ClipboardChanged, len(delta.FilesAdded), len(delta.FilesRemoved), delta.PluginChanged)

	if len(tl.snapshots) > 0 && !delta.WindowsChanged && !delta.ClipboardChanged && len(delta.FilesAdded) == 0 && len(delta.FilesRemoved) == 0 && !delta.PluginChanged {
		fmt.Printf("[DEBUG] No changes detected, skipping snapshot\n")
		return nil, nil
	}

	sid := uuid.NewString()
	var baseID *string
	if len(tl.snapshots) > 0 {
		b := tl.snapshots[len(tl.snapshots)-1].SnapshotID
		baseID = &b
	}

	snap := Snapshot{
		SnapshotID:     sid,
		AppID:          app.AppID,
		BaseSnapshotID: baseID,
		Delta:          delta,
		Timestamp:      app.Timestamp,
	}

	if len(tl.snapshots)%30 == 0 {
		ref, err := e.spillFullSnapshotLocked(app.AppID, &FullSnapshot{App: *app})
		if err == nil {
			snap.Spilled = true
			snap.DiskRef = ref
			snap.BaseSnapshotID = nil
		}
	}

	tl.snapshots = append(tl.snapshots, snap)
	fmt.Printf("[DEBUG] Snapshot created: %s for app %s (total snapshots: %d)\n", sid, app.AppID, len(tl.snapshots))

	if len(tl.snapshots) > e.cfg.MaxSnapshotsPerApp {
		tl.snapshots = tl.snapshots[len(tl.snapshots)-e.cfg.MaxSnapshotsPerApp:]
	}

	return &ipcapi.SnapshotMeta{
		SnapshotID:   sid,
		AppID:        app.AppID,
		Timestamp:    app.Timestamp.UTC().UnixMilli(),
		WindowsCount: len(app.Windows),
		FilesAdded:   len(delta.FilesAdded),
		FilesRemoved: len(delta.FilesRemoved),
	}, nil
}

func (e *Engine) ResolveSnapshot(appID, snapshotID string) (*Snapshot, *FullSnapshot, error) {
	e.mu.RLock()
	defer e.mu.RUnlock()
	tl := e.apps[appID]
	if tl == nil {
		return nil, nil, errors.New("unknown app")
	}
	s, full, err := e.resolveSnapshotLocked(tl, snapshotID)
	return s, full, err
}

func (e *Engine) resolveSnapshotLocked(tl *appTimeline, snapshotID string) (*Snapshot, *FullSnapshot, error) {
	var idx = -1
	for i := range tl.snapshots {
		if tl.snapshots[i].SnapshotID == snapshotID {
			idx = i
			break
		}
	}
	if idx == -1 {
		return nil, nil, errors.New("snapshot not found")
	}

	var chain []Snapshot
	for i := idx; i >= 0; i-- {
		chain = append(chain, tl.snapshots[i])
		if tl.snapshots[i].Spilled && tl.snapshots[i].DiskRef != "" && tl.snapshots[i].BaseSnapshotID == nil {
			break
		}
		if tl.snapshots[i].BaseSnapshotID == nil && i == 0 {
			break
		}
	}

	var base *FullSnapshot
	last := chain[len(chain)-1]
	if last.Spilled && last.DiskRef != "" && last.BaseSnapshotID == nil {
		fs, err := e.loadFullSnapshotLocked(last.DiskRef)
		if err != nil {
			return nil, nil, err
		}
		base = fs
	} else {
		base = &FullSnapshot{App: state.AppState{AppID: tl.appID, ExecutablePath: tl.exe}}
	}

	for i := len(chain) - 1; i >= 0; i-- {
		applyDelta(&base.App, chain[i].Delta)
	}

	sel := tl.snapshots[idx]
	return &sel, base, nil
}

func (e *Engine) applyRetentionLocked(tl *appTimeline) {
	if e.cfg.Retention <= 0 {
		return
	}
	cut := time.Now().Add(-e.cfg.Retention)
	var kept []Snapshot
	for _, s := range tl.snapshots {
		if s.Timestamp.After(cut) {
			kept = append(kept, s)
		}
	}
	tl.snapshots = kept
}

func diffStates(prev *state.AppState, next *state.AppState) StateDelta {
	fmt.Printf("[DEBUG] diffStates: prev windows=%d, next windows=%d\n", len(prev.Windows), len(next.Windows))
	prevW := map[uintptr]state.WindowState{}
	for _, w := range prev.Windows {
		prevW[w.HWND] = w
	}
	nextW := map[uintptr]state.WindowState{}
	for _, w := range next.Windows {
		nextW[w.HWND] = w
	}

	var diffs []WindowDiff
	for hwnd, nw := range nextW {
		pw, ok := prevW[hwnd]
		if !ok {
			cp := nw
			diffs = append(diffs, WindowDiff{HWND: hwnd, Before: nil, After: &cp})
			continue
		}
		if !windowEq(&pw, &nw) {
			before := pw
			after := nw
			diffs = append(diffs, WindowDiff{HWND: hwnd, Before: &before, After: &after})
		}
	}
	for hwnd, pw := range prevW {
		if _, ok := nextW[hwnd]; !ok {
			before := pw
			diffs = append(diffs, WindowDiff{HWND: hwnd, Before: &before, After: nil})
		}
	}
	fmt.Printf("[DEBUG] diffStates: found %d window diffs\n", len(diffs))

	prevF := map[string]struct{}{}
	for _, f := range prev.OpenFiles {
		prevF[stringsToLower(f.Path)] = struct{}{}
	}
	nextF := map[string]struct{}{}
	for _, f := range next.OpenFiles {
		nextF[stringsToLower(f.Path)] = struct{}{}
	}
	var added []state.FileRef
	for p := range nextF {
		if _, ok := prevF[p]; !ok {
			added = append(added, state.FileRef{Path: p})
		}
	}
	var removed []state.FileRef
	for p := range prevF {
		if _, ok := nextF[p]; !ok {
			removed = append(removed, state.FileRef{Path: p})
		}
	}

	clipChanged := prev.ClipboardHash != "" && next.ClipboardHash != "" && prev.ClipboardHash != next.ClipboardHash

	pluginChanged := !jsonEq(prev.PluginData, next.PluginData)

	return StateDelta{
		WindowsChanged:   len(diffs) > 0,
		WindowDiffs:      diffs,
		FilesAdded:       added,
		FilesRemoved:     removed,
		ClipboardChanged: clipChanged,
		PluginChanged:    pluginChanged,
		PluginData:       next.PluginData,
	}
}

func applyDelta(app *state.AppState, d StateDelta) {
	if d.WindowsChanged {
		m := map[uintptr]state.WindowState{}
		for _, w := range app.Windows {
			m[w.HWND] = w
		}
		for _, wd := range d.WindowDiffs {
			if wd.After == nil {
				delete(m, wd.HWND)
				continue
			}
			m[wd.HWND] = *wd.After
		}
		var ws []state.WindowState
		for _, w := range m {
			ws = append(ws, w)
		}
		sort.Slice(ws, func(i, j int) bool { return ws[i].ZOrder < ws[j].ZOrder })
		app.Windows = ws
	}
	if len(d.FilesAdded) > 0 || len(d.FilesRemoved) > 0 {
		m := map[string]state.FileRef{}
		for _, f := range app.OpenFiles {
			m[stringsToLower(f.Path)] = f
		}
		for _, f := range d.FilesRemoved {
			delete(m, stringsToLower(f.Path))
		}
		for _, f := range d.FilesAdded {
			m[stringsToLower(f.Path)] = f
		}
		var fs []state.FileRef
		for _, f := range m {
			fs = append(fs, f)
		}
		sort.Slice(fs, func(i, j int) bool { return fs[i].Path < fs[j].Path })
		app.OpenFiles = fs
	}
	if d.ClipboardChanged {
	}
	if d.PluginChanged {
		app.PluginData = d.PluginData
	}
}

func windowEq(a, b *state.WindowState) bool {
	return a.Rect == b.Rect &&
		a.MonitorID == b.MonitorID &&
		a.ZOrder == b.ZOrder &&
		a.IsForeground == b.IsForeground &&
		a.IsMinimized == b.IsMinimized &&
		a.IsMaximized == b.IsMaximized &&
		a.VirtualDesktop == b.VirtualDesktop
}

func stringsToLower(s string) string {
	return strings.ToLower(strings.TrimSpace(s))
}

func jsonEq(a, b any) bool {
	aj, _ := json.Marshal(a)
	bj, _ := json.Marshal(b)
	return bytes.Equal(aj, bj)
}

func (e *Engine) spillFullSnapshotLocked(appID string, fs *FullSnapshot) (string, error) {
	_ = os.MkdirAll(e.cfg.StorageDir, 0o755)
	raw, err := json.Marshal(fs)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(raw)
	name := hex.EncodeToString(sum[:16]) + ".json.gz"
	path := filepath.Join(e.cfg.StorageDir, appID, name)
	_ = os.MkdirAll(filepath.Dir(path), 0o755)

	var buf bytes.Buffer
	zw := gzip.NewWriter(&buf)
	_, _ = zw.Write(raw)
	_ = zw.Close()
	if err := os.WriteFile(path, buf.Bytes(), 0o644); err != nil {
		return "", err
	}
	return filepath.ToSlash(filepath.Join(appID, name)), nil
}

func (e *Engine) loadFullSnapshotLocked(ref string) (*FullSnapshot, error) {
	path := filepath.Join(e.cfg.StorageDir, filepath.FromSlash(ref))
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	zr, err := gzip.NewReader(bytes.NewReader(b))
	if err != nil {
		return nil, err
	}
	defer zr.Close()
	raw, err := ioReadAllLimit(zr, 10<<20) // 10MB guard
	if err != nil {
		return nil, err
	}
	var fs FullSnapshot
	if err := json.Unmarshal(raw, &fs); err != nil {
		return nil, err
	}
	return &fs, nil
}

func ioReadAllLimit(r *gzip.Reader, limit int64) ([]byte, error) {
	var buf bytes.Buffer
	if _, err := buf.ReadFrom(io.LimitReader(r, limit)); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}
