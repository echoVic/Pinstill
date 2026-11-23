// Package capture contains platform-neutral pieces of Pinstill's capture flow.
package capture

import "math"

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

// Empty reports whether the selection is invalid or has no area worth capturing.
func (r Rect) Empty() bool {
	return !finite(r.X) || !finite(r.Y) || !finite(r.Width) || !finite(r.Height) || r.Width <= 0 || r.Height <= 0
}

func (r Rect) Intersect(other Rect) Rect {
	if r.Empty() || other.Empty() {
		return Rect{}
	}
	left, top := max(r.X, other.X), max(r.Y, other.Y)
	right, bottom := min(r.X+r.Width, other.X+other.Width), min(r.Y+r.Height, other.Y+other.Height)
	if left >= right || top >= bottom {
		return Rect{}
	}
	return Rect{X: left, Y: top, Width: right - left, Height: bottom - top}
}

func finite(v float64) bool { return !math.IsNaN(v) && !math.IsInf(v, 0) }

// NormalizeSelection converts two drag endpoints into a top-left rectangle.
// Sub-pixel drags smaller than one logical pixel are treated as clicks so the
// capture flow does not attempt to create a zero-content image.
func NormalizeSelection(start, end Point) Rect {
	if !finite(start.X) || !finite(start.Y) || !finite(end.X) || !finite(end.Y) {
		return Rect{}
	}
	x, width := start.X, end.X-start.X
	if width < 0 {
		x, width = end.X, -width
	}
	y, height := start.Y, end.Y-start.Y
	if height < 0 {
		y, height = end.Y, -height
	}
	if !finite(width) || !finite(height) {
		return Rect{}
	}
	if width < 1 {
		width = 0
	}
	if height < 1 {
		height = 0
	}
	return Rect{X: x, Y: y, Width: width, Height: height}
}
