//go:build darwin

package app

import (
	"github.com/ebitengine/purego/objc"
	"github.com/egoist/mygo"
)

const supportsAcrossDisplayOverlay = true

func configureCaptureOverlay(window *mygo.Window) {
	native := objc.ID(window.NativeHandle())
	const nonActivatingBorderlessPanel = 1 << 7
	const popUpMenuWindowLevel = 101
	objc.Send[objc.ID](native, objc.RegisterName("setStyleMask:"), uintptr(nonActivatingBorderlessPanel))
	objc.Send[objc.ID](native, objc.RegisterName("setLevel:"), uintptr(popUpMenuWindowLevel))
}

func captureOverlayBounds(display mygo.Display) mygo.Rectangle {
	return display.Bounds
}
