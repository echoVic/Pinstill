//go:build darwin

package capture

import (
	"os"
	"testing"
)

func TestVisibleWindowsDarwin(t *testing.T) {
	selfPID := os.Getpid()
	windows := VisibleWindows(selfPID)
	for i, w := range windows {
		if w.PID == selfPID {
			t.Fatalf("VisibleWindows(%d) included self process window at index %d: %+v", selfPID, i, w)
		}
		if w.Rect.Empty() || w.Rect.Width < 12 || w.Rect.Height < 12 {
			t.Fatalf("VisibleWindows(%d) returned invalid rect at index %d: %+v", selfPID, i, w)
		}
	}
	t.Logf("enumerated %d visible windows on darwin", len(windows))
}
