//go:build darwin

package app

import (
	"github.com/ebitengine/purego/objc"
	"github.com/egoist/mygo"
)

func clotheslineAboveNormalWindow(line, normal *mygo.Window) bool {
	return objc.Send[int64](objc.ID(line.NativeHandle()), objc.RegisterName("level")) >
		objc.Send[int64](objc.ID(normal.NativeHandle()), objc.RegisterName("level"))
}
