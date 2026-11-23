//go:build darwin

package capture

import (
	"errors"
	"sync"

	"github.com/ebitengine/purego"
)

var displayBudget struct {
	sync.Once
	err         error
	active      func(uint32, *uint32, *uint32) int32
	bounds      func(uint32) cgRect
	mode        func(uint32) uintptr
	width       func(uintptr) uintptr
	pixelWidth  func(uintptr) uintptr
	height      func(uintptr) uintptr
	pixelHeight func(uintptr) uintptr
	release     func(uintptr)
}

// Conservatively budget at the largest active display's backing scale before
// launching screencapture. The output header is checked again before reading.
// These display metadata calls do not query/request screen-recording access.
func checkDarwinCapturePixels(width, height int, scaleHint float64) error {
	if finite(scaleHint) && scaleHint >= 1 {
		return checkScaledCapturePixels(width, height, scaleHint)
	}
	displays, err := darwinDisplaySurfaces()
	if err != nil {
		return err
	}
	scale := 1.0
	for _, display := range displays {
		scale = max(scale, display.Scale)
	}
	return checkScaledCapturePixels(width, height, scale)
}

func darwinDisplaySurfaces() ([]DisplaySurface, error) {
	displayBudget.Do(func() {
		lib, err := purego.Dlopen(coreGraphicsFrameworkPath, purego.RTLD_LAZY|purego.RTLD_LOCAL)
		if err != nil {
			displayBudget.err = err
			return
		}
		for _, symbol := range []struct {
			name string
			fn   any
		}{
			{"CGGetActiveDisplayList", &displayBudget.active},
			{"CGDisplayBounds", &displayBudget.bounds},
			{"CGDisplayCopyDisplayMode", &displayBudget.mode},
			{"CGDisplayModeGetWidth", &displayBudget.width},
			{"CGDisplayModeGetPixelWidth", &displayBudget.pixelWidth},
			{"CGDisplayModeGetHeight", &displayBudget.height},
			{"CGDisplayModeGetPixelHeight", &displayBudget.pixelHeight},
			{"CGDisplayModeRelease", &displayBudget.release},
		} {
			p, err := purego.Dlsym(lib, symbol.name)
			if err != nil {
				displayBudget.err = err
				return
			}
			purego.RegisterFunc(symbol.fn, p)
		}
	})
	if displayBudget.err != nil {
		return nil, displayBudget.err
	}
	var displays [128]uint32
	var count uint32
	if displayBudget.active(uint32(len(displays)), &displays[0], &count) != 0 || count == 0 || count >= uint32(len(displays)) {
		return nil, errors.New("capture: cannot determine display pixel budget")
	}
	surfaces := make([]DisplaySurface, 0, count)
	for _, display := range displays[:count] {
		mode := displayBudget.mode(display)
		if mode == 0 {
			return nil, errors.New("capture: cannot determine display scale")
		}
		width, height := displayBudget.width(mode), displayBudget.height(mode)
		pixelWidth, pixelHeight := displayBudget.pixelWidth(mode), displayBudget.pixelHeight(mode)
		displayBudget.release(mode)
		if width == 0 || height == 0 || pixelWidth == 0 || pixelHeight == 0 {
			return nil, errors.New("capture: invalid display mode")
		}
		bounds := displayBudget.bounds(display)
		surfaces = append(surfaces, DisplaySurface{
			Bounds: Rect{X: bounds.Origin.X, Y: bounds.Origin.Y, Width: bounds.Size.Width, Height: bounds.Size.Height},
			Scale:  max(float64(pixelWidth)/float64(width), float64(pixelHeight)/float64(height)),
		})
	}
	return surfaces, nil
}
