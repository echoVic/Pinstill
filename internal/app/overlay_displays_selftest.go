package app

import (
	"encoding/json"
	"fmt"
	"slices"
	"time"

	"github.com/echoVic/Pinstill/internal/capture"
)

func (a *App) selftestMultiscreen() {
	selftestCheckError(a.beginRegion())
	defer a.cancelRegion()
	time.Sleep(800 * time.Millisecond)
	a.mu.Lock()
	panes := slices.Clone(a.overlaySurfaces)
	a.mu.Unlock()
	if len(panes) < 2 {
		fmt.Println("SELFTEST multiscreen: skipped; two displays required")
		selftestExit(0)
	}
	destination, source := panes[0], panes[1]
	selftestRequire(source.window.IsVisible() && destination.window.IsVisible(), "both display overlays must be visible")
	selftestRequire(source.window.ContentBounds() == source.display.Bounds && destination.window.ContentBounds() == destination.display.Bounds,
		"overlays do not cover both displays: %+v %+v", source.window.ContentBounds(), destination.window.ContentBounds())
	start := capture.Point{X: float64(source.bounds.Width / 2), Y: float64(source.bounds.Height / 2)}
	end := capture.Point{
		X: float64(destination.bounds.X + destination.bounds.Width/2 - source.bounds.X),
		Y: float64(destination.bounds.Y + destination.bounds.Height/3 - source.bounds.Y),
	}
	sel := capture.NormalizeSelection(start, end)
	if sel.Width < 24 {
		end.X += 24
	}
	if sel.Height < 24 {
		end.Y += 24
	}
	sel = capture.NormalizeSelection(start, end)
	startScript := `(() => {
		window.dispatchEvent(new PointerEvent('pointerdown', { pointerId: 1, button: 0, clientX: %g, clientY: %g }));
		window.dispatchEvent(new PointerEvent('pointermove', { pointerId: 1, button: 0, clientX: %g, clientY: %g }));
	})()`
	selftestValue(source.window.Page().Eval(fmt.Sprintf(startScript, start.X, start.Y, end.X, end.Y)))
	expectedPreview := projectOverlayRect(Rect(sel), source.bounds, destination.bounds)
	previewJSON, err := json.Marshal(expectedPreview)
	selftestCheckError(err)
	deadline := time.Now().Add(3 * time.Second)
	mirrored := false
	for time.Now().Before(deadline) {
		if selftestValue(destination.window.Page().Eval(fmt.Sprintf(`(() => {
			const selection = document.getElementById('selection');
			const expected = %s;
			return !selection.hidden && parseFloat(selection.style.left) === expected.x && parseFloat(selection.style.top) === expected.y && parseFloat(selection.style.width) === expected.width && parseFloat(selection.style.height) === expected.height;
		})()`, previewJSON))) == true {
			mirrored = true
			break
		}
		time.Sleep(50 * time.Millisecond)
	}
	selftestRequire(mirrored, "the other display did not mirror the drag")
	selftestValue(source.window.Page().Eval(fmt.Sprintf(`window.dispatchEvent(new PointerEvent('pointerup', { pointerId: 1, button: 0, clientX: %g, clientY: %g }))`, end.X, end.Y)))
	selftestCheckError(a.waitOverlayAnnotating(8 * time.Second))
	selftestRequire(!destination.window.IsVisible() && source.window.IsVisible(), "annotation should remain in the requesting overlay")
	fitted := selftestValue(source.window.Page().Eval(`(() => {
		const frame = document.getElementById('selection').getBoundingClientRect();
		return frame.x >= 0 && frame.y >= 0 && frame.right <= innerWidth && frame.bottom <= innerHeight;
	})()`))
	selftestRequire(fitted == true, "cross-display annotation was clipped in its source window")
	fmt.Printf("SELFTEST multiscreen: displays=%d source=%+v other=%+v capture=%+v\n", len(panes), source.bounds, destination.bounds, sel)
	selftestExit(0)
}
