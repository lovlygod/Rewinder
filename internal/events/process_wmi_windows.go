package events

import (
	"fmt"
	"time"

	"github.com/go-ole/go-ole"
	"github.com/go-ole/go-ole/oleutil"
)

func runProcessWMIWatcher(emit func(SystemEvent), stopCh <-chan struct{}) {
	go wmiTraceLoop("Win32_ProcessStartTrace", func(pid int, name string) {
		emit(SystemEvent{
			Type:      EventProcessStarted,
			Timestamp: time.Now().UTC().UnixMilli(),
			PID:       pid,
			Metadata: map[string]any{
				"name": name,
			},
		})
	}, stopCh)

	go wmiTraceLoop("Win32_ProcessStopTrace", func(pid int, name string) {
		emit(SystemEvent{
			Type:      EventProcessExited,
			Timestamp: time.Now().UTC().UnixMilli(),
			PID:       pid,
			Metadata: map[string]any{
				"name": name,
			},
		})
	}, stopCh)
}

func wmiTraceLoop(className string, onEvent func(pid int, name string), stopCh <-chan struct{}) {
	_ = ole.CoInitializeEx(0, ole.COINIT_MULTITHREADED)
	defer ole.CoUninitialize()

	locatorObj, err := oleutil.CreateObject("WbemScripting.SWbemLocator")
	if err != nil {
		return
	}
	defer locatorObj.Release()

	locator, err := locatorObj.QueryInterface(ole.IID_IDispatch)
	if err != nil {
		return
	}
	defer locator.Release()

	svcRaw, err := oleutil.CallMethod(locator, "ConnectServer", nil, "root\\cimv2")
	if err != nil {
		return
	}
	svc := svcRaw.ToIDispatch()
	defer svc.Release()

	query := fmt.Sprintf("SELECT * FROM %s", className)
	srcRaw, err := oleutil.CallMethod(svc, "ExecNotificationQuery", query)
	if err != nil {
		return
	}
	src := srcRaw.ToIDispatch()
	defer src.Release()

	for {
		select {
		case <-stopCh:
			return
		default:
			evRaw, err := oleutil.CallMethod(src, "NextEvent", 1000)
			if err != nil {
				continue
			}
			ev := evRaw.ToIDispatch()
			if ev == nil {
				continue
			}

			pidV, _ := oleutil.GetProperty(ev, "ProcessID")
			nameV, _ := oleutil.GetProperty(ev, "ProcessName")
			pid := int(pidV.Val)
			name := ""
			if nameV != nil {
				name = nameV.ToString()
			}
			if pid > 0 {
				onEvent(pid, name)
			}
			ev.Release()
		}
	}
}
