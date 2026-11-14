package capture

import "math"

// PixelRect converts a logical (point/DIP) rectangle into integer device
// pixel bounds for a display with the given scale factor. Origin coordinates
// may be negative on multi-display setups. An empty rectangle, or a
// non-positive scale, yields a zero rectangle. Non-finite geometry and values
// outside the integer range are rejected in the same way.
func PixelRect(rect Rect, scale float64) (x, y, width, height int) {
	if rect.Empty() || !finite(scale) || scale <= 0 {
		return 0, 0, 0, 0
	}
	rounded := [4]float64{
		math.Round(rect.X * scale), math.Round(rect.Y * scale),
		math.Round(rect.Width * scale), math.Round(rect.Height * scale),
	}
	// Use the exclusive upper bound: float64(maxInt) rounds up on 64-bit
	// platforms, so testing against it with <= would accept an overflow.
	const maxInt = int(^uint(0) >> 1)
	limit := float64(maxInt/2+1) * 2
	for _, v := range rounded {
		if !finite(v) || v < -limit || v >= limit {
			return 0, 0, 0, 0
		}
	}
	x, y, width, height = int(rounded[0]), int(rounded[1]), int(rounded[2]), int(rounded[3])
	if width < 1 {
		width = 1
	}
	if height < 1 {
		height = 1
	}
	return x, y, width, height
}
