//go:build !darwin && !windows

package capture

import "context"

// Supported reports whether this platform has a real screen capture backend.
func Supported() bool { return false }

func captureRegion(_ context.Context, _ Rect, _ CaptureOptions) ([]byte, error) {
	return nil, ErrUnsupported
}

// PermissionGranted reports that no screen recording permission gate exists on
// this platform.
func PermissionGranted() bool { return true }

// OpenPermissionSettings is unsupported on this platform.
func OpenPermissionSettings() error {
	return ErrUnsupported
}
