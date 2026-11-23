package app

import (
	"github.com/ebitengine/purego/objc"
	"github.com/egoist/mygo"
)

func nativePinClickThrough(window *mygo.Window) (bool, bool) {
	var ignored bool
	mygo.RunOnMain(func() {
		ignored = objc.Send[bool](objc.ID(window.NativeHandle()), objc.RegisterName("ignoresMouseEvents"))
	})
	return ignored, true
}
