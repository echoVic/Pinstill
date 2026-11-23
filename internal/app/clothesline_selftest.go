package app

import (
	"fmt"
	"time"

	"github.com/egoist/mygo"
)

func (a *App) selftestClothesline() {
	pngBytes := syntheticScreenshot()
	selftestCheckError(a.addClotheslineImage(pngBytes, "第一张截图"))
	selftestCheckError(a.addClotheslineImage(pngBytes, "第二张截图"))
	selftestCheckError(a.addClotheslineImage(pngBytes, "第三张截图"))
	var window *mygo.Window
	mygo.RunOnMain(func() { window = a.lineWindow })
	selftestRequire(window != nil, "clothesline window was not created")
	selftestWaitClothesline("three decoded thumbnails", func() bool {
		return window.IsVisible() && selftestClotheslinePage(window, `(() => {
			const images = [...document.querySelectorAll('.line-photo img')];
			return images.length === 3 && images.every(image => image.naturalWidth > 0);
		})()`)
	})
	selftestRequire(!window.IsFocused(), "clothesline took focus on show")
	bounds := window.Bounds()
	display := mygo.Screen.PrimaryDisplay()
	workArea := display.WorkArea
	if workArea.Width <= 0 || workArea.Height <= 0 {
		workArea = display.Bounds
	}
	selftestRequire(bounds.Y >= workArea.Y && bounds.Y-workArea.Y <= 16,
		"clothesline y=%d must be within 16 points of the top of the work area at y=%d", bounds.Y, workArea.Y)
	normal := mygo.NewWindow(mygo.WindowOptions{
		Page: mygo.PageOptions{Session: a.pageSession}, Title: "Pinstill selftest normal window",
		X: bounds.X, Y: bounds.Y, Width: bounds.Width, Height: bounds.Height, Hidden: true,
	})
	defer normal.Destroy()
	normal.Show()
	normal.Focus()
	selftestWaitClothesline("the overlapping ordinary window to take focus", normal.IsFocused)
	checkLayer := func() {
		var above bool
		mygo.RunOnMain(func() { above = clotheslineAboveNormalWindow(window, normal) })
		selftestRequire(window.IsVisible() && above, "clothesline is not above the focused ordinary window")
		selftestRequire(normal.IsFocused(), "clothesline show took focus from the ordinary window")
	}
	checkLayer()
	selftestRequire(selftestClotheslinePage(window, `(() => {
		document.querySelectorAll('.line-photo')[2].click();
		const button = document.getElementById('lineMoveLeft');
		const bounds = button.getBoundingClientRect();
		if (button.disabled || bounds.width === 0 || bounds.bottom > innerHeight) return false;
		button.click();
		return true;
	})()`), "clothesline move control is missing or clipped")
	selftestWaitClothesline("the updated order", func() bool {
		return selftestClotheslinePage(window, `document.querySelectorAll('.line-photo')[1]?.title === '第三张截图'`)
	})
	shot, err := window.CapturePage()
	selftestCheckError(err)
	selftestArtifact("pinstill-clothesline.png", shot)
	selftestRequire(selftestClotheslinePage(window, `(() => {
		document.getElementById('lineHide').click();
		return true;
	})()`), "clothesline page did not send the hide action")
	selftestWaitClothesline("hide", func() bool { return !window.IsVisible() })
	state, err := a.lineStore.Snapshot()
	selftestCheckError(err)
	selftestRequire(!state.Visible && len(state.Items) == 3 && state.Items[1].Name == "第三张截图", "clothesline did not persist order and visibility")
	a.toggleClothesline()
	selftestWaitClothesline("tray toggle to show it again", window.IsVisible)
	checkLayer()
	fmt.Printf("SELFTEST clothesline: images=%d top_margin=%d above_ordinary=true focus_preserved=true reorder=true restored=true\n", len(state.Items), bounds.Y-workArea.Y)
}

func selftestClotheslinePage(window *mygo.Window, script string) bool {
	value, err := mygo.EvalAs[bool](window.Page(), script)
	selftestCheckError(err)
	return value
}

func selftestWaitClothesline(label string, ready func() bool) {
	deadline := time.Now().Add(8 * time.Second)
	for time.Now().Before(deadline) {
		if ready() {
			return
		}
		time.Sleep(50 * time.Millisecond)
	}
	selftestFail("clothesline timed out waiting for %s", label)
}
