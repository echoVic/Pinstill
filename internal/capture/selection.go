// Package capture contains platform-neutral pieces of Pinstill's capture flow.
package capture

// Point is a point in the coordinate space of a display or capture surface.
type Point struct {
	X float64
	Y float64
}

// Rect is a normalized capture selection. Width and Height are never negative.
type Rect struct {
	X      float64
	Y      float64
	Width  float64
	Height float64
}

// Empty reports whether the selection has no area worth capturing.
func (r Rect) Empty() bool {
	return r.Width <= 0 || r.Height <= 0
}

// NormalizeSelection converts two drag endpoints into a top-left rectangle.
// Sub-pixel drags smaller than one logical pixel are treated as clicks so the
// capture flow does not attempt to create a zero-content image.
func NormalizeSelection(start, end Point) Rect {
	x, width := start.X, end.X-start.X
	if width < 0 {
		x, width = end.X, -width
	}
	y, height := start.Y, end.Y-start.Y
	if height < 0 {
		y, height = end.Y, -height
	}
	if width < 1 {
		width = 0
	}
	if height < 1 {
		height = 0
	}
	return Rect{X: x, Y: y, Width: width, Height: height}
}
