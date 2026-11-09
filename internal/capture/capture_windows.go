//go:build windows

package capture

import (
	"bytes"
	"errors"
	"fmt"
	"image"
	"image/png"
	"math"
	"syscall"
	"unsafe"
)

// Windows screen capture backend built on GDI BitBlt. No cgo and no COM/WinRT
// dependencies, so it cross-compiles from macOS and Linux with a stock toolchain
// and works on any Windows version that has user32/gdi32.
//
// Coordinates are treated as device pixels of the virtual desktop (the origin
// is the top-left of the primary display, negative values address displays to
// the left of/above it). MyGo keeps the process DPI aware, so logical and
// device pixels only differ on scaled displays; callers should pass physical
// pixel bounds.
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

	procGetDC            = user32.NewProc("GetDC")
	procReleaseDC        = user32.NewProc("ReleaseDC")
	procGetSystemMetrics = user32.NewProc("GetSystemMetrics")

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

// CaptureRegion grabs rect from the desktop and returns PNG bytes.
func CaptureRegion(rect Rect, opts CaptureOptions) ([]byte, error) {
	if rect.Empty() {
		return nil, errors.New("capture: empty capture region")
	}
	sleepBeforeCapture(opts)

	x := int(math.Round(rect.X))
	y := int(math.Round(rect.Y))
	width := int(math.Round(rect.Width))
	height := int(math.Round(rect.Height))

	virtualX, virtualY, virtualWidth, virtualHeight := virtualScreenBounds()
	// Clip the requested rectangle to the virtual desktop so BitBlt does not
	// produce undefined content for areas that do not exist.
	if x < virtualX {
		width -= virtualX - x
		x = virtualX
	}
	if y < virtualY {
		height -= virtualY - y
		y = virtualY
	}
	if limit := virtualX + virtualWidth; x+width > limit {
		width = limit - x
	}
	if limit := virtualY + virtualHeight; y+height > limit {
		height = limit - y
	}
	if width <= 0 || height <= 0 {
		return nil, errors.New("capture: capture region is outside the screen")
	}

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
	info.bmiHeader.biSizeImage = uint32(width * height * 4)

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
	if previous == 0 {
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
	procGdiFlush.Call()

	pixels := unsafe.Slice((*byte)(bits), width*height*4)
	// GDI stores 32bpp BI_RGB pixels as BGRX with a zero alpha channel; convert
	// them in place to opaque RGBA so the PNG encoder keeps the colors right.
	for i := 0; i < len(pixels); i += 4 {
		b, g, r := pixels[i], pixels[i+1], pixels[i+2]
		pixels[i], pixels[i+1], pixels[i+2], pixels[i+3] = r, g, b, 0xff
	}
	img := &image.RGBA{Pix: pixels, Stride: width * 4, Rect: image.Rect(0, 0, width, height)}

	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		return nil, fmt.Errorf("capture: encode PNG: %w", err)
	}
	return buf.Bytes(), nil
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
