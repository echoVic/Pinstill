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
