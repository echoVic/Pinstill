package capture

import (
	"image"
	"math"
	"testing"
)

func TestMonitorPixelRectMatchesMyGo09(t *testing.T) {
	primary := monitorGeometry{image.Rect(0, 0, 3840, 2160), 192}
	for _, tt := range []struct {
		name     string
		monitors []monitorGeometry
		rect     Rect
		want     [4]int
	}{
		{"100 percent", []monitorGeometry{{image.Rect(0, 0, 1920, 1080), 96}}, Rect{100, 50, 800, 600}, [4]int{100, 50, 800, 600}},
		{"150 percent", []monitorGeometry{{image.Rect(0, 0, 2880, 1800), 144}}, Rect{100, 50, 800, 600}, [4]int{150, 75, 1200, 900}},
		{"200 percent", []monitorGeometry{primary}, Rect{100, 50, 800, 600}, [4]int{200, 100, 1600, 1200}},
		// MyGo's #1039 fixture: the origin is 3840/1.75 rounded = 2194
		// DIPs, not 3840/2 and not 3840. Offsets then use 175%.
		{"mixed right", []monitorGeometry{primary, {image.Rect(3840, 0, 6400, 1600), 168}}, Rect{2494, 200, 800, 500}, [4]int{4365, 350, 1400, 875}},
		{"mixed left", []monitorGeometry{primary, {image.Rect(-2560, 0, 0, 1440), 96}}, Rect{-2460, 50, 800, 600}, [4]int{-2460, 50, 800, 600}},
		{"mixed above", []monitorGeometry{{image.Rect(0, 0, 2880, 1800), 144}, {image.Rect(200, -1440, 2760, 0), 96}}, Rect{300, -1340, 800, 600}, [4]int{300, -1340, 800, 600}},
		{"scaled left origin", []monitorGeometry{{image.Rect(0, 0, 2560, 1600), 120}, {image.Rect(-1920, 300, 0, 1380), 144}}, Rect{-1436, 340, 600, 400}, [4]int{-1770, 450, 900, 600}},
		{"negative half rounds up", []monitorGeometry{{image.Rect(-2880, 0, 0, 1800), 144}}, Rect{-1919, -1, 10, 10}, [4]int{-2878, -1, 15, 15}},
		{"cross monitor round trip", []monitorGeometry{{image.Rect(0, 0, 2560, 1440), 96}, {image.Rect(2560, 0, 6400, 2160), 192}}, Rect{2280, 50, 750, 400}, [4]int{2000, 100, 1500, 800}},
	} {
		t.Run(tt.name, func(t *testing.T) {
			got, err := monitorPixelRect(tt.rect, tt.monitors)
			if err != nil || got != tt.want {
				t.Fatalf("got %v, %v; want %v", got, err, tt.want)
			}
		})
	}
}

func TestMonitorPixelRectRejectsUnsafeBounds(t *testing.T) {
	monitors := []monitorGeometry{{image.Rect(0, 0, 3840, 2160), 192}}
	for _, rect := range []Rect{{math.MaxInt32, 0, 100, 100}, {0, math.MinInt32, 100, 100}, {0, 0, 4096, 4096}, {0, 0, math.MaxFloat64, 100}, {math.NaN(), 0, 100, 100}} {
		if _, err := monitorPixelRect(rect, monitors); err == nil {
			t.Errorf("accepted unsafe rectangle %+v", rect)
		}
	}
	if _, err := monitorPixelRect(Rect{0, 0, 10, 10}, nil); err == nil {
		t.Fatal("accepted absent monitor geometry")
	}
}

func TestClipPixelRectCheckedEdges(t *testing.T) {
	got, err := clipPixelRect([4]int{-110, -60, 40, 30}, [4]int{-100, -50, 200, 100})
	if err != nil || got != [4]int{-100, -50, 30, 20} {
		t.Fatalf("clip = %v, %v", got, err)
	}
	for _, rect := range [][4]int{{math.MaxInt32, 0, 10, 10}, {0, 0, 1, math.MaxInt32}, {int(^uint(0) >> 1), 0, 1, 1}} {
		if _, err := clipPixelRect(rect, [4]int{0, 0, 100, 100}); err == nil {
			t.Errorf("unsafe bounds accepted: %v", rect)
		}
	}
}
