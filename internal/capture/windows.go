package capture

import "math"

// WindowInfo describes one visible top-level desktop window in screen DIPs
// (origin at primary display top-left), ordered front-to-back.
type WindowInfo struct {
	ID    uint64
	PID   int
	Owner string
	Title string
	Rect  Rect
}

// ProjectWindowsToSurface clips front-to-back desktop windows against surface,
// converts their coordinates into surface-local DIPs, filters out windows
// smaller than minSide in either dimension, and deduplicates identical rounded
// rectangles while preserving front-to-back order.
func ProjectWindowsToSurface(windows []WindowInfo, surface Rect, minSide float64, limit int) []WindowInfo {
	if surface.Empty() {
		return nil
	}
	if !finite(minSide) || minSide <= 0 {
		minSide = 1
	}
	surfaceRight := surface.X + surface.Width
	surfaceBottom := surface.Y + surface.Height
	if !finite(surfaceRight) || !finite(surfaceBottom) {
		return nil
	}

	seen := make(map[[4]int]struct{}, len(windows))
	var out []WindowInfo
	for _, w := range windows {
		if w.Rect.Empty() {
			continue
		}
		wRight := w.Rect.X + w.Rect.Width
		wBottom := w.Rect.Y + w.Rect.Height
		if !finite(wRight) || !finite(wBottom) {
			continue
		}
		x0 := max(w.Rect.X, surface.X)
		y0 := max(w.Rect.Y, surface.Y)
		x1 := min(wRight, surfaceRight)
		y1 := min(wBottom, surfaceBottom)
		local := Rect{
			X:      x0 - surface.X,
			Y:      y0 - surface.Y,
			Width:  x1 - x0,
			Height: y1 - y0,
		}
		if local.Empty() || local.Width < minSide || local.Height < minSide {
			continue
		}
		key := [4]int{
			int(math.Round(local.X)),
			int(math.Round(local.Y)),
			int(math.Round(local.Width)),
			int(math.Round(local.Height)),
		}
		if _, dup := seen[key]; dup {
			continue
		}
		seen[key] = struct{}{}
		projected := w
		projected.Rect = local
		out = append(out, projected)
		if limit > 0 && len(out) >= limit {
			break
		}
	}
	return out
}
