//go:build windows

package capture

import (
	"context"
	"errors"
	"fmt"
	"image"
	"runtime"
	"sync"
	"syscall"
	"unsafe"

	"github.com/echoVic/Pinstill/internal/imagebudget"
)

// Windows screen capture backend built on GDI BitBlt. No cgo and no COM/WinRT
// dependencies, so it cross-compiles from macOS and Linux with a stock toolchain
// and works on any Windows version that has user32/gdi32.
//
// Input coordinates use MyGo's screen DIPs. Monitor origins and offsets
// are converted separately by monitorPixelRect before GDI sees device pixels.
const (
	srcCopy = 0x00CC0020

	dibRGBColors = 0
	biRGB        = 0

	smXVirtualScreen  = 76
	smYVirtualScreen  = 77
	smCXVirtualScreen = 78
	smCYVirtualScreen = 79
)

var (
	user32 = syscall.NewLazyDLL("user32.dll")
	gdi32  = syscall.NewLazyDLL("gdi32.dll")
	shcore = syscall.NewLazyDLL("shcore.dll")

	procGetDC               = user32.NewProc("GetDC")
	procReleaseDC           = user32.NewProc("ReleaseDC")
	procGetSystemMetrics    = user32.NewProc("GetSystemMetrics")
	procEnumDisplayMonitors = user32.NewProc("EnumDisplayMonitors")
	procGetMonitorInfo      = user32.NewProc("GetMonitorInfoW")
	procGetDpiForMonitor    = shcore.NewProc("GetDpiForMonitor")
	procGetDpiForSystem     = user32.NewProc("GetDpiForSystem")
	procGetCursorInfo       = user32.NewProc("GetCursorInfo")
	procGetIconInfo         = user32.NewProc("GetIconInfo")
	procDrawIconEx          = user32.NewProc("DrawIconEx")
	procCopyIcon            = user32.NewProc("CopyIcon")
	procDestroyIcon         = user32.NewProc("DestroyIcon")

	procCreateCompatibleDC = gdi32.NewProc("CreateCompatibleDC")
	procCreateDIBSection   = gdi32.NewProc("CreateDIBSection")
	procSelectObject       = gdi32.NewProc("SelectObject")
	procBitBlt             = gdi32.NewProc("BitBlt")
	procGdiFlush           = gdi32.NewProc("GdiFlush")
	procDeleteObject       = gdi32.NewProc("DeleteObject")
	procDeleteDC           = gdi32.NewProc("DeleteDC")
)

// bitmapInfoHeader mirrors the Win32 BITMAPINFOHEADER structure.
type bitmapInfoHeader struct {
	biSize          uint32
	biWidth         int32
	biHeight        int32
	biPlanes        uint16
	biBitCount      uint16
	biCompression   uint32
	biSizeImage     uint32
	biXPelsPerMeter int32
	biYPelsPerMeter int32
	biClrUsed       uint32
	biClrImportant  uint32
}

// bitmapInfo mirrors the Win32 BITMAPINFO structure. A single color entry is
// enough because 32bpp BI_RGB images do not use the palette.
type bitmapInfo struct {
	bmiHeader bitmapInfoHeader
	bmiColors [1]uint32
}

// Supported reports whether this platform has a real screen capture backend.
func Supported() bool { return true }

// PermissionGranted reports that Windows has no screen-recording permission
// gate for classic desktop applications.
func PermissionGranted() bool { return true }

// OpenPermissionSettings is unsupported on Windows: there is no screen
// recording privacy pane to open.
func OpenPermissionSettings() error { return ErrUnsupported }

// captureRegion grabs rect from the desktop and returns PNG bytes.
func captureRegion(ctx context.Context, rect Rect, opts CaptureOptions) ([]byte, error) {
	if rect.Empty() {
		return nil, errors.New("capture: empty or invalid capture region")
	}
	// GetDC and ReleaseDC must run on the same OS thread. Keep the entire
	// GDI object lifetime, cursor composition, and encoder's DIB access here.
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	monitors, err := captureMonitors()
	if err != nil {
		return nil, err
	}
	pixelRect, err := monitorPixelRect(rect, monitors)
	if err != nil {
		return nil, err
	}

	virtualX, virtualY, virtualWidth, virtualHeight := virtualScreenBounds()
	pixelRect, err = clipPixelRect(pixelRect, [4]int{virtualX, virtualY, virtualWidth, virtualHeight})
	if err != nil {
		return nil, err
	}
	x, y, width, height := pixelRect[0], pixelRect[1], pixelRect[2], pixelRect[3]
	// Dimensions/edges are int32-safe and <= MaxPixels before multiplication.
	byteSize := width * height * 4

	screenDC, _, _ := procGetDC.Call(0)
	if screenDC == 0 {
		return nil, errors.New("capture: GetDC failed")
	}
	defer procReleaseDC.Call(0, screenDC)

	memDC, _, _ := procCreateCompatibleDC.Call(screenDC)
	if memDC == 0 {
		return nil, errors.New("capture: CreateCompatibleDC failed")
	}
	defer procDeleteDC.Call(memDC)

	info := bitmapInfo{}
	info.bmiHeader.biSize = uint32(unsafe.Sizeof(bitmapInfoHeader{}))
	info.bmiHeader.biWidth = int32(width)
	// A negative height requests top-down rows, matching image.RGBA's layout.
	info.bmiHeader.biHeight = -int32(height)
	info.bmiHeader.biPlanes = 1
	info.bmiHeader.biBitCount = 32
	info.bmiHeader.biCompression = biRGB
	info.bmiHeader.biSizeImage = uint32(byteSize)

	var bits unsafe.Pointer
	bitmap, _, _ := procCreateDIBSection.Call(
		screenDC,
		uintptr(unsafe.Pointer(&info)),
		dibRGBColors,
		uintptr(unsafe.Pointer(&bits)),
		0,
		0,
	)
	if bitmap == 0 || bits == nil {
		return nil, errors.New("capture: CreateDIBSection failed")
	}

	previous, _, _ := procSelectObject.Call(memDC, bitmap)
	if previous == 0 || previous == ^uintptr(0) {
		procDeleteObject.Call(bitmap)
		return nil, errors.New("capture: SelectObject failed")
	}
	defer func() {
		procSelectObject.Call(memDC, previous)
		procDeleteObject.Call(bitmap)
	}()

	copied, _, _ := procBitBlt.Call(
		memDC,
		0, 0,
		uintptr(width), uintptr(height),
		screenDC,
		argInt32(int32(x)), argInt32(int32(y)),
		srcCopy,
	)
	if copied == 0 {
		return nil, errors.New("capture: BitBlt failed")
	}
	if opts.IncludeCursor {
		if err := drawCaptureCursor(memDC, x, y); err != nil {
			return nil, err
		}
	}
	procGdiFlush.Call()
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	pixels := unsafe.Slice((*byte)(bits), byteSize)
	// GDI stores 32bpp BI_RGB pixels as BGRX with a zero alpha channel; convert
	// them in place to opaque RGBA so the PNG encoder keeps the colors right.
	for i := 0; i < len(pixels); i += 4 {
		b, g, r := pixels[i], pixels[i+1], pixels[i+2]
		pixels[i], pixels[i+1], pixels[i+2], pixels[i+3] = r, g, b, 0xff
	}
	img := &image.RGBA{Pix: pixels, Stride: width * 4, Rect: image.Rect(0, 0, width, height)}
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	data, err := imagebudget.EncodePNG(img)
	if err != nil {
		return nil, fmt.Errorf("capture: encode PNG: %w", err)
	}
	return data, ctx.Err()
}

type winRect struct{ Left, Top, Right, Bottom int32 }
type winMonitorInfo struct {
	Size          uint32
	Monitor, Work winRect
	Flags         uint32
}

var monitorEnumeration struct {
	sync.Mutex
	once     sync.Once
	callback uintptr
	monitors []monitorGeometry
	err      error
}

// One persistent callback avoids leaking a syscall callback on every frame.
// The synchronous enumeration is serialized; unlike MyGo's main-thread-only
// sink it is safe for overlapping capture goroutines.
func captureMonitors() ([]monitorGeometry, error) {
	monitorEnumeration.Lock()
	defer monitorEnumeration.Unlock()
	monitorEnumeration.once.Do(func() {
		monitorEnumeration.callback = syscall.NewCallback(func(mon, _, _, _ uintptr) uintptr {
			info := winMonitorInfo{Size: uint32(unsafe.Sizeof(winMonitorInfo{}))}
			ok, _, _ := procGetMonitorInfo.Call(mon, uintptr(unsafe.Pointer(&info)))
			if ok == 0 {
				monitorEnumeration.err = errors.New("capture: GetMonitorInfo failed")
				return 0
			}
			dpi := uint32(96)
			if procGetDpiForSystem.Find() == nil {
				if d, _, _ := procGetDpiForSystem.Call(); d != 0 {
					dpi = uint32(d)
				}
			}
			if procGetDpiForMonitor.Find() == nil {
				var x, y uint32
				if hr, _, _ := procGetDpiForMonitor.Call(mon, 0, uintptr(unsafe.Pointer(&x)), uintptr(unsafe.Pointer(&y))); hr == 0 && x != 0 {
					dpi = x
				}
			}
			r := info.Monitor
			monitorEnumeration.monitors = append(monitorEnumeration.monitors, monitorGeometry{image.Rect(int(r.Left), int(r.Top), int(r.Right), int(r.Bottom)), int(dpi)})
			return 1
		})
	})
	monitorEnumeration.monitors, monitorEnumeration.err = nil, nil
	ok, _, _ := procEnumDisplayMonitors.Call(0, 0, monitorEnumeration.callback, 0)
	if ok == 0 && monitorEnumeration.err == nil {
		monitorEnumeration.err = errors.New("capture: EnumDisplayMonitors failed")
	}
	return monitorEnumeration.monitors, monitorEnumeration.err
}

type winCursorInfo struct {
	Size, Flags uint32
	Cursor      uintptr
	X, Y        int32
}
type winIconInfo struct {
	Icon        int32
	HotX, HotY  uint32
	Mask, Color uintptr
}

func drawCaptureCursor(dc uintptr, x, y int) error {
	info := winCursorInfo{Size: uint32(unsafe.Sizeof(winCursorInfo{}))}
	ok, _, _ := procGetCursorInfo.Call(uintptr(unsafe.Pointer(&info)))
	if ok == 0 {
		return errors.New("capture: GetCursorInfo failed")
	}
	if info.Flags&1 == 0 || info.Cursor == 0 {
		return nil
	}
	// The shared cursor can change while capturing; own a copy until drawn.
	icon, _, _ := procCopyIcon.Call(info.Cursor)
	if icon == 0 {
		return errors.New("capture: CopyIcon failed")
	}
	defer procDestroyIcon.Call(icon)
	var shape winIconInfo
	ok, _, _ = procGetIconInfo.Call(icon, uintptr(unsafe.Pointer(&shape)))
	if ok == 0 {
		return errors.New("capture: GetIconInfo failed")
	}
	if shape.Mask != 0 {
		defer procDeleteObject.Call(shape.Mask)
	}
	if shape.Color != 0 {
		defer procDeleteObject.Call(shape.Color)
	}
	dx, dy, valid := cursorDrawPosition(info.X, info.Y, shape.HotX, shape.HotY, x, y)
	if !valid {
		return nil
	} // well outside this DIB
	// DI_NORMAL composes both mask and image (including alpha cursors).
	ok, _, _ = procDrawIconEx.Call(dc, argInt32(dx), argInt32(dy), icon, 0, 0, 0, 0, 3)
	if ok == 0 {
		return errors.New("capture: DrawIconEx failed")
	}
	return nil
}

// virtualScreenBounds returns the bounding box of all displays in device
// pixels; it can start at negative coordinates.
func virtualScreenBounds() (x, y, width, height int) {
	metric := func(index uintptr) int {
		value, _, _ := procGetSystemMetrics.Call(index)
		return int(int32(value))
	}
	return metric(smXVirtualScreen), metric(smYVirtualScreen), metric(smCXVirtualScreen), metric(smCYVirtualScreen)
}

// argInt32 widens a signed 32-bit Windows argument without inventing a
// negative uintptr value.
func argInt32(value int32) uintptr {
	return uintptr(uint32(value))
}
