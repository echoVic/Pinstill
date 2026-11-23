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
	selftestCheckError(a.beginRegion())
	time.Sleep(750 * time.Millisecond)
	selftestRequire(a.overlayVisible(), "overlay did not appear")
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
	selftestRequire(strings.EqualFold(reading, baseline.Hex), "expected raw color %s under overlay, got %s", baseline.Hex, reading)
	a.cancelRegion()
	fixture.Close()
	selftestExit(0)
}
