package app

import (
	"testing"
	"time"

	"github.com/echoVic/Pinstill/internal/pin"
	"github.com/egoist/mygo"
)

// testRect is a shorthand for the mygo.Rectangle literals used below.
func testRect(x, y, width, height int) mygo.Rectangle {
	return mygo.Rectangle{X: x, Y: y, Width: width, Height: height}
}

// testDisplay builds a display from its bounds and work area. The remaining
// mygo.Display fields are irrelevant to clampPinToDisplays (it only reads
// Bounds and WorkArea), so they stay at their zero values.
func testDisplay(bounds, workArea mygo.Rectangle) mygo.Display {
	return mygo.Display{Bounds: bounds, WorkArea: workArea}
}

// testPinState is the state template for the geometry table; each case only
// varies X/Y/Width/Height through withGeometry.
func testPinState() pin.State {
	return pin.State{
		ID:        "pin-test",
		File:      "pin-test.png",
		X:         0,
		Y:         0,
		Width:     400,
		Height:    300,
		Opacity:   80,
		CreatedAt: time.Date(2026, 11, 10, 21, 0, 0, 0, time.UTC),
	}
}

// withGeometry returns a copy of st with the requested window geometry.
func withGeometry(st pin.State, x, y, width, height int) pin.State {
	st.X, st.Y, st.Width, st.Height = x, y, width, height
	return st
}

// TestClampPinToDisplays covers the restore-time geometry clamp: a pin saved
// on a display layout that no longer exists (unplugged monitor, changed
// resolution, negative secondary display coordinates) must be pulled back
// into a reachable work area without changing its identity or state.
func TestClampPinToDisplays(t *testing.T) {
	// A typical dual display layout: the primary display on the left and a
	// secondary one to its right. Work areas exclude menu bar and Dock.
	primary := testDisplay(testRect(0, 0, 1728, 1117), testRect(0, 25, 1728, 1052))
	secondaryRight := testDisplay(testRect(1728, 0, 1512, 982), testRect(1728, 25, 1512, 957))
	// A display to the left of the primary has negative coordinates.
	secondaryLeft := testDisplay(testRect(-1512, 0, 1512, 982), testRect(-1512, 25, 1512, 957))
	primaryOnly := []mygo.Display{primary}
	allDisplays := []mygo.Display{secondaryLeft, primary, secondaryRight}

	tests := []struct {
		name     string
		state    pin.State
		displays []mygo.Display
		want     pin.State
	}{
		{
			name:     "inside a single work area is unchanged",
			state:    withGeometry(testPinState(), 100, 200, 400, 300),
			displays: primaryOnly,
			want:     withGeometry(testPinState(), 100, 200, 400, 300),
		},
		{
			name:     "left edge is pulled back to the work area",
			state:    withGeometry(testPinState(), -200, 200, 400, 300),
			displays: primaryOnly,
			want:     withGeometry(testPinState(), 0, 200, 400, 300),
		},
		{
			name:     "right edge is pulled back to the work area",
			state:    withGeometry(testPinState(), 1600, 200, 400, 300),
			displays: primaryOnly,
			want:     withGeometry(testPinState(), 1328, 200, 400, 300),
		},
		{
			name:     "top edge is pulled below the menu bar",
			state:    withGeometry(testPinState(), 100, -50, 400, 300),
			displays: primaryOnly,
			want:     withGeometry(testPinState(), 100, 25, 400, 300),
		},
		{
			name:     "bottom edge is pulled above the dock",
			state:    withGeometry(testPinState(), 100, 1000, 400, 300),
			displays: primaryOnly,
			want:     withGeometry(testPinState(), 100, 777, 400, 300),
		},
		{
			name:     "oversized pin is clamped to the work area",
			state:    withGeometry(testPinState(), 0, 0, 5000, 5000),
			displays: primaryOnly,
			want:     withGeometry(testPinState(), 0, 25, 1052, 1052),
		},
		{
			name:     "tiny pins retain their dimensions",
			state:    withGeometry(testPinState(), 10, 100, 4, 4),
			displays: primaryOnly,
			want:     withGeometry(testPinState(), 10, 100, 4, 4),
		},
		{
			name:     "pin on a negative-coordinate display stays in place",
			state:    withGeometry(testPinState(), -1400, 200, 400, 300),
			displays: allDisplays,
			want:     withGeometry(testPinState(), -1400, 200, 400, 300),
		},
		{
			name:     "pin beyond the left display is pulled back onto it",
			state:    withGeometry(testPinState(), -2000, 300, 400, 300),
			displays: allDisplays,
			want:     withGeometry(testPinState(), -1512, 300, 400, 300),
		},
		{
			name:     "pin centered on the right display uses its work area",
			state:    withGeometry(testPinState(), 1800, 5, 800, 500),
			displays: allDisplays,
			want:     withGeometry(testPinState(), 1800, 25, 800, 500),
		},
		{
			name:     "pin straddling two displays is clamped to the one under its center",
			state:    withGeometry(testPinState(), 1400, 200, 400, 300),
			displays: allDisplays,
			want:     withGeometry(testPinState(), 1328, 200, 400, 300),
		},
		{
			name:     "unreachable coordinates are pulled back onto the only display",
			state:    withGeometry(testPinState(), 9999, 9999, 400, 300),
			displays: primaryOnly,
			want:     withGeometry(testPinState(), 1328, 777, 400, 300),
		},
		{
			name:     "tiny pin on the right display retains its dimensions",
			state:    withGeometry(testPinState(), 1800, 100, 8, 8),
			displays: allDisplays,
			want:     withGeometry(testPinState(), 1800, 100, 8, 8),
		},
		{
			name:     "zero work area falls back to the display bounds",
			state:    withGeometry(testPinState(), 1200, 800, 400, 300),
			displays: []mygo.Display{testDisplay(testRect(0, 0, 1440, 900), mygo.Rectangle{})},
			want:     withGeometry(testPinState(), 1040, 600, 400, 300),
		},
		{
			name:     "no displays leaves the state untouched",
			state:    withGeometry(testPinState(), -999, 5000, 123, 456),
			displays: nil,
			want:     withGeometry(testPinState(), -999, 5000, 123, 456),
		},
		{
			name:     "empty display slice leaves the state untouched",
			state:    withGeometry(testPinState(), -999, 5000, 123, 456),
			displays: []mygo.Display{},
			want:     withGeometry(testPinState(), -999, 5000, 123, 456),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := clampPinToDisplays(tt.state, tt.displays)
			if got != tt.want {
				t.Fatalf("clampPinToDisplays() = %+v, want %+v", got, tt.want)
			}
			// A second restore pass must not move the pin again.
			if again := clampPinToDisplays(got, tt.displays); again != got {
				t.Fatalf("clampPinToDisplays() is not idempotent: %+v then %+v", got, again)
			}
		})
	}
}
