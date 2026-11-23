//go:build !darwin

package app

import "github.com/egoist/mygo"

func clotheslineAboveNormalWindow(line, _ *mygo.Window) bool { return line.IsAlwaysOnTop() }
