//go:build darwin

package capture

import (
	"errors"
	"math"
	"sync"

	"github.com/ebitengine/purego"
	"github.com/echoVic/Pinstill/internal/imagebudget"
)

var displayBudget struct {
	sync.Once
	err         error
	active      func(uint32, *uint32, *uint32) int32
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
func checkDarwinCapturePixels(width, height int) error {
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
		return displayBudget.err
	}
	var displays [128]uint32
	var count uint32
	if displayBudget.active(uint32(len(displays)), &displays[0], &count) != 0 || count == 0 || count >= uint32(len(displays)) {
		return errors.New("capture: cannot determine display pixel budget")
	}
	scale := 1.0
	for _, d := range displays[:count] {
		mode := displayBudget.mode(d)
		if mode == 0 {
			return errors.New("capture: cannot determine display scale")
		}
		w, h := displayBudget.width(mode), displayBudget.height(mode)
		pw, ph := displayBudget.pixelWidth(mode), displayBudget.pixelHeight(mode)
		displayBudget.release(mode)
		if w == 0 || h == 0 || pw == 0 || ph == 0 {
			return errors.New("capture: invalid display mode")
		}
		scale = max(scale, float64(pw)/float64(w), float64(ph)/float64(h))
	}
	w, h := math.Ceil(float64(width)*scale), math.Ceil(float64(height)*scale)
	if !finite(w) || !finite(h) || w > imagebudget.MaxPixels || h > imagebudget.MaxPixels {
		return errors.New("capture: display pixel limit exceeded")
	}
	return imagebudget.CheckDimensions(int(w), int(h))
}
