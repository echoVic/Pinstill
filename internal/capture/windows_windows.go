//go:build windows

package capture

import (
	"sync"
	"syscall"
	"unsafe"
)

const (
	dwmwaExtendedFrameBounds = 9
	dwmwaCloaked             = 14

	gwlExStyle = -20

	wsExToolWindow = 0x00000080
	wsExAppWindow  = 0x00040000
)

var (
	dwmapi = syscall.NewLazyDLL("dwmapi.dll")

	procEnumWindows              = user32.NewProc("EnumWindows")
	procIsWindowVisible          = user32.NewProc("IsWindowVisible")
	procIsIconic                 = user32.NewProc("IsIconic")
	procGetWindowRect            = user32.NewProc("GetWindowRect")
	procGetWindowThreadProcessId = user32.NewProc("GetWindowThreadProcessId")
	procGetWindowLongW           = user32.NewProc("GetWindowLongW")
	procGetWindowTextW           = user32.NewProc("GetWindowTextW")
	procGetClassNameW            = user32.NewProc("GetClassNameW")
	procGetShellWindow           = user32.NewProc("GetShellWindow")
	procDwmGetWindowAttribute    = dwmapi.NewProc("DwmGetWindowAttribute")
)

var windowEnumeration struct {
	sync.Mutex
	once       sync.Once
	callback   uintptr
	excludePID int
	shellWnd   uintptr
	monitors   []monitorGeometry
	windows    []WindowInfo
}

// VisibleWindows enumerates visible top-level desktop windows front-to-back in
// MyGo screen DIPs.
func VisibleWindows(excludePID int) []WindowInfo {
	monitors, err := captureMonitors()
	if err != nil || len(monitors) == 0 {
		return nil
	}
	windowEnumeration.Lock()
	defer windowEnumeration.Unlock()
	windowEnumeration.once.Do(func() {
		windowEnumeration.callback = syscall.NewCallback(enumWindowCallback)
	})
	shellWnd, _, _ := procGetShellWindow.Call()
	windowEnumeration.excludePID = excludePID
	windowEnumeration.shellWnd = shellWnd
	windowEnumeration.monitors = monitors
	windowEnumeration.windows = nil
	_, _, _ = procEnumWindows.Call(windowEnumeration.callback, 0)
	out := windowEnumeration.windows
	windowEnumeration.monitors = nil
	windowEnumeration.windows = nil
	return out
}

func enumWindowCallback(hwnd, _ uintptr) uintptr {
	if hwnd == 0 || hwnd == windowEnumeration.shellWnd {
		return 1
	}
	if vis, _, _ := procIsWindowVisible.Call(hwnd); vis == 0 {
		return 1
	}
	if iconic, _, _ := procIsIconic.Call(hwnd); iconic != 0 {
		return 1
	}
	var pid uint32
	_, _, _ = procGetWindowThreadProcessId.Call(hwnd, uintptr(unsafe.Pointer(&pid)))
	if windowEnumeration.excludePID > 0 && int(pid) == windowEnumeration.excludePID {
		return 1
	}
	if procDwmGetWindowAttribute.Find() == nil {
		var cloaked uint32
		hr, _, _ := procDwmGetWindowAttribute.Call(hwnd, dwmwaCloaked, uintptr(unsafe.Pointer(&cloaked)), unsafe.Sizeof(cloaked))
		if hr == 0 && cloaked != 0 {
			return 1
		}
	}
	exStyle, _, _ := procGetWindowLongW.Call(hwnd, argInt32(gwlExStyle))
	if uint32(exStyle)&wsExToolWindow != 0 && uint32(exStyle)&wsExAppWindow == 0 {
		return 1
	}
	className := windowClassName(hwnd)
	if className == "Progman" || className == "WorkerW" {
		return 1
	}
	var wr winRect
	gotBounds := false
	if procDwmGetWindowAttribute.Find() == nil {
		hr, _, _ := procDwmGetWindowAttribute.Call(hwnd, dwmwaExtendedFrameBounds, uintptr(unsafe.Pointer(&wr)), unsafe.Sizeof(wr))
		gotBounds = hr == 0
	}
	if !gotBounds {
		ok, _, _ := procGetWindowRect.Call(hwnd, uintptr(unsafe.Pointer(&wr)))
		gotBounds = ok != 0
	}
	if !gotBounds {
		return 1
	}
	px := [4]int{int(wr.Left), int(wr.Top), int(wr.Right - wr.Left), int(wr.Bottom - wr.Top)}
	rect, ok := pixelsToMonitorDIPRect(px, windowEnumeration.monitors)
	if !ok || rect.Width < 12 || rect.Height < 12 {
		return 1
	}
	windowEnumeration.windows = append(windowEnumeration.windows, WindowInfo{
		ID:    uint64(hwnd),
		PID:   int(pid),
		Title: windowText(hwnd),
		Rect:  rect,
	})
	return 1
}

func windowClassName(hwnd uintptr) string {
	var buf [128]uint16
	n, _, _ := procGetClassNameW.Call(hwnd, uintptr(unsafe.Pointer(&buf[0])), uintptr(len(buf)))
	if n == 0 {
		return ""
	}
	return syscall.UTF16ToString(buf[:min(int(n), len(buf))])
}

func windowText(hwnd uintptr) string {
	var buf [256]uint16
	n, _, _ := procGetWindowTextW.Call(hwnd, uintptr(unsafe.Pointer(&buf[0])), uintptr(len(buf)))
	if n == 0 {
		return ""
	}
	return syscall.UTF16ToString(buf[:min(int(n), len(buf))])
}
