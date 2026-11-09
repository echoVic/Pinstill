//go:build darwin

package capture

import "testing"

func TestSupportedOnDarwin(t *testing.T) {
	if !Supported() {
		t.Fatal("Supported() = false on darwin, want true")
	}
}

func TestCaptureRegionRejectsEmptyRect(t *testing.T) {
	for _, rect := range []Rect{
		{},
		{X: 10, Y: 10, Width: 0, Height: 20},
		{X: 10, Y: 10, Width: 20, Height: -3},
	} {
		if _, err := CaptureRegion(rect, CaptureOptions{}); err == nil {
			t.Fatalf("CaptureRegion(%+v) returned nil error for empty rect", rect)
		}
	}
}

func TestPermissionGrantedIsStable(t *testing.T) {
	first := PermissionGranted()
	second := PermissionGranted()
	if first != second {
		t.Fatalf("PermissionGranted() is not stable: %v then %v", first, second)
	}
	// The value depends on the machine's TCC state; only log it for context.
	t.Logf("screen recording permission granted: %v", first)
}
