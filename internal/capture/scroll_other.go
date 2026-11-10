//go:build !darwin

package capture

import "errors"

// ScrollSupported reports whether the app can send synthetic scroll events.
func ScrollSupported() bool { return false }

// ScrollWheel is not implemented on this platform yet.
func ScrollWheel(p Point, deltaPixels int32) error {
	return ErrUnsupported
}

// AccessibilityGranted is always true where input events need no permission.
func AccessibilityGranted() bool { return true }

// OpenAccessibilitySettings is not available where input events need no
// permission.
func OpenAccessibilitySettings() error {
	return errors.New("capture: accessibility settings are only needed on macOS")
}
