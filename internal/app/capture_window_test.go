package app

import "testing"

type captureWindow struct {
	visible, minimized, hidden bool
}

func (w *captureWindow) IsVisible() bool   { return w.visible }
func (w *captureWindow) IsMinimized() bool { return w.minimized }
func (w *captureWindow) Hide()             { w.hidden = true }

func TestCaptureKeepsHiddenAndMinimizedWindows(t *testing.T) {
	for _, state := range []struct {
		name               string
		visible, minimized bool
		wantRestore        bool
	}{
		{"displayed", true, false, true},
		{"hidden", false, false, false},
		{"minimized macOS", true, true, false},
		{"minimized Windows", false, true, false},
	} {
		t.Run(state.name, func(t *testing.T) {
			w := &captureWindow{visible: state.visible, minimized: state.minimized}
			if restore := hideForCapture(w); restore != state.wantRestore || w.hidden != state.wantRestore {
				t.Fatalf("restore=%v hidden=%v; expected %v", restore, w.hidden, state.wantRestore)
			}
		})
	}
}
