package events

import (
	"errors"
	"sync"
)

type EventType string

const (
	EventForegroundChanged EventType = "foreground_changed"
	EventWindowMoved       EventType = "window_moved"
	EventWindowDestroyed   EventType = "window_destroyed"
	EventWindowShown       EventType = "window_shown"
	EventWindowHidden      EventType = "window_hidden"
	EventProcessStarted    EventType = "process_started"
	EventProcessExited     EventType = "process_exited"
	EventClipboardChanged  EventType = "clipboard_changed"
)

type SystemEvent struct {
	Type      EventType      `json:"type"`
	Timestamp int64          `json:"timestampUTC"`
	PID       int            `json:"pid"`
	HWND      uintptr        `json:"hwnd"`
	Metadata  map[string]any `json:"metadata,omitempty"`
}

type Bus struct {
	ch     chan SystemEvent
	stopCh chan struct{}
	once   sync.Once

	win *windowsSources
}

func NewBus(buffer int) *Bus {
	return &Bus{
		ch:     make(chan SystemEvent, buffer),
		stopCh: make(chan struct{}),
	}
}

func (b *Bus) Events() <-chan SystemEvent { return b.ch }

func (b *Bus) Emit(ev SystemEvent) {
	select {
	case b.ch <- ev:
	default:
	}
}

func (b *Bus) StartWindowsSources() error {
	if b.win != nil {
		return nil
	}
	ws, err := newWindowsSources(b.Emit, b.stopCh)
	if err != nil {
		return err
	}
	b.win = ws
	return ws.start()
}

func (b *Bus) Stop() {
	b.once.Do(func() {
		close(b.stopCh)
	})
}

var ErrNotSupported = errors.New("not supported")
