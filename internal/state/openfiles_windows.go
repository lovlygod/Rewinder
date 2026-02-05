//go:build windows

package state

import (
	"path/filepath"
	"strings"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"
)

func enumerateOpenFilesBestEffort(pid int) []FileRef {
	start := time.Now()
	out := map[string]struct{}{}

	// Увеличиваем таймаут для оптимизации производительности
	handles, err := querySystemHandles()
	if err != nil {
		return nil
	}

	hProc, err := windows.OpenProcess(windows.PROCESS_DUP_HANDLE|windows.PROCESS_QUERY_LIMITED_INFORMATION, false, uint32(pid))
	if err != nil {
		return nil
	}
	defer windows.CloseHandle(hProc)

	// Ограничиваем количество обрабатываемых дескрипторов для снижения нагрузки
	processedCount := 0
	for _, h := range handles {
		if int(h.UniqueProcessID) != pid {
			continue
		}

		// Увеличиваем таймаут и ограничиваем количество обрабатываемых дескрипторов
		if time.Since(start) > 300*time.Millisecond || processedCount > 50 {
			break
		}

		var dup windows.Handle
		err = windows.DuplicateHandle(hProc, windows.Handle(h.HandleValue), windows.CurrentProcess(), &dup, 0, false, windows.DUPLICATE_SAME_ACCESS)
		if err != nil {
			continue
		}
		name, typ := queryObjectNameAndType(dup)
		_ = windows.CloseHandle(dup)

		if typ != "File" || name == "" {
			continue
		}

		dos := ntPathToDOS(name)
		if dos == "" {
			continue
		}
		low := strings.ToLower(dos)
		if strings.Contains(low, `\\pipe\\`) || strings.Contains(low, `\\device\\`) {
			continue
		}
		if strings.Contains(low, `\\appdata\\local\\temp\\`) {
			continue
		}
		out[filepath.Clean(dos)] = struct{}{}
		processedCount++
	}

	var res []FileRef
	for p := range out {
		res = append(res, FileRef{Path: p})
	}
	return res
}

type systemHandleEntry struct {
	UniqueProcessID uint32
	ObjectTypeIndex uint8
	Flags           uint8
	HandleValue     uint16
	Object          uintptr
	GrantedAccess   uint32
}

type systemHandleInfoEx struct {
	NumberOfHandles uintptr
	Reserved        uintptr
	Handles         [1]systemHandleEntry
}

var (
	ntdll                        = windows.NewLazySystemDLL("ntdll.dll")
	procNtQuerySystemInformation = ntdll.NewProc("NtQuerySystemInformation")
	procNtQueryObject            = ntdll.NewProc("NtQueryObject")

	kernel32            = windows.NewLazySystemDLL("kernel32.dll")
	procQueryDosDeviceW = kernel32.NewProc("QueryDosDeviceW")
)

const (
	SystemExtendedHandleInformation = 0x40
	ObjectNameInformation           = 1
	ObjectTypeInformation           = 2
)

func querySystemHandles() ([]systemHandleEntry, error) {
	size := uint32(1 << 20)
	for i := 0; i < 6; i++ {
		buf := make([]byte, size)
		var retLen uint32
		r1, _, _ := procNtQuerySystemInformation.Call(
			uintptr(SystemExtendedHandleInformation),
			uintptr(unsafe.Pointer(&buf[0])),
			uintptr(size),
			uintptr(unsafe.Pointer(&retLen)),
		)
		// STATUS_INFO_LENGTH_MISMATCH = 0xC0000004
		if uint32(r1) == 0xC0000004 {
			if retLen > size {
				size = retLen + (1 << 16)
			} else {
				size *= 2
			}
			continue
		}
		if r1 != 0 {
			return nil, windows.Errno(r1)
		}

		info := (*systemHandleInfoEx)(unsafe.Pointer(&buf[0]))
		count := int(info.NumberOfHandles)
		base := uintptr(unsafe.Pointer(&info.Handles[0]))
		entries := make([]systemHandleEntry, 0, count)
		for j := 0; j < count; j++ {
			e := *(*systemHandleEntry)(unsafe.Pointer(base + uintptr(j)*unsafe.Sizeof(systemHandleEntry{})))
			entries = append(entries, e)
		}
		return entries, nil
	}
	return nil, windows.ERROR_INSUFFICIENT_BUFFER
}

type unicodeString struct {
	Length        uint16
	MaximumLength uint16
	Buffer        *uint16
}

type objectTypeInfo struct {
	TypeName unicodeString
}

type objectNameInfo struct {
	Name unicodeString
}

func queryObjectNameAndType(h windows.Handle) (name string, typ string) {
	{
		buf := make([]byte, 4096)
		var retLen uint32
		r1, _, _ := procNtQueryObject.Call(
			uintptr(h),
			uintptr(ObjectTypeInformation),
			uintptr(unsafe.Pointer(&buf[0])),
			uintptr(len(buf)),
			uintptr(unsafe.Pointer(&retLen)),
		)
		if r1 == 0 {
			oti := (*objectTypeInfo)(unsafe.Pointer(&buf[0]))
			if oti.TypeName.Buffer != nil && oti.TypeName.Length > 0 {
				u := unsafe.Slice(oti.TypeName.Buffer, oti.TypeName.Length/2)
				typ = windows.UTF16ToString(u)
			}
		}
	}

	size := 4096
	for i := 0; i < 4; i++ {
		buf := make([]byte, size)
		var retLen uint32
		r1, _, _ := procNtQueryObject.Call(
			uintptr(h),
			uintptr(ObjectNameInformation),
			uintptr(unsafe.Pointer(&buf[0])),
			uintptr(len(buf)),
			uintptr(unsafe.Pointer(&retLen)),
		)
		if uint32(r1) == 0xC0000004 {
			size = int(retLen) + 1024
			continue
		}
		if r1 != 0 {
			return "", typ
		}
		oni := (*objectNameInfo)(unsafe.Pointer(&buf[0]))
		if oni.Name.Buffer != nil && oni.Name.Length > 0 {
			u := unsafe.Slice(oni.Name.Buffer, oni.Name.Length/2)
			name = windows.UTF16ToString(u)
		}
		return name, typ
	}
	return "", typ
}

func ntPathToDOS(ntPath string) string {
	for _, drive := range []string{"A:", "B:", "C:", "D:", "E:", "F:", "G:", "H:", "I:", "J:", "K:"} {
		target := queryDosDevice(drive)
		if target == "" {
			continue
		}
		if strings.HasPrefix(strings.ToLower(ntPath), strings.ToLower(target)) {
			rest := ntPath[len(target):]
			if strings.HasPrefix(rest, `\`) {
				return drive + rest
			}
			return drive + `\` + rest
		}
	}
	return ""
}

func queryDosDevice(drive string) string {
	d, _ := windows.UTF16PtrFromString(drive)
	buf := make([]uint16, 1024)
	r1, _, _ := procQueryDosDeviceW.Call(uintptr(unsafe.Pointer(d)), uintptr(unsafe.Pointer(&buf[0])), uintptr(len(buf)))
	if r1 == 0 {
		return ""
	}
	return windows.UTF16ToString(buf[:r1])
}
