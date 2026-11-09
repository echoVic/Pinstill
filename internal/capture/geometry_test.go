package capture

import "testing"

func TestPixelRectScalesAndRounds(t *testing.T) {
	tests := []struct {
		name  string
		rect  Rect
		scale float64
		want  [4]int
	}{
		{
			name:  "retina doubles",
			rect:  Rect{X: 100, Y: 50, Width: 200, Height: 150},
			scale: 2,
			want:  [4]int{200, 100, 400, 300},
		},
		{
			name:  "rounds to nearest pixel",
			rect:  Rect{X: 10.4, Y: 20.6, Width: 100.5, Height: 50.4},
			scale: 2,
			want:  [4]int{21, 41, 201, 101},
		},
		{
			name:  "negative origin for secondary displays",
			rect:  Rect{X: -100.4, Y: -20.6, Width: 50, Height: 50},
			scale: 2,
			want:  [4]int{-201, -41, 100, 100},
		},
		{
			name:  "non retina scale",
			rect:  Rect{X: 10, Y: 20, Width: 300, Height: 200},
			scale: 1,
			want:  [4]int{10, 20, 300, 200},
		},
		{
			name:  "sub pixel size clamps to one pixel",
			rect:  Rect{X: 0, Y: 0, Width: 0.2, Height: 0.2},
			scale: 2,
			want:  [4]int{0, 0, 1, 1},
		},
		{
			name:  "half scale",
			rect:  Rect{X: 100, Y: 100, Width: 100, Height: 100},
			scale: 0.5,
			want:  [4]int{50, 50, 50, 50},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			x, y, w, h := PixelRect(tt.rect, tt.scale)
			got := [4]int{x, y, w, h}
			if got != tt.want {
				t.Fatalf("PixelRect(%+v, %v) = %v, want %v", tt.rect, tt.scale, got, tt.want)
			}
		})
	}
}

func TestPixelRectEmptyOrInvalidScale(t *testing.T) {
	for _, tt := range []struct {
		rect  Rect
		scale float64
	}{
		{rect: Rect{X: 10, Y: 10}, scale: 2},                          // zero size
		{rect: Rect{X: 10, Y: 10, Width: 100, Height: 50}, scale: 0},  // no scale
		{rect: Rect{X: 10, Y: 10, Width: 100, Height: 50}, scale: -2}, // bogus scale
		{rect: Rect{X: 10, Y: 10, Width: -100, Height: 50}, scale: 2}, // degenerate width
	} {
		x, y, w, h := PixelRect(tt.rect, tt.scale)
		if got := [4]int{x, y, w, h}; got != [4]int{0, 0, 0, 0} {
			t.Fatalf("PixelRect(%+v, %v) = %v, want zeros", tt.rect, tt.scale, got)
		}
	}
}

func TestPointRectRoundsPoints(t *testing.T) {
	x, y, w, h := pointRect(Rect{X: 100.4, Y: 99.6, Width: 200.5, Height: 149.4})
	if got, want := [4]int{x, y, w, h}, [4]int{100, 100, 201, 149}; got != want {
		t.Fatalf("pointRect() = %v, want %v", got, want)
	}
}
