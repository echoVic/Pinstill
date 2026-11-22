//go:build !darwin && !windows

package capture

// VisibleWindows reports no desktop windows on platforms without a native
// capture backend.
func VisibleWindows(_ int) []WindowInfo { return nil }
