//go:build darwin && integration

// These tests talk to the real /usr/sbin/screencapture and therefore need the
// process to be granted Screen Recording permission. They are opt-in:
//
//	go test -tags integration -run Integration -v ./internal/capture
package capture

import (
	"bytes"
	"image/png"
	"os"
	"strings"
	"testing"
	"time"
)

// TestIntegrationCaptureRegion captures a small region and checks that the
// system produced a real PNG whose aspect ratio matches the requested points.
func TestIntegrationCaptureRegion(t *testing.T) {
	if !PermissionGranted() {
		t.Skip("screen recording permission is not granted")
	}
	data, err := CaptureRegion(Rect{X: 100, Y: 100, Width: 200, Height: 150}, CaptureOptions{})
	if err != nil {
		t.Fatalf("CaptureRegion() error = %v", err)
	}
	config, err := png.DecodeConfig(bytes.NewReader(data))
	if err != nil {
		t.Fatalf("png.DecodeConfig() error = %v", err)
	}
	if config.Width < 200 || config.Height < 150 {
		t.Fatalf("captured image %dx%d is smaller than the requested 200x150 points", config.Width, config.Height)
	}
	if config.Width*150 != config.Height*200 {
		t.Fatalf("captured image %dx%d does not preserve the 4:3 aspect ratio of 200x150 points", config.Width, config.Height)
	}
	t.Logf("captured %d bytes, %dx%d pixels", len(data), config.Width, config.Height)
}

// TestIntegrationCaptureRegionIncludeCursor verifies that -C is accepted by
// the system utility (the cursor itself may be at any location).
func TestIntegrationCaptureRegionIncludeCursor(t *testing.T) {
	if !PermissionGranted() {
		t.Skip("screen recording permission is not granted")
	}
	data, err := CaptureRegion(Rect{X: 0, Y: 0, Width: 64, Height: 48}, CaptureOptions{IncludeCursor: true})
	if err != nil {
		t.Fatalf("CaptureRegion(IncludeCursor) error = %v", err)
	}
	if _, err := png.DecodeConfig(bytes.NewReader(data)); err != nil {
		t.Fatalf("captured data is not a PNG: %v", err)
	}
}

// TestIntegrationCaptureRegionDelay verifies that DelayMillis delays the grab
// before screencapture runs.
func TestIntegrationCaptureRegionDelay(t *testing.T) {
	if !PermissionGranted() {
		t.Skip("screen recording permission is not granted")
	}
	const delay = 200 * time.Millisecond
	start := time.Now()
	if _, err := CaptureRegion(Rect{X: 0, Y: 0, Width: 16, Height: 16}, CaptureOptions{DelayMillis: int(delay / time.Millisecond)}); err != nil {
		t.Fatalf("CaptureRegion(DelayMillis) error = %v", err)
	}
	if elapsed := time.Since(start); elapsed < delay {
		t.Fatalf("CaptureRegion() returned after %v, want at least %v", elapsed, delay)
	}
}

// TestIntegrationNoTempFileLeak verifies the temporary file is removed.
func TestIntegrationNoTempFileLeak(t *testing.T) {
	if !PermissionGranted() {
		t.Skip("screen recording permission is not granted")
	}
	before := countCaptureTempFiles(t)
	if _, err := CaptureRegion(Rect{X: 0, Y: 0, Width: 8, Height: 8}, CaptureOptions{}); err != nil {
		t.Fatalf("CaptureRegion() error = %v", err)
	}
	if after := countCaptureTempFiles(t); after != before {
		t.Fatalf("temporary files leaked: %d before, %d after", before, after)
	}
}

func countCaptureTempFiles(t *testing.T) int {
	t.Helper()
	entries, err := os.ReadDir(os.TempDir())
	if err != nil {
		t.Fatalf("os.ReadDir(%q) error = %v", os.TempDir(), err)
	}
	count := 0
	for _, entry := range entries {
		if strings.HasPrefix(entry.Name(), "pinstill-capture-") {
			count++
		}
	}
	return count
}
