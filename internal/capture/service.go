package capture

// CaptureService exposes platform-neutral capture operations to the frontend.
// Platform-specific screen acquisition will be added behind this boundary.
type CaptureService struct{}

// NormalizeSelection turns two drag endpoints into a capture rectangle.
func (CaptureService) NormalizeSelection(start, end Point) Rect {
	return NormalizeSelection(start, end)
}
