package capture

import (
	"reflect"
	"testing"
)

func TestBuildScreencaptureArgsDefaults(t *testing.T) {
	got := buildScreencaptureArgs(Rect{X: 100, Y: 100, Width: 200, Height: 150}, CaptureOptions{})
	want := []string{"-x", "-t", "png", "-R", "100,100,200,150"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("buildScreencaptureArgs() = %v, want %v", got, want)
	}
}

func TestBuildScreencaptureArgsIncludeCursor(t *testing.T) {
	got := buildScreencaptureArgs(Rect{X: 0, Y: 0, Width: 10, Height: 10}, CaptureOptions{IncludeCursor: true})
	want := []string{"-x", "-t", "png", "-C", "-R", "0,0,10,10"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("buildScreencaptureArgs() = %v, want %v", got, want)
	}
}

func TestBuildScreencaptureArgsRoundsAndKeepsNegativeOrigin(t *testing.T) {
	got := buildScreencaptureArgs(Rect{X: -100.4, Y: -20.6, Width: 50.5, Height: 30.4}, CaptureOptions{})
	want := []string{"-x", "-t", "png", "-R", "-100,-21,51,30"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("buildScreencaptureArgs() = %v, want %v", got, want)
	}
}

func TestBuildScreencaptureArgsClampsSubPixelRegion(t *testing.T) {
	got := buildScreencaptureArgs(Rect{X: 10, Y: 10, Width: 0.2, Height: 0.3}, CaptureOptions{})
	want := []string{"-x", "-t", "png", "-R", "10,10,1,1"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("buildScreencaptureArgs() = %v, want %v", got, want)
	}
}

func TestBuildScreencaptureArgsIgnoresDelay(t *testing.T) {
	// The delay is implemented in Go (sleepBeforeCapture) instead of relying on
	// screencapture's -T flag, so it must not show up in the argument vector.
	got := buildScreencaptureArgs(Rect{X: 1, Y: 2, Width: 3, Height: 4}, CaptureOptions{DelayMillis: 500, IncludeCursor: true})
	want := []string{"-x", "-t", "png", "-C", "-R", "1,2,3,4"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("buildScreencaptureArgs() = %v, want %v", got, want)
	}
}

func TestBuildScreencaptureArgsEmptyRectYieldsZeroRectangle(t *testing.T) {
	got := buildScreencaptureArgs(Rect{}, CaptureOptions{})
	want := []string{"-x", "-t", "png", "-R", "0,0,0,0"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("buildScreencaptureArgs() = %v, want %v", got, want)
	}
}

func TestBuildScreencaptureArgsDegenerateRectYieldsZeroRectangle(t *testing.T) {
	for _, rect := range []Rect{
		{X: 10, Y: 20, Width: 0, Height: 30},
		{X: 10, Y: 20, Width: 30, Height: 0},
		{X: 10, Y: 20, Width: -10, Height: 30},
		{X: 10, Y: 20, Width: 30, Height: -10},
	} {
		got := buildScreencaptureArgs(rect, CaptureOptions{})
		want := []string{"-x", "-t", "png", "-R", "0,0,0,0"}
		if !reflect.DeepEqual(got, want) {
			t.Fatalf("buildScreencaptureArgs(%+v) = %v, want %v", rect, got, want)
		}
	}
}

func TestBuildScreencaptureArgsCrossScreenBoundary(t *testing.T) {
	tests := []struct {
		name string
		rect Rect
		want []string
	}{
		{
			name: "display left of the primary",
			rect: Rect{X: -1440, Y: -900, Width: 2880, Height: 1800},
			want: []string{"-x", "-t", "png", "-R", "-1440,-900,2880,1800"},
		},
		{
			name: "rectangle crossing the primary origin",
			rect: Rect{X: -960, Y: 0, Width: 1920, Height: 1080},
			want: []string{"-x", "-t", "png", "-R", "-960,0,1920,1080"},
		},
		{
			name: "fractional cross-screen bounds round per component",
			rect: Rect{X: -960.5, Y: 0.5, Width: 1920.5, Height: 1080},
			want: []string{"-x", "-t", "png", "-R", "-961,1,1921,1080"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := buildScreencaptureArgs(tt.rect, CaptureOptions{}); !reflect.DeepEqual(got, tt.want) {
				t.Fatalf("buildScreencaptureArgs(%+v) = %v, want %v", tt.rect, got, tt.want)
			}
		})
	}
}

func TestBuildScreencaptureArgsHalfPointRounding(t *testing.T) {
	got := buildScreencaptureArgs(Rect{X: -0.5, Y: 0.5, Width: 1.5, Height: 2.5}, CaptureOptions{})
	want := []string{"-x", "-t", "png", "-R", "-1,1,2,3"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("buildScreencaptureArgs() = %v, want %v", got, want)
	}
}
