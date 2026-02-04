package services

import "golang.org/x/sys/windows"

var (
	user32         = windows.NewLazySystemDLL("user32.dll")
	procBlockInput = user32.NewProc("BlockInput")
)

func BlockOSInput(block bool) error {
	var v uintptr
	if block {
		v = 1
	}
	r1, _, e1 := procBlockInput.Call(v)
	if r1 == 0 {
		return e1
	}
	return nil
}
