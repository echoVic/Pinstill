package capture

import (
	"image"
	"math"
	"reflect"
	"testing"
)

func TestProjectWindowsToSurface(t *testing.T) {
	surface := Rect{X: -1920, Y: 24, Width: 1920, Height: 1056}
	input := []WindowInfo{
		{ID: 1, PID: 101, Owner: "Front", Title: "Doc", Rect: Rect{X: -1820, Y: 10, Width: 500, Height: 400}},
		// Same rounded rect as ID 1 once clipped to surface: should be deduplicated, keeping Front.
		{ID: 2, PID: 102, Owner: "BehindSame", Title: "Shadow", Rect: Rect{X: -1820.2, Y: 10, Width: 500.2, Height: 400}},
		// Completely outside surface.
		{ID: 3, PID: 103, Owner: "OtherMonitor", Rect: Rect{X: 100, Y: 100, Width: 400, Height: 300}},
		// Too small after clipping.
		{ID: 4, PID: 104, Owner: "TinyClip", Rect: Rect{X: -10, Y: 100, Width: 100, Height: 200}},
		// Non-finite rect.
		{ID: 5, PID: 105, Owner: "NaN", Rect: Rect{X: math.NaN(), Y: 50, Width: 100, Height: 100}},
		// Second valid window, spanning right edge.
		{ID: 6, PID: 106, Owner: "Lower", Title: "Notes", Rect: Rect{X: -400, Y: 200, Width: 600, Height: 300}},
		// Third valid window, excluded when limit=2.
		{ID: 7, PID: 107, Owner: "Third", Rect: Rect{X: -1500, Y: 300, Width: 300, Height: 200}},
	}

	got := ProjectWindowsToSurface(input, surface, 12, 2)
	want := []WindowInfo{
		{
			ID:    1,
			PID:   101,
			Owner: "Front",
			Title: "Doc",
			Rect:  Rect{X: 100, Y: 0, Width: 500, Height: 386},
		},
		{
			ID:    6,
			PID:   106,
			Owner: "Lower",
			Title: "Notes",
			Rect:  Rect{X: 1520, Y: 176, Width: 400, Height: 300},
		},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("ProjectWindowsToSurface() = %+v, want %+v", got, want)
	}

	if empty := ProjectWindowsToSurface(input, Rect{}, 12, 10); empty != nil {
		t.Fatalf("expected nil for empty surface, got %+v", empty)
	}
}

func TestPixelsToMonitorDIPRect(t *testing.T) {
	primary := monitorGeometry{image.Rect(0, 0, 3840, 2160), 192}
	for _, tt := range []struct {
		name     string
		monitors []monitorGeometry
		px       [4]int
		want     Rect
	}{
		{"100 percent", []monitorGeometry{{image.Rect(0, 0, 1920, 1080), 96}}, [4]int{100, 50, 800, 600}, Rect{100, 50, 800, 600}},
		{"150 percent", []monitorGeometry{{image.Rect(0, 0, 2880, 1800), 144}}, [4]int{150, 75, 1200, 900}, Rect{100, 50, 800, 600}},
		{"200 percent", []monitorGeometry{primary}, [4]int{200, 100, 1600, 1200}, Rect{100, 50, 800, 600}},
		{"mixed left negative", []monitorGeometry{primary, {image.Rect(-2560, 0, 0, 1440), 96}}, [4]int{-2460, 50, 800, 600}, Rect{-2460, 50, 800, 600}},
		{"cross monitor round trip", []monitorGeometry{{image.Rect(0, 0, 2560, 1440), 96}, {image.Rect(2560, 0, 6400, 2160), 192}}, [4]int{2000, 100, 1500, 800}, Rect{2280, 50, 750, 400}},
	} {
		t.Run(tt.name, func(t *testing.T) {
			got, ok := pixelsToMonitorDIPRect(tt.px, tt.monitors)
			if !ok {
				t.Fatalf("pixelsToMonitorDIPRect(%v) returned ok=false", tt.px)
			}
			if got != tt.want {
				t.Fatalf("got %+v, want %+v", got, tt.want)
			}
			// Round-trip back to physical pixels via monitorPixelRect.
			px, err := monitorPixelRect(got, tt.monitors)
			if err != nil || px != tt.px {
				t.Fatalf("round-trip monitorPixelRect(%+v) = %v, %v; want %v", got, px, err, tt.px)
			}
		})
	}

	if _, ok := pixelsToMonitorDIPRect([4]int{0, 0, 0, 10}, []monitorGeometry{primary}); ok {
		t.Fatal("expected ok=false for empty pixel rect")
	}
	if _, ok := pixelsToMonitorDIPRect([4]int{0, 0, 100, 100}, nil); ok {
		t.Fatal("expected ok=false for missing monitors")
	}
}
