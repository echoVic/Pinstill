package capture

import "testing"

func TestNormalizeSelectionHandlesReverseDrag(t *testing.T) {
	got := NormalizeSelection(Point{X: 420, Y: 280}, Point{X: 120, Y: 80})
	want := Rect{X: 120, Y: 80, Width: 300, Height: 200}
	if got != want {
		t.Fatalf("NormalizeSelection() = %+v, want %+v", got, want)
	}
}

func TestNormalizeSelectionReportsEmptyClick(t *testing.T) {
	got := NormalizeSelection(Point{X: 12, Y: 24}, Point{X: 12, Y: 24})
	if !got.Empty() {
		t.Fatalf("Empty() = false for zero-size selection: %+v", got)
	}
}

func TestNormalizeSelectionClampsTinyDrag(t *testing.T) {
	got := NormalizeSelection(Point{X: 10, Y: 10}, Point{X: 10.2, Y: 10.4})
	if got.Width != 0 || got.Height != 0 {
		t.Fatalf("tiny drag = %+v, want zero-size selection", got)
	}
}

func TestNormalizeSelectionAllDragDirections(t *testing.T) {
	want := Rect{X: 10, Y: 20, Width: 100, Height: 70}
	tests := []struct {
		name       string
		start, end Point
	}{
		{name: "right-down", start: Point{X: 10, Y: 20}, end: Point{X: 110, Y: 90}},
		{name: "right-up", start: Point{X: 10, Y: 90}, end: Point{X: 110, Y: 20}},
		{name: "left-down", start: Point{X: 110, Y: 20}, end: Point{X: 10, Y: 90}},
		{name: "left-up", start: Point{X: 110, Y: 90}, end: Point{X: 10, Y: 20}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := NormalizeSelection(tt.start, tt.end); got != want {
				t.Fatalf("NormalizeSelection(%+v, %+v) = %+v, want %+v", tt.start, tt.end, got, want)
			}
		})
	}
}

func TestNormalizeSelectionNegativeScreenCoordinates(t *testing.T) {
	tests := []struct {
		name       string
		start, end Point
		want       Rect
	}{
		{
			name:  "drag crossing the primary origin",
			start: Point{X: -500, Y: 100},
			end:   Point{X: 300, Y: 400},
			want:  Rect{X: -500, Y: 100, Width: 800, Height: 300},
		},
		{
			name:  "both endpoints negative",
			start: Point{X: -200, Y: -300},
			end:   Point{X: -100, Y: -100},
			want:  Rect{X: -200, Y: -300, Width: 100, Height: 200},
		},
		{
			name:  "drag toward the origin",
			start: Point{X: -100, Y: -100},
			end:   Point{X: 0, Y: 0},
			want:  Rect{X: -100, Y: -100, Width: 100, Height: 100},
		},
		{
			name:  "drag away from the origin",
			start: Point{X: 0, Y: 0},
			end:   Point{X: -100, Y: -100},
			want:  Rect{X: -100, Y: -100, Width: 100, Height: 100},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := NormalizeSelection(tt.start, tt.end); got != tt.want {
				t.Fatalf("NormalizeSelection(%+v, %+v) = %+v, want %+v", tt.start, tt.end, got, tt.want)
			}
		})
	}
}

func TestNormalizeSelectionKeepsOneLogicalPixel(t *testing.T) {
	// Exactly one point is the smallest meaningful selection; it must not be
	// clamped to zero like a sub-pixel drag.
	got := NormalizeSelection(Point{X: 5, Y: 6}, Point{X: 6, Y: 7})
	want := Rect{X: 5, Y: 6, Width: 1, Height: 1}
	if got != want {
		t.Fatalf("NormalizeSelection() = %+v, want %+v", got, want)
	}
	if got.Empty() {
		t.Fatalf("one-point selection reported as empty: %+v", got)
	}
}

func TestNormalizeSelectionClampsAxesIndependently(t *testing.T) {
	// A drag with no horizontal travel clears only the width; the vertical
	// extent is kept but the rectangle is still empty.
	got := NormalizeSelection(Point{X: 10, Y: 10}, Point{X: 10.4, Y: 50})
	if want := (Rect{X: 10, Y: 10, Width: 0, Height: 40}); got != want {
		t.Fatalf("vertical-only drag = %+v, want %+v", got, want)
	}
	if !got.Empty() {
		t.Fatalf("vertical-only drag reported as non-empty: %+v", got)
	}

	got = NormalizeSelection(Point{X: 10, Y: 10}, Point{X: 20, Y: 10.4})
	if want := (Rect{X: 10, Y: 10, Width: 10, Height: 0}); got != want {
		t.Fatalf("horizontal-only drag = %+v, want %+v", got, want)
	}
	if !got.Empty() {
		t.Fatalf("horizontal-only drag reported as non-empty: %+v", got)
	}
}

func TestNormalizeSelectionSubPixelDragAtNegativeCoordinates(t *testing.T) {
	got := NormalizeSelection(Point{X: -10.5, Y: -20.5}, Point{X: -10.4, Y: -20.4})
	want := Rect{X: -10.5, Y: -20.5}
	if got != want {
		t.Fatalf("NormalizeSelection() = %+v, want %+v", got, want)
	}
	if !got.Empty() {
		t.Fatalf("sub-pixel drag = %+v, want empty", got)
	}
}

func TestCaptureServiceNormalizeSelection(t *testing.T) {
	start, end := Point{X: -42, Y: 17}, Point{X: 8, Y: -3}
	want := NormalizeSelection(start, end)
	if got := (CaptureService{}).NormalizeSelection(start, end); got != want {
		t.Fatalf("CaptureService.NormalizeSelection() = %+v, want %+v", got, want)
	}
}
