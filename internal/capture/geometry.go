package capture

import "math"

// PixelRect converts a logical (point/DIP) rectangle into integer device
// pixel bounds for a display with the given scale factor. Origin coordinates
// may be negative on multi-display setups. An empty rectangle, or a
// non-positive scale, yields a zero rectangle.
func PixelRect(rect Rect, scale float64) (x, y, width, height int) {
	if rect.Empty() || scale <= 0 {
		return 0, 0, 0, 0
	}
	x = int(math.Round(rect.X * scale))
	y = int(math.Round(rect.Y * scale))
	width = int(math.Round(rect.Width * scale))
	height = int(math.Round(rect.Height * scale))
	if width < 1 {
		width = 1
	}
	if height < 1 {
		height = 1
	}
	return x, y, width, height
}
