package app

import (
	"fmt"
	"strings"
	"time"

	"github.com/echoVic/Pinstill/internal/capture"
	"github.com/echoVic/Pinstill/internal/imagebudget"
	"github.com/egoist/mygo"
)

func (a *App) selftestPrecision() {
	if a.main != nil {
		a.main.Hide()
	}
	display := mygo.Screen.DisplayNearestPoint(mygo.Screen.CursorScreenPoint())
	workspace := display.WorkArea
	fixture := mygo.NewWindow(mygo.WindowOptions{
		Title:           "Pinstill Pixel Test",
		URL:             "/frame.html",
		Page:            mygo.PageOptions{Session: a.pageSession},
		X:               workspace.X + 200,
		Y:               workspace.Y + 200,
		Width:           240,
		Height:          180,
		Frameless:       true,
		AlwaysOnTop:     true,
		BackgroundColor: "#a05040",
	})
	time.Sleep(550 * time.Millisecond)
	selftestValue(fixture.Page().Eval(`document.body.replaceChildren(); document.documentElement.style.background = '#a05040'`))
	time.Sleep(250 * time.Millisecond)
	fixtureBounds := fixture.ContentBounds()
	point := capture.Point{X: float64(fixtureBounds.X + 100), Y: float64(fixtureBounds.Y + 80)}
	region, scale := pixelSampleRegion(point, []mygo.Display{display})
	data, err := a.captureRegion(region, capture.CaptureOptions{BackingScale: scale})
	selftestCheckError(err)
	frame, err := imagebudget.DecodePNG(data)
	selftestCheckError(err)
	baseline, err := makePixelSample(frame, region, point)
	selftestCheckError(err)
	selftestRequire(baseline.Hex != "#000000", "the sample fixture was not visible")
	a.visibleWindows = func(int) []capture.WindowInfo {
		return []capture.WindowInfo{{Rect: asCaptureRect(fixtureBounds), Title: "Pixel fixture"}}
	}
	type copyResult struct {
		action string
		data   []byte
	}
	copied := make(chan copyResult, 1)
	a.captureAction = func(action string, data []byte, _ capturePayload) (CaptureActionResult, error) {
		copied <- copyResult{action: action, data: data}
		return CaptureActionResult{Completed: true}, nil
	}
	selftestCheckError(a.beginRegion())
	fixture.Hide()
	time.Sleep(750 * time.Millisecond)
	selftestRequire(a.overlayVisible(), "overlay did not appear")
	selftestRequire(selftestValue(a.overlay.Page().Eval(`(() => {
		const backdrop = document.getElementById('overlayBackdrop');
		return !backdrop.hidden && backdrop.naturalWidth > 0;
	})()`)) == true, "the frozen desktop backdrop was not displayed")
	origin := a.overlayInfo()
	selftestValue(a.overlay.Page().Eval(fmt.Sprintf(`window.dispatchEvent(new PointerEvent('pointermove', {clientX: %g, clientY: %g})); window.dispatchEvent(new KeyboardEvent('keydown', {key: 'z'}));`, point.X-float64(origin.DisplayX), point.Y-float64(origin.DisplayY))))
	deadline := time.Now().Add(4 * time.Second)
	var reading string
	for time.Now().Before(deadline) {
		reading = fmt.Sprint(selftestValue(a.overlay.Page().Eval(`document.getElementById('precisionColor').textContent`)))
		if strings.HasPrefix(reading, "#") {
			break
		}
		time.Sleep(75 * time.Millisecond)
	}
	selftestRequire(strings.EqualFold(reading, baseline.Hex), "expected frozen color %s after fixture was hidden, got %s", baseline.Hex, reading)
	selftestValue(a.overlay.Page().Eval(fmt.Sprintf(`(() => {
		const opts = {clientX: %g, clientY: %g, pointerId: 1, pointerType: 'mouse', button: 0};
		for (const event of ['pointerdown', 'pointerup', 'pointerdown', 'pointerup']) {
			window.dispatchEvent(new PointerEvent(event, opts));
		}
	})()`, point.X-float64(origin.DisplayX), point.Y-float64(origin.DisplayY))))
	select {
	case result := <-copied:
		selftestRequire(result.action == "copy", "expected quick copy, got %s", result.action)
		shot, err := imagebudget.DecodePNG(result.data)
		selftestCheckError(err)
		sample, err := makePixelSample(shot, asCaptureRect(fixtureBounds), point)
		selftestCheckError(err)
		selftestRequire(sample.Hex == baseline.Hex, "quick copy lost the hidden fixture: expected %s, got %s", baseline.Hex, sample.Hex)
	case <-time.After(5 * time.Second):
		selftestFail("double click did not copy the frozen pixels")
	}
	fixture.Close()
	selftestExit(0)
}
