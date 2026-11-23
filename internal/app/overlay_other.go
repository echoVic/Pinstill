//go:build !darwin

package app

import "github.com/egoist/mygo"

const supportsAcrossDisplayOverlay = false

func configureCaptureOverlay(*mygo.Window) {}

func captureOverlayBounds(display mygo.Display) mygo.Rectangle {
	if display.WorkArea.Width > 0 && display.WorkArea.Height > 0 {
		return display.WorkArea
	}
	return display.Bounds
}
