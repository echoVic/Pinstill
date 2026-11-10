//go:build darwin

package capture

import (
	"errors"
	"fmt"
	"os/exec"
	"sync"

	"github.com/ebitengine/purego"
	"structs"
)

const (
	scrollGraphicsPath      = "/System/Library/Frameworks/CoreGraphics.framework/CoreGraphics"
	applicationServicesPath = "/System/Library/Frameworks/ApplicationServices.framework/ApplicationServices"
	// kCGScrollEventUnitPixel scrolls by pixels, so the step does not depend
	// on the user's line-scroll settings.
	kCGScrollEventUnitPixel = 0
	// kCGHIDEventTap posts at the HID event tap, as a real input device does.
	kCGHIDEventTap = 0
)

// cgPoint mirrors CoreGraphics' CGPoint for C calls made through purego.
type cgPoint struct {
	_ structs.HostLayout
	X float64
	Y float64
}

var (
	scrollOnce sync.Once
	scrollInit error

	cgEventCreateScrollWheelEvent2 func(source uintptr, units uint32, wheelCount uint32, wheel1 int32, wheel2 int32, wheel3 int32) uintptr
	cgEventSetLocation             func(event uintptr, point cgPoint)
	cgEventPost                    func(tap uint32, event uintptr)
	cfRelease                      func(obj uintptr)

	accessibilityOnce sync.Once
	accessibilityFn   func() bool
)

// ScrollSupported reports whether the app can send synthetic scroll events.
func ScrollSupported() bool { return true }

func initScroll() error {
	scrollOnce.Do(func() {
		lib, err := purego.Dlopen(scrollGraphicsPath, purego.RTLD_LAZY|purego.RTLD_LOCAL)
		if err != nil {
			scrollInit = fmt.Errorf("capture: load CoreGraphics: %w", err)
			return
		}
		symbols := []struct {
			name string
			fn   any
		}{
			{"CGEventCreateScrollWheelEvent2", &cgEventCreateScrollWheelEvent2},
			{"CGEventSetLocation", &cgEventSetLocation},
			{"CGEventPost", &cgEventPost},
			{"CFRelease", &cfRelease},
		}
		for _, symbol := range symbols {
			ptr, err := purego.Dlsym(lib, symbol.name)
			if err != nil || ptr == 0 {
				scrollInit = fmt.Errorf("capture: CoreGraphics is missing %s", symbol.name)
				return
			}
			purego.RegisterFunc(symbol.fn, ptr)
		}
	})
	return scrollInit
}

// ScrollWheel sends one scroll-wheel event at screen point p (DIPs, origin at
// the primary display's top-left). Negative delta scrolls the content down
// (that is, towards the end of the page).
func ScrollWheel(p Point, deltaPixels int32) error {
	if err := initScroll(); err != nil {
		return err
	}
	if deltaPixels == 0 {
		return errors.New("capture: empty scroll delta")
	}
	event := cgEventCreateScrollWheelEvent2(0, kCGScrollEventUnitPixel, 1, deltaPixels, 0, 0)
	if event == 0 {
		return errors.New("capture: could not create a scroll event")
	}
	defer cfRelease(event)
	cgEventSetLocation(event, cgPoint{X: p.X, Y: p.Y})
	cgEventPost(kCGHIDEventTap, event)
	return nil
}

// AccessibilityGranted reports whether the process may post input events
// (System Settings > Privacy & Security > Accessibility).
func AccessibilityGranted() bool {
	accessibilityOnce.Do(func() {
		lib, err := purego.Dlopen(applicationServicesPath, purego.RTLD_LAZY|purego.RTLD_LOCAL)
		if err != nil {
			return
		}
		ptr, err := purego.Dlsym(lib, "AXIsProcessTrusted")
		if err != nil || ptr == 0 {
			return
		}
		var fn func() bool
		purego.RegisterFunc(&fn, ptr)
		accessibilityFn = fn
	})
	if accessibilityFn == nil {
		return false
	}
	return accessibilityFn()
}

// OpenAccessibilitySettings opens the Accessibility privacy pane.
func OpenAccessibilitySettings() error {
	url := "x-apple.systempreferences:com.apple.preference.security?Privacy_Accessibility"
	if err := exec.Command(openPath, url).Run(); err != nil {
		return fmt.Errorf("capture: open accessibility settings: %w", err)
	}
	return nil
}
