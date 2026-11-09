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
