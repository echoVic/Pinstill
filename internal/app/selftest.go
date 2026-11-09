package app

import (
	"bytes"
	"errors"
	"fmt"
	"image"
	"image/color"
	"image/draw"
	"image/png"
	"log"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/echoVic/Tupen/internal/capture"
	"github.com/egoist/mygo"
)

// runSelfTest exercises the native pieces without a user driving the app.
// Set TUPEN_SELFTEST to one of capture, overlay, history, pin, hotkeys or
// settings; it is a development aid, not part of the product.
func (a *App) runSelfTest(mode string) {
	time.Sleep(800 * time.Millisecond)
	switch mode {
	case "capture":
		a.selftestCapture()
	case "overlay":
		a.selftestOverlay()
	case "history":
		a.selftestHistory()
	case "pin":
		a.selftestPin()
	case "hotkeys":
		a.selftestHotkeys()
	case "settings":
		a.selftestSettings()
	case "deliver":
		a.selftestDeliver()
	case "uicapture":
		a.selftestUICapture()
	case "drag":
		a.selftestDrag()
	case "esc":
		a.selftestEscape()
	case "uiactions":
		a.selftestUIActions()
	case "hotkeypress":
		a.selftestHotkeyPress()
	case "demo":
		a.selftestDemo()
	case "showcase":
		a.selftestShowcase()
	case "quit":
		a.selftestQuit()
	case "flow":
		a.selftestFlow()
	case "pinpersist-setup":
		a.selftestPinPersistSetup()
	case "pinpersist-check":
		a.selftestPinPersistCheck()
	case "pinui":
		a.selftestPinUI()
	case "hold":
		a.selftestHold()
	default:
		log.Printf("tupen: unknown TUPEN_SELFTEST mode %q", mode)
	}
}

func (a *App) selftestCapture() {
	disp := mygo.Screen.PrimaryDisplay()
	b := disp.Bounds
	region := capture.Rect{
		X:      float64(b.X) + 120,
		Y:      float64(b.Y) + 120,
		Width:  600,
		Height: 400,
	}
	s := a.settingsSnapshot()
	if a.main != nil {
		a.main.Hide()
	}
	time.Sleep(200 * time.Millisecond)
	pngBytes, err := capture.CaptureRegion(region, capture.CaptureOptions{IncludeCursor: s.IncludeCursor})
	if err != nil {
		fmt.Printf("SELFTEST capture: ERROR %v\n", err)
		mygo.App.Exit(1)
		return
	}
	width, height := imageDimensions(pngBytes)
	if err := os.WriteFile("/tmp/tupen-selftest-capture.png", pngBytes, 0o644); err != nil {
		fmt.Printf("SELFTEST capture: write error %v\n", err)
	}
	fmt.Printf("SELFTEST capture: ok %dx%d bytes=%d scale=%.1f file=/tmp/tupen-selftest-capture.png\n",
		width, height, len(pngBytes), disp.ScaleFactor)
	if a.store != nil {
		entry, err := a.store.Save(pngBytes, time.Now())
		if err != nil {
			fmt.Printf("SELFTEST capture: history save error %v\n", err)
		} else {
			fmt.Printf("SELFTEST capture: history entry id=%s w=%d h=%d entries=%d\n",
				entry.ID, entry.Width, entry.Height, len(a.store.List()))
		}
	}
	if err := mygo.Clipboard.WriteImage(pngBytes); err != nil {
		fmt.Printf("SELFTEST capture: clipboard error %v\n", err)
	} else {
		fmt.Printf("SELFTEST capture: clipboard ok\n")
		if os.Getenv("TUPEN_SELFTEST_HOLD") != "" {
			fmt.Printf("SELFTEST clipboard: READY holding for external pbpaste\n")
			time.Sleep(20 * time.Second)
		}
	}
	mygo.App.Exit(0)
}

func (a *App) selftestOverlay() {
	if a.main != nil {
		a.main.Show()
	}
	time.Sleep(300 * time.Millisecond)
	if err := a.beginRegion(); err != nil {
		fmt.Printf("SELFTEST overlay: begin error %v\n", err)
		mygo.App.Exit(1)
		return
	}
	time.Sleep(1200 * time.Millisecond)
	if a.overlay != nil {
		disp0 := mygo.Screen.PrimaryDisplay()
		fmt.Printf("SELFTEST overlay: primary bounds=%+v cursor=%+v\n", disp0.Bounds, mygo.Screen.CursorScreenPoint())
		fmt.Printf("SELFTEST overlay: overlay bounds now=%+v content=%+v\n", a.overlay.Bounds(), a.overlay.ContentBounds())
		a.overlay.SetBounds(a.overlay.Bounds())
		time.Sleep(200 * time.Millisecond)
		fmt.Printf("SELFTEST overlay: after re-set bounds=%+v content=%+v\n", a.overlay.Bounds(), a.overlay.ContentBounds())
		state, err := a.overlay.Page().Eval("document.readyState + '|' + document.body.children.length + '|' + (document.getElementById('dim') ? getComputedStyle(document.getElementById('dim')).backgroundColor : 'no-dim')")
		fmt.Printf("SELFTEST overlay: page state=%v err=%v\n", state, err)
		fmt.Printf("SELFTEST overlay: window visible=%v bounds=%+v displayScale=%.1f\n",
			a.overlay.IsVisible(), a.overlay.Bounds(), a.overlayDisplay.ScaleFactor)
		if pagePNG, err := a.overlay.CapturePage(); err == nil {
			_ = os.WriteFile("/tmp/tupen-selftest-overlay-page.png", pagePNG, 0o644)
			fmt.Printf("SELFTEST overlay: page capture %d bytes -> /tmp/tupen-selftest-overlay-page.png\n", len(pagePNG))
		} else {
			fmt.Printf("SELFTEST overlay: page capture error %v\n", err)
		}
	}
	region := virtualDesktopRect()
	pngBytes, err := capture.CaptureRegion(region, capture.CaptureOptions{})
	if err != nil {
		fmt.Printf("SELFTEST overlay: capture error %v\n", err)
	} else {
		_ = os.WriteFile("/tmp/tupen-selftest-overlay.png", pngBytes, 0o644)
		width, height := imageDimensions(pngBytes)
		fmt.Printf("SELFTEST overlay: ok overlay shown, desktop %dx%d rect=%+v file=/tmp/tupen-selftest-overlay.png\n", width, height, region)
	}
	a.cancelRegion()
	time.Sleep(300 * time.Millisecond)
	mygo.App.Exit(0)
}

func (a *App) selftestHistory() {
	disp := mygo.Screen.PrimaryDisplay()
	b := disp.Bounds
	region := capture.Rect{X: float64(b.X) + 60, Y: float64(b.Y) + 60, Width: 500, Height: 320}
	pngBytes, err := capture.CaptureRegion(region, capture.CaptureOptions{})
	if err != nil {
		fmt.Printf("SELFTEST history: capture error %v\n", err)
		mygo.App.Exit(1)
		return
	}
	if a.store == nil {
		fmt.Printf("SELFTEST history: no store\n")
		mygo.App.Exit(1)
		return
	}
	entry, err := a.store.Save(pngBytes, time.Now())
	if err != nil {
		fmt.Printf("SELFTEST history: save error %v\n", err)
		mygo.App.Exit(1)
		return
	}
	list := a.historyEntries()
	fmt.Printf("SELFTEST history: saved id=%s entries=%d firstURL=%s thumbURL=%s\n",
		entry.ID, len(list), list[0].URL, list[0].ThumbURL)
	mygo.App.Exit(0)
}

func (a *App) selftestPin() {
	disp := mygo.Screen.PrimaryDisplay()
	b := disp.Bounds
	region := capture.Rect{X: float64(b.X) + 90, Y: float64(b.Y) + 90, Width: 520, Height: 360}
	if a.main != nil {
		a.main.Hide()
	}
	time.Sleep(200 * time.Millisecond)
	pngBytes, err := capture.CaptureRegion(region, capture.CaptureOptions{})
	if err != nil {
		fmt.Printf("SELFTEST pin: capture error %v\n", err)
		mygo.App.Exit(1)
		return
	}
	id, err := a.pinPNG(pngBytes, 0, 0)
	if err != nil {
		fmt.Printf("SELFTEST pin: pin error %v\n", err)
		mygo.App.Exit(1)
		return
	}
	fmt.Printf("SELFTEST pin: window id=%s\n", id)
	time.Sleep(1000 * time.Millisecond)
	full := capture.Rect{X: float64(b.X), Y: float64(b.Y), Width: float64(b.Width), Height: float64(b.Height)}
	snap, err := capture.CaptureRegion(full, capture.CaptureOptions{})
	if err != nil {
		fmt.Printf("SELFTEST pin: verify capture error %v\n", err)
	} else {
		_ = os.WriteFile("/tmp/tupen-selftest-pin.png", snap, 0o644)
		fmt.Printf("SELFTEST pin: screenshot /tmp/tupen-selftest-pin.png\n")
	}
	if err := a.closePin(id); err != nil {
		fmt.Printf("SELFTEST pin: close error %v\n", err)
	}
	time.Sleep(200 * time.Millisecond)
	mygo.App.Exit(0)
}

// selftestDeliver runs a real capture through the app's delivery path and
// screenshots the main window, proving the editor shows a real image.
func (a *App) selftestDeliver() {
	disp := mygo.Screen.PrimaryDisplay()
	b := disp.Bounds
	region := capture.Rect{X: float64(b.X) + 140, Y: float64(b.Y) + 140, Width: 640, Height: 420}
	if a.main != nil {
		a.main.Hide()
	}
	go a.captureDeliver(region, "region")
	time.Sleep(4500 * time.Millisecond)
	mux := virtualDesktopRect()
	snap, err := capture.CaptureRegion(mux, capture.CaptureOptions{})
	if err != nil {
		fmt.Printf("SELFTEST deliver: verify capture error %v\n", err)
	} else {
		_ = os.WriteFile("/tmp/tupen-selftest-deliver.png", snap, 0o644)
		fmt.Printf("SELFTEST deliver: screenshot /tmp/tupen-selftest-deliver.png\n")
	}
	if a.main != nil {
		if pagePNG, err := a.main.CapturePage(); err == nil {
			_ = os.WriteFile("/tmp/tupen-selftest-editor-page.png", pagePNG, 0o644)
			fmt.Printf("SELFTEST deliver: editor page capture %d bytes -> /tmp/tupen-selftest-editor-page.png\n", len(pagePNG))
		} else {
			fmt.Printf("SELFTEST deliver: editor page capture error %v\n", err)
		}
	}
	mygo.App.Exit(0)
}

// selftestUICapture walks the main window's views and saves a clean
// screenshot of each, for UI review without manual clicking.
func (a *App) selftestUICapture() {
	if a.main == nil {
		fmt.Printf("SELFTEST uicapture: no main window\n")
		mygo.App.Exit(1)
		return
	}
	a.showMain()
	time.Sleep(1500 * time.Millisecond)
	for _, d := range mygo.Screen.Displays() {
		fmt.Printf("SELFTEST uicapture: display id=%d label=%q bounds=%+v work=%+v scale=%.1f primary=%v\n",
			d.ID, d.Label, d.Bounds, d.WorkArea, d.ScaleFactor, d.Primary)
	}
	if a.main != nil {
		fmt.Printf("SELFTEST uicapture: main bounds=%+v content=%+v visible=%v\n", a.main.Bounds(), a.main.ContentBounds(), a.main.IsVisible())
	}
	a.mu.Lock()
	trayOK := a.tray != nil
	a.mu.Unlock()
	fmt.Printf("SELFTEST uicapture: tray=%v\n", trayOK)
	save := func(name string) {
		png, err := a.main.CapturePage()
		if err != nil {
			fmt.Printf("SELFTEST uicapture: %s error %v\n", name, err)
			return
		}
		_ = os.WriteFile("/tmp/tupen-ui-"+name+".png", png, 0o644)
		width, height := imageDimensions(png)
		fmt.Printf("SELFTEST uicapture: %s %dx%d -> /tmp/tupen-ui-%s.png\n", name, width, height, name)
	}
	save("rack")
	for _, view := range []string{"history", "settings"} {
		if _, err := a.main.Page().Eval(`document.querySelector('[data-view="` + view + `"]').click()`); err != nil {
			fmt.Printf("SELFTEST uicapture: switch %s error %v\n", view, err)
		}
		time.Sleep(800 * time.Millisecond)
		save(view)
	}
	mygo.App.Exit(0)
}

// selftestDrag finishes a region capture with a known overlay-local rect and
// proves the mapping: the delivered image must match a direct capture of the
// screen rect the overlay says the selection is on. It also checks that the
// editor loaded the image.
func (a *App) selftestDrag() {
	if err := a.beginRegion(); err != nil {
		fmt.Printf("SELFTEST drag: begin error %v\n", err)
		mygo.App.Exit(1)
		return
	}
	time.Sleep(900 * time.Millisecond)
	origin := a.overlayRect
	if a.overlay != nil {
		if live := a.overlay.ContentBounds(); live.Width > 0 {
			origin = live
		}
	}
	local := Rect{X: 10, Y: 10, Width: 120, Height: 100}
	a.finishRegion(local)
	time.Sleep(2600 * time.Millisecond)
	expected := capture.Rect{X: float64(origin.X) + local.X, Y: float64(origin.Y) + local.Y, Width: local.Width, Height: local.Height}
	fmt.Printf("SELFTEST drag: overlay origin=%+v expected screen rect=%+v\n", origin, expected)
	delivered, err := newestSessionPNG(a.sessionDir)
	if err != nil {
		fmt.Printf("SELFTEST drag: delivered image error %v\n", err)
	} else {
		dw, dh := imageDimensions(delivered)
		_ = os.WriteFile("/tmp/tupen-selftest-drag-delivered.png", delivered, 0o644)
		fmt.Printf("SELFTEST drag: delivered %dx%d px -> /tmp/tupen-selftest-drag-delivered.png\n", dw, dh)
		direct, derr := capture.CaptureRegion(expected, capture.CaptureOptions{})
		if derr != nil {
			fmt.Printf("SELFTEST drag: direct capture error %v\n", derr)
		} else {
			_ = os.WriteFile("/tmp/tupen-selftest-drag-direct.png", direct, 0o644)
			diff, same := imageDiff(delivered, direct)
			dw2, dh2 := imageDimensions(direct)
			fmt.Printf("SELFTEST drag: direct %dx%d px, mean abs diff=%.2f same=%v\n", dw2, dh2, diff, same)
		}
	}
	if a.main != nil {
		if v, err := a.main.Page().Eval("document.getElementById('shotCanvas').width + 'x' + document.getElementById('shotCanvas').height"); err == nil {
			fmt.Printf("SELFTEST drag: editor canvas=%v\n", v)
		}
	}
	mygo.App.Exit(0)
}

// selftestEscape shows the overlay and has System Events press Escape, then
// checks that the overlay hid and the main window came back.
func (a *App) selftestEscape() {
	if err := a.beginRegion(); err != nil {
		fmt.Printf("SELFTEST esc: begin error %v\n", err)
		mygo.App.Exit(1)
		return
	}
	time.Sleep(900 * time.Millisecond)
	// System Events occasionally misses a synthesized key while the system
	// is busy; press Escape a few times before calling it a failure.
	for attempt := 1; attempt <= 3; attempt++ {
		cmd := exec.Command("/usr/bin/osascript", "-e", "tell application \"System Events\" to key code 53")
		out, err := cmd.CombinedOutput()
		fmt.Printf("SELFTEST esc: attempt=%d osascript err=%v out=%s\n", attempt, err, strings.TrimSpace(string(out)))
		time.Sleep(900 * time.Millisecond)
		if a.overlay == nil || !a.overlay.IsVisible() {
			break
		}
	}
	if a.overlay != nil {
		fmt.Printf("SELFTEST esc: overlay visible=%v\n", a.overlay.IsVisible())
	}
	if a.main != nil {
		fmt.Printf("SELFTEST esc: main visible=%v\n", a.main.IsVisible())
	}
	mygo.App.Exit(0)
}

// newestSessionPNG returns the most recent capture written for the current
// session.
func newestSessionPNG(dir string) ([]byte, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	var newest string
	var newestTime time.Time
	for _, entry := range entries {
		name := entry.Name()
		if entry.IsDir() || !strings.HasSuffix(name, ".png") || strings.HasPrefix(name, "pin-") {
			continue
		}
		info, err := entry.Info()
		if err != nil {
			continue
		}
		if info.ModTime().After(newestTime) {
			newest = name
			newestTime = info.ModTime()
		}
	}
	if newest == "" {
		return nil, errors.New("no session capture found")
	}
	return os.ReadFile(filepath.Join(dir, newest))
}

// imageDiff decodes two PNGs of the same size and returns the mean absolute
// per-pixel difference (0..255) plus whether they are close enough to call
// the mapping correct.
func imageDiff(aBytes, bBytes []byte) (float64, bool) {
	aImg, err := png.Decode(bytes.NewReader(aBytes))
	if err != nil {
		return 255, false
	}
	bImg, err := png.Decode(bytes.NewReader(bBytes))
	if err != nil {
		return 255, false
	}
	if aImg.Bounds() != bImg.Bounds() {
		return 255, false
	}
	b := aImg.Bounds()
	total := 0.0
	var count float64
	for y := b.Min.Y; y < b.Max.Y; y += 2 {
		for x := b.Min.X; x < b.Max.X; x += 2 {
			ar, ag, ab, _ := aImg.At(x, y).RGBA()
			br, bg, bb, _ := bImg.At(x, y).RGBA()
			diff := math.Abs(float64(ar>>8)-float64(br>>8)) + math.Abs(float64(ag>>8)-float64(bg>>8)) + math.Abs(float64(ab>>8)-float64(bb>>8))
			total += diff / 3
			count++
		}
	}
	mean := total / count
	return mean, mean < 12
}

// syntheticScreenshot draws a wireframe mock of a design review page, for
// screenshots that must not contain the user's real desktop.
func syntheticScreenshot() []byte {
	const width, height = 1280, 800
	img := image.NewNRGBA(image.Rect(0, 0, width, height))
	fill := func(x0, y0, x1, y1 int, c color.NRGBA) {
		draw.Draw(img, image.Rect(x0, y0, x1, y1), &image.Uniform{C: c}, image.Point{}, draw.Src)
	}
	paper := color.NRGBA{R: 242, G: 238, B: 228, A: 255}
	ink := color.NRGBA{R: 180, G: 176, B: 160, A: 255}
	amber := color.NRGBA{R: 243, G: 162, B: 78, A: 255}
	fill(0, 0, width, height, paper)
	fill(0, 0, width, 44, color.NRGBA{R: 228, G: 224, B: 214, A: 255})
	for i, c := range []color.NRGBA{
		{R: 211, G: 125, B: 101, A: 255},
		{R: 210, G: 174, B: 100, A: 255},
		{R: 144, G: 171, B: 134, A: 255},
	} {
		draw.Draw(img, image.Rect(18+i*16, 16, 26+i*16, 24), &image.Uniform{C: c}, image.Point{}, draw.Src)
	}
	fill(90, 16, 320, 24, ink)
	fill(0, 44, 220, height, color.NRGBA{R: 232, G: 227, B: 215, A: 255})
	fill(28, 84, 150, 96, color.NRGBA{R: 52, G: 59, B: 50, A: 255})
	for i := 0; i < 4; i++ {
		fill(28, 140+i*44, 190, 162+i*44, ink)
	}
	fill(24, 300, 196, 336, color.NRGBA{R: 216, G: 215, B: 196, A: 255})
	fill(260, 80, 700, 96, color.NRGBA{R: 140, G: 136, B: 124, A: 255})
	fill(260, 120, 900, 168, color.NRGBA{R: 48, G: 58, B: 46, A: 255})
	fill(260, 186, 1180, 190, ink)
	for i := 0; i < 3; i++ {
		fill(260, 214+i*30, 1080-i*90, 222+i*30, ink)
	}
	fill(260, 320, 740, 580, color.NRGBA{R: 221, G: 220, B: 201, A: 255})
	fill(770, 320, 1180, 580, color.NRGBA{R: 209, G: 215, B: 202, A: 255})
	fill(260, 610, 1180, 760, color.NRGBA{R: 225, G: 211, B: 189, A: 255})
	fill(300, 380, 430, 410, amber)
	for i := 0; i < 5; i++ {
		fill(300, 440+i*24, 700-i*40, 446+i*24, ink)
	}
	fill(800, 380, 1150, 400, ink)
	fill(800, 424, 1100, 430, ink)
	fill(800, 452, 1120, 458, ink)
	for i := 0; i < 3; i++ {
		fill(300+i*310, 650, 560+i*310, 720, []color.NRGBA{
			{R: 243, G: 162, B: 78, A: 255},
			{R: 120, G: 165, B: 118, A: 255},
			{R: 112, G: 168, B: 194, A: 255},
		}[i])
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		return nil
	}
	return buf.Bytes()
}

// selftestDemo delivers a generated screenshot, so UI screenshots for the
// repository show no content from the user's desktop.
func (a *App) selftestDemo() {
	pngBytes := syntheticScreenshot()
	if len(pngBytes) == 0 {
		fmt.Printf("SELFTEST demo: could not render the synthetic screenshot\n")
		mygo.App.Exit(1)
		return
	}
	if a.main != nil {
		a.main.Hide()
	}
	a.deliverPNG(pngBytes, "demo")
	time.Sleep(2500 * time.Millisecond)
	if a.main != nil {
		if pagePNG, err := a.main.CapturePage(); err == nil {
			_ = os.WriteFile("/tmp/tupen-selftest-demo-editor.png", pagePNG, 0o644)
			fmt.Printf("SELFTEST demo: editor page capture %d bytes -> /tmp/tupen-selftest-demo-editor.png\n", len(pagePNG))
		} else {
			fmt.Printf("SELFTEST demo: editor page capture error %v\n", err)
		}
	}
	mygo.App.Exit(0)
}

// dragOverlaySelection drives a pointer drag on the overlay page, so the
// selftests exercise the real selection UI instead of calling the binding.
func (a *App) dragOverlaySelection(x1, y1, x2, y2 float64) error {
	a.mu.Lock()
	overlay := a.overlay
	a.mu.Unlock()
	if overlay == nil {
		return errors.New("overlay window is not available")
	}
	js := fmt.Sprintf(`(() => {
		const dispatch = (type, x, y) => {
			const target = document.getElementById('overlay') || document.body;
			target.dispatchEvent(new PointerEvent(type, {
				clientX: x, clientY: y, bubbles: true, cancelable: true,
				pointerId: 1, pointerType: 'mouse', button: 0,
				buttons: type === 'pointerup' ? 0 : 1,
			}));
		};
		dispatch('pointerdown', %g, %g);
		dispatch('pointermove', %g, %g);
		dispatch('pointerup', %g, %g);
		return 'dragged';
	})()`, x1, y1, x2, y2, x2, y2)
	_, err := overlay.Page().Eval(js)
	return err
}

// clickOverlayAction clicks one button of the overlay's action bar.
func (a *App) clickOverlayAction(action string) error {
	a.mu.Lock()
	overlay := a.overlay
	a.mu.Unlock()
	if overlay == nil {
		return errors.New("overlay window is not available")
	}
	js := fmt.Sprintf(`(() => {
		const button = document.querySelector('[data-action="%s"]');
		if (!button) return 'missing';
		button.click();
		return 'clicked';
	})()`, action)
	value, err := overlay.Page().Eval(js)
	if err != nil {
		return err
	}
	if value != "clicked" {
		return fmt.Errorf("overlay action %q not found (%v)", action, value)
	}
	return nil
}

// selftestFlow exercises the round-2 flow end to end: drag a selection on
// the overlay, then run each action of its toolbar.
func (a *App) selftestFlow() {
	if a.main != nil {
		a.main.Show()
	}
	time.Sleep(1200 * time.Millisecond)
	run := func(name, action string) {
		if err := a.beginRegion(); err != nil {
			fmt.Printf("SELFTEST flow: %s begin error %v\n", name, err)
			return
		}
		time.Sleep(900 * time.Millisecond)
		if err := a.dragOverlaySelection(80, 90, 320, 250); err != nil {
			fmt.Printf("SELFTEST flow: %s drag error %v\n", name, err)
			return
		}
		time.Sleep(500 * time.Millisecond)
		if err := a.clickOverlayAction(action); err != nil {
			fmt.Printf("SELFTEST flow: %s click error %v\n", name, err)
			return
		}
		time.Sleep(1800 * time.Millisecond)
		fmt.Printf("SELFTEST flow: %s clicked %s\n", name, action)
	}
	// copy
	run("copy", "copy")
	if img := mygo.Clipboard.ReadImage(); len(img) > 0 {
		w, h := imageDimensions(img)
		fmt.Printf("SELFTEST flow: copy clipboard %dx%d bytes=%d\n", w, h, len(img))
	} else {
		fmt.Printf("SELFTEST flow: copy clipboard EMPTY\n")
	}
	// pin
	run("pin", "pin")
	a.mu.Lock()
	pins := len(a.pins)
	a.mu.Unlock()
	fmt.Printf("SELFTEST flow: pin pins=%d\n", pins)
	// annotate
	run("annotate", "annotate")
	if a.main != nil {
		if v, err := a.main.Page().Eval("document.getElementById('shotCanvas').width + 'x' + document.getElementById('shotCanvas').height"); err == nil {
			fmt.Printf("SELFTEST flow: annotate editor canvas=%v\n", v)
		}
	}
	// afterCapture=clipboard must run the action without clicking.
	mygo.Clipboard.Clear()
	auto := DefaultSettings()
	auto.AfterCapture = "clipboard"
	a.applySettings(auto)
	if err := a.beginRegion(); err == nil {
		time.Sleep(900 * time.Millisecond)
		if err := a.dragOverlaySelection(60, 60, 260, 200); err != nil {
			fmt.Printf("SELFTEST flow: auto drag error %v\n", err)
		}
		time.Sleep(2600 * time.Millisecond)
		if img := mygo.Clipboard.ReadImage(); len(img) > 0 {
			w, h := imageDimensions(img)
			fmt.Printf("SELFTEST flow: auto clipboard %dx%d\n", w, h)
		} else {
			fmt.Printf("SELFTEST flow: auto clipboard EMPTY\n")
		}
		a.cancelRegion()
	}
	a.applySettings(DefaultSettings())
	time.Sleep(300 * time.Millisecond)
	// cancel: no capture should be delivered
	before := len(a.historyEntries())
	run("cancel", "cancel")
	time.Sleep(600 * time.Millisecond)
	after := len(a.historyEntries())
	fmt.Printf("SELFTEST flow: cancel history before=%d after=%d\n", before, after)
	for id := range a.pins {
		_ = a.closePin(id)
	}
	mygo.App.Exit(0)
}

// selftestPinUI drives the real pin page: its context menu, wheel scaling and
// close button, through the same bindings a user's clicks reach.
func (a *App) selftestPinUI() {
	time.Sleep(1200 * time.Millisecond)
	pngBytes := syntheticScreenshot()
	if len(pngBytes) == 0 {
		fmt.Printf("SELFTEST pinui: no image\n")
		mygo.App.Exit(1)
		return
	}
	mygo.Clipboard.Clear()
	id, err := a.pinPNG(pngBytes, 1280, 800)
	if err != nil {
		fmt.Printf("SELFTEST pinui: pin error %v\n", err)
		mygo.App.Exit(1)
		return
	}
	time.Sleep(1000 * time.Millisecond)
	pageOf := func() *mygo.Page {
		a.mu.Lock()
		win := a.pins[id]
		a.mu.Unlock()
		if win == nil {
			return nil
		}
		return win.Page()
	}
	eval := func(js string) any {
		page := pageOf()
		if page == nil {
			return nil
		}
		v, err := page.Eval(js)
		if err != nil {
			fmt.Printf("SELFTEST pinui: eval %q error %v\n", js, err)
			return nil
		}
		return v
	}
	var before mygo.Rectangle
	a.mu.Lock()
	if win := a.pins[id]; win != nil {
		before = win.Bounds()
	}
	a.mu.Unlock()
	// double-click copies and must not move/resize the window
	fmt.Printf("SELFTEST pinui: dblclick=%v\n", eval(`(() => { const img = document.getElementById('pinImage'); if (!img) return 'missing'; img.dispatchEvent(new MouseEvent('dblclick', { bubbles: true, cancelable: true })); return 'sent'; })()`))
	time.Sleep(900 * time.Millisecond)
	if img := mygo.Clipboard.ReadImage(); len(img) > 0 {
		w, h := imageDimensions(img)
		fmt.Printf("SELFTEST pinui: dblclick clipboard %dx%d\n", w, h)
	} else {
		fmt.Printf("SELFTEST pinui: dblclick clipboard EMPTY\n")
	}
	a.mu.Lock()
	after := mygo.Rectangle{}
	if win := a.pins[id]; win != nil {
		after = win.Bounds()
	}
	a.mu.Unlock()
	fmt.Printf("SELFTEST pinui: bounds unchanged=%v before=%+v after=%+v\n", before == after, before, after)
	// context menu: copy and opacity
	fmt.Printf("SELFTEST pinui: menu copy=%v\n", eval(`(() => { const b = document.querySelector('[data-menu-action="copy"]'); if (!b) return 'missing'; b.click(); return 'clicked'; })()`))
	time.Sleep(700 * time.Millisecond)
	a.mu.Lock()
	winAfterCopy := a.pins[id]
	mapAfterCopy := winAfterCopy != nil
	pinsAfterCopy := len(a.pins)
	a.mu.Unlock()
	destroyed := winAfterCopy == nil || winAfterCopy.IsDestroyed()
	visible := winAfterCopy != nil && winAfterCopy.IsVisible()
	storedAfterCopy := 0
	if a.pinStore != nil {
		storedAfterCopy = len(a.pinStore.List())
	}
	fmt.Printf("SELFTEST pinui: after copy map=%v pins=%d destroyed=%v visible=%v stored=%d\n", mapAfterCopy, pinsAfterCopy, destroyed, visible, storedAfterCopy)
	opacityBefore := 100.0
	a.mu.Lock()
	if win := a.pins[id]; win != nil {
		opacityBefore = win.Opacity() * 100
	}
	a.mu.Unlock()
	fmt.Printf("SELFTEST pinui: menu opacity=%v\n", eval(`(() => { const b = document.querySelector('[data-menu-action="opacity-down"]'); if (!b) return 'missing'; b.click(); return 'clicked'; })()`))
	time.Sleep(700 * time.Millisecond)
	a.mu.Lock()
	opacityAfter := 0.0
	if win := a.pins[id]; win != nil {
		opacityAfter = win.Opacity() * 100
	}
	a.mu.Unlock()
	fmt.Printf("SELFTEST pinui: opacity before=%.0f after=%.0f\n", opacityBefore, opacityAfter)
	// wheel scales the pin window
	fmt.Printf("SELFTEST pinui: wheel=%v\n", eval(`(() => { window.dispatchEvent(new WheelEvent('wheel', { deltaY: -120, bubbles: true, cancelable: true })); return 'sent'; })()`))
	time.Sleep(800 * time.Millisecond)
	a.mu.Lock()
	afterWheel := mygo.Rectangle{}
	if win := a.pins[id]; win != nil {
		afterWheel = win.Bounds()
	}
	a.mu.Unlock()
	fmt.Printf("SELFTEST pinui: wheel bounds=%+v scaled=%v\n", afterWheel, afterWheel.Width > after.Width)
	// close through the page's close button: state must disappear
	fmt.Printf("SELFTEST pinui: close=%v\n", eval(`(() => { const b = document.getElementById('pinClose'); if (!b) return 'missing'; b.click(); return 'clicked'; })()`))
	time.Sleep(900 * time.Millisecond)
	a.mu.Lock()
	remaining := len(a.pins)
	a.mu.Unlock()
	stored := 0
	if a.pinStore != nil {
		stored = len(a.pinStore.List())
	}
	fmt.Printf("SELFTEST pinui: after close pins=%d stored=%d\n", remaining, stored)
	mygo.App.Exit(0)
}

// selftestPinPersistSetup creates one desktop pin and quits through the real
// quit path, so the next launch must restore it.
func (a *App) selftestPinPersistSetup() {
	time.Sleep(1200 * time.Millisecond)
	pngBytes := syntheticScreenshot()
	if len(pngBytes) == 0 {
		fmt.Printf("SELFTEST pinpersist: setup could not render image\n")
		mygo.App.Exit(1)
		return
	}
	id, err := a.pinPNG(pngBytes, 640, 400)
	if err != nil {
		fmt.Printf("SELFTEST pinpersist: setup pin error %v\n", err)
		mygo.App.Exit(1)
		return
	}
	time.Sleep(900 * time.Millisecond)
	a.mu.Lock()
	win := a.pins[id]
	st := a.pinStates[id]
	a.mu.Unlock()
	if win != nil {
		b := win.Bounds()
		fmt.Printf("SELFTEST pinpersist: setup created id=%s bounds=%+v stored=%+v\n", id, b, st)
	} else {
		fmt.Printf("SELFTEST pinpersist: setup created id=%s but window is missing\n", id)
	}
	fmt.Printf("SELFTEST pinpersist: setup quitting\n")
	mygo.App.Quit()
	time.Sleep(2 * time.Second)
}

// selftestPinPersistCheck reports the pins restored from the previous run.
func (a *App) selftestPinPersistCheck() {
	time.Sleep(1500 * time.Millisecond)
	a.mu.Lock()
	count := len(a.pins)
	ids := make([]string, 0, count)
	for id := range a.pins {
		ids = append(ids, id)
	}
	a.mu.Unlock()
	stored := 0
	if a.pinStore != nil {
		stored = len(a.pinStore.List())
	}
	fmt.Printf("SELFTEST pinpersist: check restored=%d stored=%d ids=%v\n", count, stored, ids)
	for _, id := range ids {
		if err := a.closePin(id); err != nil {
			fmt.Printf("SELFTEST pinpersist: close %s error %v\n", id, err)
		}
	}
	time.Sleep(400 * time.Millisecond)
	if a.pinStore != nil {
		fmt.Printf("SELFTEST pinpersist: check after close stored=%d\n", len(a.pinStore.List()))
	}
	mygo.App.Exit(0)
}

// selftestShowcase produces the UI screenshots used by the README. Every
// image it shows is generated, so no content from the user's desktop lands
// in the repository.
func (a *App) selftestShowcase() {
	if strings.TrimSpace(os.Getenv("TUPEN_DATA_DIR")) == "" {
		fmt.Printf("SELFTEST showcase: TUPEN_DATA_DIR must point at an isolated data directory\n")
		mygo.App.Exit(1)
		return
	}
	pngBytes := syntheticScreenshot()
	if len(pngBytes) == 0 {
		fmt.Printf("SELFTEST showcase: no synthetic image\n")
		mygo.App.Exit(1)
		return
	}
	if a.main != nil {
		a.main.Hide()
	}
	a.deliverPNG(pngBytes, "demo")
	time.Sleep(2200 * time.Millisecond)
	savePage := func(win *mygo.Window, name string) {
		if win == nil {
			return
		}
		pagePNG, err := win.CapturePage()
		if err != nil {
			fmt.Printf("SELFTEST showcase: %s capture error %v\n", name, err)
			return
		}
		path := "/tmp/tupen-showcase-" + name + ".png"
		_ = os.WriteFile(path, pagePNG, 0o644)
		w, h := imageDimensions(pagePNG)
		fmt.Printf("SELFTEST showcase: %s %dx%d -> %s\n", name, w, h, path)
	}
	savePage(a.main, "editor")
	for _, view := range []string{"history", "settings"} {
		if a.main != nil {
			_, _ = a.main.Page().Eval(`document.querySelector('[data-view="` + view + `"]').click()`)
			time.Sleep(700 * time.Millisecond)
			savePage(a.main, view)
		}
	}
	if a.main != nil {
		_, _ = a.main.Page().Eval(`document.querySelector('[data-view="rack"]').click()`)
	}
	// Overlay with a live selection and its action bar, captured from the
	// overlay's own page so the desktop behind it stays out of the image.
	if err := a.beginRegion(); err == nil {
		time.Sleep(800 * time.Millisecond)
		if err := a.dragOverlaySelection(120, 140, 620, 420); err != nil {
			fmt.Printf("SELFTEST showcase: overlay drag error %v\n", err)
		}
		time.Sleep(600 * time.Millisecond)
		savePage(a.overlay, "overlay")
		a.cancelRegion()
		time.Sleep(600 * time.Millisecond)
	}
	// A desktop pin showing the generated image.
	if id, err := a.pinPNG(pngBytes, 1280, 800); err == nil {
		time.Sleep(900 * time.Millisecond)
		a.mu.Lock()
		win := a.pins[id]
		a.mu.Unlock()
		savePage(win, "pin")
		_ = a.closePin(id)
	} else {
		fmt.Printf("SELFTEST showcase: pin error %v\n", err)
	}
	time.Sleep(400 * time.Millisecond)
	mygo.App.Exit(0)
}

// selftestQuit exercises the real quit sequence (Cmd+Q, menu Quit, tray
// Quit) rather than the force exit the other selftests use. A window that
// refuses to close aborts the quit, so a blocked quit reports itself.
func (a *App) selftestQuit() {
	if a.main != nil {
		a.main.Show()
	}
	time.Sleep(1500 * time.Millisecond)
	fmt.Printf("SELFTEST quit: calling App.Quit\n")
	done := make(chan struct{})
	go func() {
		mygo.App.Quit()
		close(done)
	}()
	select {
	case <-done:
		fmt.Printf("SELFTEST quit: quit sequence finished\n")
	case <-time.After(5 * time.Second):
		fmt.Printf("SELFTEST quit: BLOCKED, a window refused to close\n")
		os.Exit(2)
	}
	time.Sleep(500 * time.Millisecond)
}

// selftestHold keeps the app running so its native shell can be inspected
// from outside (menu bar icon, status item menus).
func (a *App) selftestHold() {
	fmt.Printf("SELFTEST hold: READY pid=%d\n", os.Getpid())
	time.Sleep(25 * time.Second)
	mygo.App.Exit(0)
}

// selftestUIActions drives the real page: capture, then click the toolbar's
// clipboard and pin tools and the history view like a user would.
func (a *App) selftestUIActions() {
	disp := mygo.Screen.PrimaryDisplay()
	b := disp.Bounds
	region := capture.Rect{X: float64(b.X) + 160, Y: float64(b.Y) + 160, Width: 560, Height: 360}
	if a.main != nil {
		a.main.Hide()
	}
	go a.captureDeliver(region, "region")
	time.Sleep(3200 * time.Millisecond)
	eval := func(js string) any {
		if a.main == nil {
			return nil
		}
		v, err := a.main.Page().Eval(js)
		if err != nil {
			fmt.Printf("SELFTEST uiactions: eval %q error %v\n", js, err)
			return nil
		}
		return v
	}
	fmt.Printf("SELFTEST uiactions: canvas=%v\n", eval("document.getElementById('shotCanvas').width + 'x' + document.getElementById('shotCanvas').height"))
	fmt.Printf("SELFTEST uiactions: rack pins=%v history cards=%v\n",
		eval("document.querySelectorAll('#rack .pin').length"),
		eval("document.querySelectorAll('#historyGrid .history-card').length"))

	// Clipboard tool: the page turns the canvas into a PNG and Go writes it.
	fmt.Printf("SELFTEST uiactions: copy click=%v\n", eval("document.querySelector('[data-tool=\"clipboard\"]').click(), 'clicked'"))
	time.Sleep(1200 * time.Millisecond)
	if img := mygo.Clipboard.ReadImage(); len(img) > 0 {
		width, height := imageDimensions(img)
		fmt.Printf("SELFTEST uiactions: clipboard png %dx%d bytes=%d\n", width, height, len(img))
	} else {
		fmt.Printf("SELFTEST uiactions: clipboard EMPTY\n")
	}

	// Pin tool: the page sends the canvas PNG and Go opens a pin window.
	fmt.Printf("SELFTEST uiactions: pin click=%v\n", eval("document.querySelector('[data-tool=\"pin\"]').click(), 'clicked'"))
	time.Sleep(1200 * time.Millisecond)
	a.mu.Lock()
	pinCount := len(a.pins)
	a.mu.Unlock()
	fmt.Printf("SELFTEST uiactions: pins=%d\n", pinCount)
	snap, err := capture.CaptureRegion(virtualDesktopRect(), capture.CaptureOptions{})
	if err == nil {
		_ = os.WriteFile("/tmp/tupen-selftest-uiactions.png", snap, 0o644)
		fmt.Printf("SELFTEST uiactions: screenshot /tmp/tupen-selftest-uiactions.png\n")
	}

	// History view: switch, then load the first card into the editor.
	fmt.Printf("SELFTEST uiactions: history view=%v\n", eval("document.querySelector('[data-view=\"history\"]').click(), 'clicked'"))
	time.Sleep(900 * time.Millisecond)
	fmt.Printf("SELFTEST uiactions: history cards=%v\n", eval("document.querySelectorAll('#historyGrid .history-card').length"))
	fmt.Printf("SELFTEST uiactions: open first=%v\n", eval("document.querySelector('#historyGrid .history-card button, #historyGrid .history-card')?.click(), 'clicked'"))
	time.Sleep(1600 * time.Millisecond)
	fmt.Printf("SELFTEST uiactions: canvas after history=%v\n", eval("document.getElementById('shotCanvas').width + 'x' + document.getElementById('shotCanvas').height"))
	for id := range a.pins {
		_ = a.closePin(id)
	}
	mygo.App.Exit(0)
}

// selftestHotkeyPress presses the registered capture shortcut through
// System Events and reports whether the overlay opened. Note: macOS may not
// deliver synthesized keyboard events to Carbon hot keys, so a false result
// here is not proof that a real key press fails; the mode also prints the
// registration state, which is the authoritative check.
func (a *App) selftestHotkeyPress() {
	captureKey := a.settingsSnapshot().CaptureKey
	key := "1"
	if idx := strings.LastIndex(captureKey, "+"); idx >= 0 && idx+1 < len(captureKey) {
		key = captureKey[idx+1:]
	}
	if a.main != nil {
		a.main.Hide()
	}
	time.Sleep(300 * time.Millisecond)
	fmt.Printf("SELFTEST hotkeypress: captureKey=%s key=%s registered=%v\n",
		captureKey, key, mygo.GlobalShortcut.IsRegistered(captureKey))
	cmd := exec.Command("/usr/bin/osascript", "-e",
		"tell application \"System Events\" to keystroke \""+strings.ToLower(key)+"\" using {command down, shift down}")
	out, err := cmd.CombinedOutput()
	fmt.Printf("SELFTEST hotkeypress: synthetic press err=%v out=%s\n", err, strings.TrimSpace(string(out)))
	time.Sleep(1500 * time.Millisecond)
	visible := a.overlay != nil && a.overlay.IsVisible()
	fmt.Printf("SELFTEST hotkeypress: overlay visible after synthetic press=%v\n", visible)
	if visible {
		a.cancelRegion()
		time.Sleep(400 * time.Millisecond)
	}
	mygo.App.Exit(0)
}

// virtualDesktopRect covers every display, so a selftest screenshot shows
// windows on any of them.
func virtualDesktopRect() capture.Rect {
	displays := mygo.Screen.Displays()
	if len(displays) == 0 {
		return capture.Rect{}
	}
	minX, minY := displays[0].Bounds.X, displays[0].Bounds.Y
	maxX := minX + displays[0].Bounds.Width
	maxY := minY + displays[0].Bounds.Height
	for _, d := range displays[1:] {
		minX = min(minX, d.Bounds.X)
		minY = min(minY, d.Bounds.Y)
		maxX = max(maxX, d.Bounds.X+d.Bounds.Width)
		maxY = max(maxY, d.Bounds.Y+d.Bounds.Height)
	}
	return capture.Rect{X: float64(minX), Y: float64(minY), Width: float64(maxX - minX), Height: float64(maxY - minY)}
}

func (a *App) selftestHotkeys() {
	// The page applies settings as it loads; apply them here too so the
	// report describes the settled state rather than a mid-load moment.
	a.applySettings(a.settingsSnapshot())
	time.Sleep(150 * time.Millisecond)
	for _, key := range []string{"captureKey", "pinKey", "ocrKey"} {
		a.mu.Lock()
		acc := a.shortcutKeys[key]
		a.mu.Unlock()
		fmt.Printf("SELFTEST hotkeys: %s %q registered=%v\n", key, acc, mygo.GlobalShortcut.IsRegistered(acc))
	}
	mygo.App.Exit(0)
}

func (a *App) selftestSettings() {
	errs := a.applySettings(DefaultSettings())
	fmt.Printf("SELFTEST settings: apply errors=%v\n", errs)
	a.mu.Lock()
	keys := a.shortcutKeys
	a.mu.Unlock()
	for key, acc := range keys {
		fmt.Printf("SELFTEST settings: %s=%s\n", key, acc)
	}
	mygo.App.Exit(0)
}
