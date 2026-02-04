//go:build windows

package events

import (
	"sync"
)

type windowsSources struct {
	emit   func(SystemEvent)
	stopCh <-chan struct{}

	wev *winEventHook
	hot *clipboardListener

	wg sync.WaitGroup
}

func newWindowsSources(emit func(SystemEvent), stopCh <-chan struct{}) (*windowsSources, error) {
	return &windowsSources{emit: emit, stopCh: stopCh}, nil
}

func (w *windowsSources) start() error {
	wev, err := newWinEventHook(w.emit)
	if err == nil {
		w.wev = wev
	}
	clip, err2 := newClipboardListener(w.emit)
	if err2 == nil {
		w.hot = clip
	}

	w.wg.Add(1)
	go func() {
		defer w.wg.Done()
		w.runWMIProcessWatcher()
	}()

	if w.wev != nil {
		w.wg.Add(1)
		go func() {
			defer w.wg.Done()
			w.wev.Run(w.stopCh)
		}()
	}
	if w.hot != nil {
		w.wg.Add(1)
		go func() {
			defer w.wg.Done()
			w.hot.Run(w.stopCh)
		}()
	}
	return nil
}

func (w *windowsSources) runWMIProcessWatcher() {
	runProcessWMIWatcher(w.emit, w.stopCh)
}
