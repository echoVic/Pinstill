//go:build !darwin

package app

import "github.com/egoist/mygo"

func nativePinClickThrough(*mygo.Window) (bool, bool) { return false, false }
