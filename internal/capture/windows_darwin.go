//go:build darwin

package capture

import (
	"bytes"
	"fmt"
	"structs"
	"sync"
	"unsafe"

	"github.com/ebitengine/purego"
)

const (
	coreFoundationFrameworkPath = "/System/Library/Frameworks/CoreFoundation.framework/CoreFoundation"

	kCGWindowListOptionOnScreenOnly     = 1 << 0
	kCGWindowListExcludeDesktopElements = 1 << 4
	kCGNullWindowID                     = 0

	kCFNumberSInt32Type  = 3
	kCFNumberSInt64Type  = 4
	kCFNumberFloat64Type = 13
	kCFStringEncodingUTF8 = 0x08000100
)

type cgSize struct {
	_      structs.HostLayout
	Width  float64
	Height float64
}

type cgRect struct {
	_      structs.HostLayout
	Origin cgPoint
	Size   cgSize
}

var darwinWindows struct {
	sync.Once
	err error

	copyWindowInfo func(option uint32, relativeToWindow uint32) uintptr
	rectFromDict   func(dict uintptr, rect *cgRect) bool

	arrayCount       func(arr uintptr) int
	arrayValueAt     func(arr uintptr, idx int) uintptr
	dictValue        func(dict uintptr, key uintptr) uintptr
	numberValue      func(num uintptr, numberType int, valuePtr unsafe.Pointer) bool
	boolValue        func(b uintptr) bool
	stringGetCString func(str uintptr, buf *byte, bufSize int, encoding uint32) bool
	getTypeID        func(cf uintptr) uintptr
	boolTypeID       func() uintptr
	numberTypeID     func() uintptr
	stringTypeID     func() uintptr
	dictTypeID       func() uintptr
	release          func(cf uintptr)

	kBounds     uintptr
	kLayer      uintptr
	kAlpha      uintptr
	kOwnerPID   uintptr
	kNumber     uintptr
	kIsOnscreen uintptr
	kOwnerName  uintptr
	kName       uintptr

	boolType   uintptr
	numberType uintptr
	stringType uintptr
	dictType   uintptr
}

func initDarwinWindows() error {
	darwinWindows.Do(func() {
		cg, err := purego.Dlopen(coreGraphicsFrameworkPath, purego.RTLD_LAZY|purego.RTLD_LOCAL)
		if err != nil {
			darwinWindows.err = fmt.Errorf("capture: load CoreGraphics: %w", err)
			return
		}
		cf, err := purego.Dlopen(coreFoundationFrameworkPath, purego.RTLD_LAZY|purego.RTLD_LOCAL)
		if err != nil {
			darwinWindows.err = fmt.Errorf("capture: load CoreFoundation: %w", err)
			return
		}
		for _, sym := range []struct {
			lib  uintptr
			name string
			fn   any
		}{
			{cg, "CGWindowListCopyWindowInfo", &darwinWindows.copyWindowInfo},
			{cg, "CGRectMakeWithDictionaryRepresentation", &darwinWindows.rectFromDict},
			{cf, "CFArrayGetCount", &darwinWindows.arrayCount},
			{cf, "CFArrayGetValueAtIndex", &darwinWindows.arrayValueAt},
			{cf, "CFDictionaryGetValue", &darwinWindows.dictValue},
			{cf, "CFNumberGetValue", &darwinWindows.numberValue},
			{cf, "CFBooleanGetValue", &darwinWindows.boolValue},
			{cf, "CFStringGetCString", &darwinWindows.stringGetCString},
			{cf, "CFGetTypeID", &darwinWindows.getTypeID},
			{cf, "CFBooleanGetTypeID", &darwinWindows.boolTypeID},
			{cf, "CFNumberGetTypeID", &darwinWindows.numberTypeID},
			{cf, "CFStringGetTypeID", &darwinWindows.stringTypeID},
			{cf, "CFDictionaryGetTypeID", &darwinWindows.dictTypeID},
			{cf, "CFRelease", &darwinWindows.release},
		} {
			ptr, err := purego.Dlsym(sym.lib, sym.name)
			if err != nil || ptr == 0 {
				darwinWindows.err = fmt.Errorf("capture: missing symbol %s", sym.name)
				return
			}
			purego.RegisterFunc(sym.fn, ptr)
		}
		for _, key := range []struct {
			name string
			dst  *uintptr
		}{
			{"kCGWindowBounds", &darwinWindows.kBounds},
			{"kCGWindowLayer", &darwinWindows.kLayer},
			{"kCGWindowAlpha", &darwinWindows.kAlpha},
			{"kCGWindowOwnerPID", &darwinWindows.kOwnerPID},
			{"kCGWindowNumber", &darwinWindows.kNumber},
			{"kCGWindowIsOnscreen", &darwinWindows.kIsOnscreen},
			{"kCGWindowOwnerName", &darwinWindows.kOwnerName},
			{"kCGWindowName", &darwinWindows.kName},
		} {
			sym, err := purego.Dlsym(cg, key.name)
			if err != nil || sym == 0 {
				darwinWindows.err = fmt.Errorf("capture: missing constant %s", key.name)
				return
			}
			// kCGWindow* symbols are addresses of const CFStringRef globals.
			*key.dst = **(**uintptr)(unsafe.Pointer(&sym))
			if *key.dst == 0 {
				darwinWindows.err = fmt.Errorf("capture: nil constant %s", key.name)
				return
			}
		}
		darwinWindows.boolType = darwinWindows.boolTypeID()
		darwinWindows.numberType = darwinWindows.numberTypeID()
		darwinWindows.stringType = darwinWindows.stringTypeID()
		darwinWindows.dictType = darwinWindows.dictTypeID()
	})
	return darwinWindows.err
}

// VisibleWindows enumerates visible standard desktop windows front-to-back in
// screen points (origin at the primary display's top-left).
func VisibleWindows(excludePID int) []WindowInfo {
	if err := initDarwinWindows(); err != nil {
		return nil
	}
	arr := darwinWindows.copyWindowInfo(kCGWindowListOptionOnScreenOnly|kCGWindowListExcludeDesktopElements, kCGNullWindowID)
	if arr == 0 {
		return nil
	}
	defer darwinWindows.release(arr)

	count := darwinWindows.arrayCount(arr)
	if count <= 0 {
		return nil
	}
	windows := make([]WindowInfo, 0, count)
	for i := 0; i < count; i++ {
		entry := darwinWindows.arrayValueAt(arr, i)
		if entry == 0 || darwinWindows.getTypeID(entry) != darwinWindows.dictType {
			continue
		}
		if onscreen, ok := cfDictBool(entry, darwinWindows.kIsOnscreen); ok && !onscreen {
			continue
		}
		layer, ok := cfDictInt32(entry, darwinWindows.kLayer)
		if !ok || layer != 0 {
			continue
		}
		if alpha, ok := cfDictFloat64(entry, darwinWindows.kAlpha); ok && alpha <= 0.01 {
			continue
		}
		pid, hasPID := cfDictInt32(entry, darwinWindows.kOwnerPID)
		if hasPID && excludePID > 0 && int(pid) == excludePID {
			continue
		}
		owner := cfDictString(entry, darwinWindows.kOwnerName)
		if owner == "Window Server" {
			continue
		}
		boundsDict := darwinWindows.dictValue(entry, darwinWindows.kBounds)
		if boundsDict == 0 || darwinWindows.getTypeID(boundsDict) != darwinWindows.dictType {
			continue
		}
		var cg cgRect
		if !darwinWindows.rectFromDict(boundsDict, &cg) {
			continue
		}
		rect := Rect{X: cg.Origin.X, Y: cg.Origin.Y, Width: cg.Size.Width, Height: cg.Size.Height}
		if rect.Empty() || rect.Width < 12 || rect.Height < 12 {
			continue
		}
		id, _ := cfDictInt64(entry, darwinWindows.kNumber)
		title := cfDictString(entry, darwinWindows.kName)
		windows = append(windows, WindowInfo{
			ID:    uint64(max(0, id)),
			PID:   int(pid),
			Owner: owner,
			Title: title,
			Rect:  rect,
		})
	}
	return windows
}

func cfDictBool(dict, key uintptr) (bool, bool) {
	val := darwinWindows.dictValue(dict, key)
	if val == 0 || darwinWindows.getTypeID(val) != darwinWindows.boolType {
		return false, false
	}
	return darwinWindows.boolValue(val), true
}

func cfDictInt32(dict, key uintptr) (int32, bool) {
	val := darwinWindows.dictValue(dict, key)
	if val == 0 || darwinWindows.getTypeID(val) != darwinWindows.numberType {
		return 0, false
	}
	var out int32
	ok := darwinWindows.numberValue(val, kCFNumberSInt32Type, unsafe.Pointer(&out))
	return out, ok
}

func cfDictInt64(dict, key uintptr) (int64, bool) {
	val := darwinWindows.dictValue(dict, key)
	if val == 0 || darwinWindows.getTypeID(val) != darwinWindows.numberType {
		return 0, false
	}
	var out int64
	ok := darwinWindows.numberValue(val, kCFNumberSInt64Type, unsafe.Pointer(&out))
	return out, ok
}

func cfDictFloat64(dict, key uintptr) (float64, bool) {
	val := darwinWindows.dictValue(dict, key)
	if val == 0 || darwinWindows.getTypeID(val) != darwinWindows.numberType {
		return 0, false
	}
	var out float64
	ok := darwinWindows.numberValue(val, kCFNumberFloat64Type, unsafe.Pointer(&out))
	return out, ok
}

func cfDictString(dict, key uintptr) string {
	val := darwinWindows.dictValue(dict, key)
	if val == 0 || darwinWindows.getTypeID(val) != darwinWindows.stringType {
		return ""
	}
	var buf [1024]byte
	if !darwinWindows.stringGetCString(val, &buf[0], len(buf), kCFStringEncodingUTF8) {
		return ""
	}
	if n := bytes.IndexByte(buf[:], 0); n >= 0 {
		return string(buf[:n])
	}
	return string(buf[:])
}
