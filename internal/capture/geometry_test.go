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

func TestRectEmpty(t *testing.T) {
	tests := []struct {
		name string
		rect Rect
		want bool
	}{
		{name: "zero value", rect: Rect{}, want: true},
		{name: "zero width", rect: Rect{X: 1, Y: 2, Width: 0, Height: 10}, want: true},
		{name: "zero height", rect: Rect{X: 1, Y: 2, Width: 10, Height: 0}, want: true},
		{name: "negative width", rect: Rect{X: 1, Y: 2, Width: -1, Height: 10}, want: true},
		{name: "negative height", rect: Rect{X: 1, Y: 2, Width: 10, Height: -0.5}, want: true},
		{name: "positive sub-pixel area", rect: Rect{X: -5, Y: -5, Width: 0.5, Height: 0.5}, want: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.rect.Empty(); got != tt.want {
				t.Fatalf("Rect%+v.Empty() = %v, want %v", tt.rect, got, tt.want)
			}
		})
	}
}

func TestPixelRectCrossScreenNegativeOrigins(t *testing.T) {
	tests := []struct {
		name  string
		rect  Rect
		scale float64
		want  [4]int
	}{
		{
			name:  "display left of the primary with retina scale",
			rect:  Rect{X: -1440, Y: -900, Width: 2880, Height: 1800},
			scale: 2,
			want:  [4]int{-2880, -1800, 5760, 3600},
		},
		{
			name:  "rectangle crossing the primary origin",
			rect:  Rect{X: -100, Y: 0, Width: 200, Height: 100},
			scale: 2,
			want:  [4]int{-200, 0, 400, 200},
		},
		{
			name:  "fractional scale rounds each edge",
			rect:  Rect{X: 10, Y: 20, Width: 100, Height: 100},
			scale: 1.5,
			want:  [4]int{15, 30, 150, 150},
		},
		{
			name:  "sub-pixel negative origin rounds to zero",
			rect:  Rect{X: -0.4, Y: -0.6, Width: 10, Height: 10},
			scale: 1,
			want:  [4]int{0, -1, 10, 10},
		},
		{
			name:  "sub-pixel size still yields one pixel at half scale",
			rect:  Rect{X: 0, Y: 0, Width: 0.2, Height: 0.2},
			scale: 0.5,
			want:  [4]int{0, 0, 1, 1},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			x, y, w, h := PixelRect(tt.rect, tt.scale)
			if got := [4]int{x, y, w, h}; got != tt.want {
				t.Fatalf("PixelRect(%+v, %v) = %v, want %v", tt.rect, tt.scale, got, tt.want)
			}
		})
	}
}

func TestPointRectEmptyOrDegenerate(t *testing.T) {
	for _, rect := range []Rect{
		{},
		{X: 10, Y: 20, Width: 0, Height: 30},
		{X: 10, Y: 20, Width: 30, Height: 0},
		{X: 10, Y: 20, Width: -10, Height: 30},
		{X: 10, Y: 20, Width: 30, Height: -10},
	} {
		x, y, w, h := pointRect(rect)
		if got := [4]int{x, y, w, h}; got != [4]int{0, 0, 0, 0} {
			t.Fatalf("pointRect(%+v) = %v, want zeros", rect, got)
		}
	}
}

func TestPointRectRoundsHalfPointsAwayFromZero(t *testing.T) {
	tests := []struct {
		name string
		rect Rect
		want [4]int
	}{
		{
			name: "positive halves",
			rect: Rect{X: 0.5, Y: 1.5, Width: 2.5, Height: 3.5},
			want: [4]int{1, 2, 3, 4},
		},
		{
			name: "negative halves",
			rect: Rect{X: -0.5, Y: -1.5, Width: 2.5, Height: 0.5},
			want: [4]int{-1, -2, 3, 1},
		},
		{
			name: "sub-point sizes clamp to one point",
			rect: Rect{X: 10, Y: 10, Width: 0.4, Height: 0.4},
			want: [4]int{10, 10, 1, 1},
		},
		{
			name: "cross-screen negative half points",
			rect: Rect{X: -960.5, Y: -0.5, Width: 1920.5, Height: 1080},
			want: [4]int{-961, -1, 1921, 1080},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			x, y, w, h := pointRect(tt.rect)
			if got := [4]int{x, y, w, h}; got != tt.want {
				t.Fatalf("pointRect(%+v) = %v, want %v", tt.rect, got, tt.want)
			}
		})
	}
}
