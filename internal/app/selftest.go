package app

import (
	"bytes"
	"encoding/base64"
	"errors"
	"fmt"
	"image"
	"image/color"
	"image/draw"
	"image/png"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/echoVic/Pinstill/internal/capture"
	"github.com/egoist/mygo"
)

// ValidateSelftestDataDir must be called before migration or storage
// initialization. A nonempty override alone is not proof of isolation.
func ValidateSelftestDataDir(dir string) error {
	if strings.TrimSpace(dir) == "" || !filepath.IsAbs(dir) {
		return errors.New("selftest data directory must be an absolute isolated path")
	}
	resolved, err := filepath.EvalSymlinks(dir)
	if err != nil {
		return fmt.Errorf("selftest data directory: %w", err)
	}
	info, err := os.Stat(resolved)
	if err != nil || !info.IsDir() {
		return errors.New("selftest data path is not a directory")
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return err
	}
	if canonical, err := filepath.EvalSymlinks(home); err == nil {
		home = canonical
	}
	lower := strings.ToLower(filepath.ToSlash(resolved))
	if resolved == filepath.VolumeName(resolved)+string(filepath.Separator) || strings.EqualFold(resolved, home) {
		return errors.New("refusing root or home as selftest data directory")
	}
	for _, protected := range []string{"library/application support/pinstill", "library/application support/tupen", "library/caches/pinstill", "library/caches/tupen", "library/webkit/dev.echovic.pinstill", "library/webkit/dev.echovic.tupen", "library/caches/dev.echovic.pinstill", "library/caches/dev.echovic.tupen", ".config/pinstill", ".cache/pinstill", "appdata/roaming/pinstill", "appdata/local/pinstill"} {
		if strings.Contains(lower+"/", "/"+protected+"/") {
			return errors.New("refusing production app data for selftests")
		}
	}
	marker := filepath.Join(resolved, ".pinstill-selftest")
	info, err = os.Lstat(marker)
	if err != nil || !info.Mode().IsRegular() || info.Size() != int64(len("pinstill-selftest\n")) {
		return errors.New("selftest directory needs a regular .pinstill-selftest marker")
	}
	data, err := os.ReadFile(marker)
	if err != nil || string(data) != "pinstill-selftest\n" {
		return errors.New("invalid .pinstill-selftest marker")
	}
	return nil
}

// Modes abort through one boundary: no failure can fall through to checks=ok.
// Panic/recover is confined to the selftest goroutine, and unexpected panics
// are failures too. Native quit completion uses its OnQuit event separately.
type selftestResult struct{ code int }

func selftestExit(code int) { panic(selftestResult{code: code}) }
func selftestFail(format string, args ...any) {
	fmt.Printf("SELFTEST failure: "+strings.TrimSuffix(format, "\n")+"\n", args...)
	selftestExit(1)
}
func selftestRequire(ok bool, format string, args ...any) {
	if !ok {
		selftestFail(format, args...)
	}
}
func selftestCheckError(err error) {
	if err != nil {
		selftestFail("%v", err)
	}
}
func selftestValue(value any, err error) any {
	selftestCheckError(err)
	return value
}
func selftestSettingsErrors(errs map[string]string) {
	selftestRequire(len(errs) == 0, "settings errors: %v", errs)
}
func selftestRun(mode string, run func(), exit func(int)) {
	defer func() {
		code := 0
		if result := recover(); result != nil {
			if status, ok := result.(selftestResult); ok {
				code = status.code
			} else {
				fmt.Printf("SELFTEST %s: panic: %v\n", mode, result)
				code = 1
			}
		}
		if code == 0 {
			fmt.Printf("SELFTEST %s: checks=ok\n", mode)
		} else {
			fmt.Printf("SELFTEST %s: checks=FAILED\n", mode)
		}
		exit(code)
	}()
	run()
}

func writeSelftestArtifact(name string, data []byte) (string, error) {
	if name == "" || name == "." || name == ".." || strings.ContainsAny(name, "/\\") || filepath.Base(name) != name {
		return "", fmt.Errorf("unsafe selftest artifact basename %q", name)
	}
	dir := os.Getenv("PINSTILL_DATA_DIR")
	if err := ValidateSelftestDataDir(dir); err != nil {
		return "", err
	}
	// Root confines all filesystem operations and refuses symlinks escaping
	// the test directory. Never follow an artifact directory or output symlink.
	root, err := os.OpenRoot(dir)
	if err != nil {
		return "", err
	}
	defer root.Close()
	if err := root.Mkdir("artifacts", 0o700); err != nil && !os.IsExist(err) {
		return "", err
	}
	info, err := root.Lstat("artifacts")
	if err != nil || !info.IsDir() {
		return "", errors.New("selftest artifacts must be a real directory")
	}
	rel := filepath.Join("artifacts", name)
	file, err := root.OpenFile(rel, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return "", err
	}
	_, writeErr := file.Write(data)
	closeErr := file.Close()
	if err := errors.Join(writeErr, closeErr); err != nil {
		return "", err
	}
	return filepath.Join(dir, rel), nil
}
func selftestArtifact(name string, data []byte) string {
	path, err := writeSelftestArtifact(name, data)
	selftestCheckError(err)
	fmt.Printf("SELFTEST artifact: %s\n", path)
	return path
}

// OnQuit fires when the real event loop has finished. Printing before Quit
// returns is insufficient: a window can veto it. The harness also bounds this
// entire mode from process launch, including a stuck main thread.
func selftestRealQuit(mode string) {
	mygo.App.OnQuit(func() { fmt.Printf("SELFTEST %s: checks=ok\n", mode) })
	mygo.App.Quit()
	time.Sleep(10 * time.Second)
	selftestFail("%s quit did not terminate the process", mode)
}

// runSelfTest exercises native pieces without a user driving the app. It is a
// development aid; work/e2e.sh supplies isolated storage and a process deadline.
func (a *App) runSelfTest(mode string) {
	selftestRun(mode, func() {
		selftestCheckError(ValidateSelftestDataDir(os.Getenv("PINSTILL_DATA_DIR")))
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
		case "perms":
			a.selftestPerms()
		case "palette":
			a.selftestPalette()
		case "textstyle":
			a.selftestTextStyle()
		case "dblclick":
			a.selftestDoubleClick()
		case "resize":
			a.selftestResize()
		case "watermark":
			a.selftestWatermark()
		case "resizestress":
			a.selftestResizeStress()
		case "uicapture":
			a.selftestUICapture()
		case "drag":
			a.selftestDrag()
		case "esc":
			a.selftestEscape()
		case "fullscreen":
			a.selftestFullScreen()
		case "historyactions":
			a.selftestHistoryActions()
		case "hotkeypress":
			a.selftestHotkeyPress()
		case "showcase":
			a.selftestShowcase()
		case "scroll":
			a.selftestScroll()
		case "scrollflow":
			a.selftestScrollFlow()
		case "scrollmanual":
			a.selftestScrollManual()
		case "scrollshot":
			a.selftestScrollShot()
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
			selftestFail("unknown PINSTILL_SELFTEST mode %q", mode)
			selftestExit(1)
		}
	}, os.Exit)
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
		selftestFail("SELFTEST capture: ERROR %v\n", err)
	}
	width, height := imageDimensions(pngBytes)
	selftestArtifact("pinstill-selftest-capture.png", pngBytes)
	fmt.Printf("SELFTEST capture: ok %dx%d bytes=%d scale=%.1f artifact=pinstill-selftest-capture.png\n",
		width, height, len(pngBytes), disp.ScaleFactor)
	selftestRequire(a.store != nil, "capture history store missing")
	if a.store != nil {
		entry, err := a.store.Save(pngBytes, time.Now())
		if err != nil {
			selftestFail("SELFTEST capture: history save error %v\n", err)
		} else {
			fmt.Printf("SELFTEST capture: history entry id=%s w=%d h=%d entries=%d\n",
				entry.ID, entry.Width, entry.Height, len(a.store.List()))
		}
	}
	if err := mygo.Clipboard.WriteImage(pngBytes); err != nil {
		selftestFail("SELFTEST capture: clipboard error %v\n", err)
	} else {
		fmt.Printf("SELFTEST capture: clipboard ok\n")
		if os.Getenv("PINSTILL_SELFTEST_HOLD") != "" {
			fmt.Printf("SELFTEST clipboard: READY holding for external pbpaste\n")
			time.Sleep(20 * time.Second)
		}
	}
	selftestExit(0)
}

func (a *App) selftestOverlay() {
	if a.main != nil {
		a.main.Show()
	}
	time.Sleep(300 * time.Millisecond)
	if err := a.beginRegion(); err != nil {
		selftestFail("SELFTEST overlay: begin error %v\n", err)
	}
	time.Sleep(1200 * time.Millisecond)
	selftestRequire(a.overlay != nil, "overlay window missing")
	if a.overlay != nil {
		selftestRequire(a.overlay.IsVisible(), "overlay window is hidden")
		disp0 := mygo.Screen.PrimaryDisplay()
		fmt.Printf("SELFTEST overlay: primary bounds=%+v cursor=%+v\n", disp0.Bounds, mygo.Screen.CursorScreenPoint())
		fmt.Printf("SELFTEST overlay: overlay bounds now=%+v content=%+v\n", a.overlay.Bounds(), a.overlay.ContentBounds())
		a.overlay.SetBounds(a.overlay.Bounds())
		time.Sleep(200 * time.Millisecond)
		fmt.Printf("SELFTEST overlay: after re-set bounds=%+v content=%+v\n", a.overlay.Bounds(), a.overlay.ContentBounds())
		state, err := a.overlay.Page().Eval("document.readyState + '|' + document.body.children.length + '|' + (document.getElementById('dim') ? getComputedStyle(document.getElementById('dim')).backgroundColor : 'no-dim')")
		fmt.Printf("SELFTEST overlay: page state=%v err=%v\n", state, err)
		selftestCheckError(err)
		fmt.Printf("SELFTEST overlay: window visible=%v bounds=%+v displayScale=%.1f\n",
			a.overlay.IsVisible(), a.overlay.Bounds(), a.overlayDisplay.ScaleFactor)
		if pagePNG, err := a.overlay.CapturePage(); err == nil {
			selftestArtifact("pinstill-selftest-overlay-page.png", pagePNG)
			fmt.Printf("SELFTEST overlay: page capture %d bytes -> artifacts/pinstill-selftest-overlay-page.png\n", len(pagePNG))
		} else {
			selftestFail("SELFTEST overlay: page capture error %v\n", err)
		}
	}
	region := virtualDesktopRect()
	pngBytes, err := capture.CaptureRegion(region, capture.CaptureOptions{})
	if err != nil {
		selftestFail("SELFTEST overlay: capture error %v\n", err)
	} else {
		selftestArtifact("pinstill-selftest-overlay.png", pngBytes)
		width, height := imageDimensions(pngBytes)
		fmt.Printf("SELFTEST overlay: ok overlay shown, desktop %dx%d rect=%+v artifact=pinstill-selftest-overlay.png\n", width, height, region)
	}
	a.cancelRegion()
	time.Sleep(300 * time.Millisecond)
	selftestExit(0)
}

func (a *App) selftestHistory() {
	disp := mygo.Screen.PrimaryDisplay()
	b := disp.Bounds
	region := capture.Rect{X: float64(b.X) + 60, Y: float64(b.Y) + 60, Width: 500, Height: 320}
	pngBytes, err := capture.CaptureRegion(region, capture.CaptureOptions{})
	if err != nil {
		selftestFail("SELFTEST history: capture error %v\n", err)
	}
	if a.store == nil {
		selftestFail("SELFTEST history: no store\n")
	}
	entry, err := a.store.Save(pngBytes, time.Now())
	if err != nil {
		selftestFail("SELFTEST history: save error %v\n", err)
	}
	list := a.historyEntries()
	selftestRequire(len(list) == 1, "history save expected exactly one entry, got %d", len(list))
	fmt.Printf("SELFTEST history: saved id=%s entries=%d firstURL=%s thumbURL=%s\n",
		entry.ID, len(list), list[0].URL, list[0].ThumbURL)
	selftestExit(0)
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
		selftestFail("SELFTEST pin: capture error %v\n", err)
	}
	id, err := a.pinPNG(pngBytes, 0, 0)
	if err != nil {
		selftestFail("SELFTEST pin: pin error %v\n", err)
	}
	fmt.Printf("SELFTEST pin: window id=%s\n", id)
	time.Sleep(1000 * time.Millisecond)
	full := capture.Rect{X: float64(b.X), Y: float64(b.Y), Width: float64(b.Width), Height: float64(b.Height)}
	snap, err := capture.CaptureRegion(full, capture.CaptureOptions{})
	if err != nil {
		selftestFail("SELFTEST pin: verify capture error %v\n", err)
	} else {
		selftestArtifact("pinstill-selftest-pin.png", snap)
		fmt.Printf("SELFTEST pin: screenshot artifacts/pinstill-selftest-pin.png\n")
	}
	if err := a.closePin(id); err != nil {
		selftestFail("SELFTEST pin: close error %v\n", err)
	}
	time.Sleep(200 * time.Millisecond)
	selftestExit(0)
}

// selftestUICapture walks the main window's views (history, settings), checks
// the editor is really gone and saves a clean screenshot of each view, for UI
// review without manual clicking. History is seeded with generated images so
// the user's real captures never appear.
func (a *App) selftestUICapture() {
	if a.main == nil {
		selftestFail("SELFTEST uicapture: no main window\n")
	}
	for i := 0; i < 2; i++ {
		if png := syntheticScreenshot(); len(png) > 0 {
			if _, err := a.storeCapture(png); err != nil {
				selftestFail("SELFTEST uicapture: seed error %v\n", err)
			}
		}
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
	selftestRequire(trayOK, "tray missing")
	eval := func(js string) string {
		v, err := a.main.Page().Eval(js)
		if err != nil {
			selftestFail("SELFTEST uicapture: eval error %v\n", err)
			return ""
		}
		return strings.TrimSpace(fmt.Sprint(v))
	}
	save := func(name string) {
		png, err := a.main.CapturePage()
		if err != nil {
			selftestFail("SELFTEST uicapture: %s error %v\n", name, err)
			return
		}
		selftestArtifact("pinstill-ui-"+name+".png", png)
		width, height := imageDimensions(png)
		fmt.Printf("SELFTEST uicapture: %s %dx%d -> artifacts/pinstill-ui-%s.png\n", name, width, height, name)
	}
	views := eval(`[...document.querySelectorAll('button[data-view]')].map((b) => b.dataset.view).join(',')`)
	editorDOM := eval(`String(!!document.getElementById('shotCanvas') || !!document.getElementById('captureToolbar'))`)
	cards := eval(`String(document.querySelectorAll('#historyGrid .history-card').length)`)
	empty := eval(`String(document.getElementById('historyEmpty')?.hidden ?? false)`)
	fmt.Printf("SELFTEST uicapture: views=%s\n", views)
	fmt.Printf("SELFTEST uicapture: editor_dom=%s history_cards=%s history_empty_hidden=%s\n", editorDOM, cards, empty)
	n, err := strconv.Atoi(cards)
	selftestRequire(err == nil && n >= 2 && empty == "true" && editorDOM == "false", "history grid or editor DOM mismatch")
	if !strings.Contains(views, "history") || !strings.Contains(views, "settings") || strings.Contains(views, "rack") {
		selftestFail("SELFTEST uicapture: views_wrong=1\n")
	}
	if editorDOM == "true" {
		selftestFail("SELFTEST uicapture: editor_dom_present=1\n")
	}
	save("history")
	// The history preview sheet: click the first card, check the original
	// image and actions, then close it with Escape.
	eval(`document.querySelector('#historyGrid .history-card')?.click(), 'ok'`)
	time.Sleep(700 * time.Millisecond)
	preview := eval(`String(!!document.getElementById('historyPreview') && !document.getElementById('historyPreview').hidden)`)
	previewImage := eval(`String(!!document.getElementById('historyPreviewImage'))`)
	previewActions := eval(`[...document.querySelectorAll('[data-history-action]')].map((b) => b.dataset.historyAction).join(',')`)
	fmt.Printf("SELFTEST uicapture: preview=%s preview_image=%s preview_actions=%s\n", preview, previewImage, previewActions)
	selftestRequire(preview == "true" && previewImage == "true", "history preview missing")
	for _, action := range []string{"copy", "save", "pin", "delete", "close"} {
		selftestRequire(strings.Contains(","+previewActions+",", ","+action+","), "history preview missing %s", action)
	}
	save("preview")
	eval(`document.body.dispatchEvent(new KeyboardEvent('keydown', { key: 'Escape', bubbles: true, cancelable: true })), 'ok'`)
	time.Sleep(400 * time.Millisecond)
	previewClosed := eval(`String(!!document.getElementById('historyPreview') && !document.getElementById('historyPreview').hidden)`)
	fmt.Printf("SELFTEST uicapture: preview_closed=%s\n", previewClosed)
	selftestRequire(previewClosed == "false", "history preview did not close")
	eval(`document.querySelector('[data-view="settings"]')?.click(), 'ok'`)
	time.Sleep(800 * time.Millisecond)
	save("settings")
	selftestExit(0)
}

// selftestDrag drags a known selection on the overlay and proves the
// mapping: the image the overlay captured in place must match a direct
// capture of the screen rect the selection is on.
func (a *App) selftestDrag() {
	if err := a.beginRegion(); err != nil {
		selftestFail("SELFTEST drag: begin error %v\n", err)
	}
	time.Sleep(1000 * time.Millisecond)
	origin := a.overlayRect
	if a.overlay != nil {
		if live := a.overlay.ContentBounds(); live.Width > 0 {
			origin = live
		}
	}
	// The page's own drag flow hides the selection chrome before asking Go for
	// the capture, so the image it gets is the clean screen behind it.
	if err := a.dragOverlaySelection(10, 10, 130, 110); err != nil {
		selftestFail("SELFTEST drag: drag error %v\n", err)
		a.cancelRegion()
		selftestExit(1)
		return
	}
	if err := a.waitOverlayAnnotating(10 * time.Second); err != nil {
		selftestFail("SELFTEST drag: prepare error %v\n", err)
		a.cancelRegion()
		selftestExit(1)
		return
	}
	time.Sleep(300 * time.Millisecond)
	local := Rect{X: 10, Y: 10, Width: 120, Height: 100}
	expected := capture.Rect{X: float64(origin.X) + local.X, Y: float64(origin.Y) + local.Y, Width: local.Width, Height: local.Height}
	fmt.Printf("SELFTEST drag: overlay origin=%+v expected screen rect=%+v\n", origin, expected)
	delivered, err := newestSessionPNG(a.sessionDir)
	if err != nil {
		selftestFail("SELFTEST drag: delivered image error %v\n", err)
	} else {
		dw, dh := imageDimensions(delivered)
		selftestArtifact("pinstill-selftest-drag-delivered.png", delivered)
		fmt.Printf("SELFTEST drag: delivered %dx%d px -> artifacts/pinstill-selftest-drag-delivered.png\n", dw, dh)
		// Compare with the screen behind the overlay: hide the overlay first so
		// the direct capture sees exactly what the in-place capture saw through
		// the transparent selection.
		if a.overlay != nil {
			a.overlay.Hide()
		}
		time.Sleep(350 * time.Millisecond)
		direct, derr := capture.CaptureRegion(expected, capture.CaptureOptions{})
		if derr != nil {
			selftestFail("SELFTEST drag: direct capture error %v\n", derr)
		} else {
			selftestArtifact("pinstill-selftest-drag-direct.png", direct)
			diff, same := imageDiff(delivered, direct)
			dw2, dh2 := imageDimensions(direct)
			fmt.Printf("SELFTEST drag: direct %dx%d px, mean abs diff=%.2f same=%v\n", dw2, dh2, diff, same)
			selftestRequire(same, "selection mapping image mismatch: %.2f", diff)
		}
	}
	a.cancelRegion()
	time.Sleep(300 * time.Millisecond)
	selftestExit(0)
}

// selftestEscape shows the overlay and has System Events press Escape, then
// checks that the overlay hid and the main window came back.
func (a *App) selftestEscape() {
	if err := a.beginRegion(); err != nil {
		selftestFail("SELFTEST esc: begin error %v\n", err)
	}
	time.Sleep(900 * time.Millisecond)
	// System Events types into the frontmost app, and earlier selftest steps
	// may have left another app in front on a busy machine. Activate Pinstill
	// first so the synthetic Escape lands on the overlay.
	activate := exec.Command("/usr/bin/osascript", "-e",
		`tell application "System Events" to set frontmost of process "Pinstill" to true`)
	selftestCheckError(activate.Run())
	time.Sleep(400 * time.Millisecond)
	// System Events occasionally misses a synthesized key while the system
	// is busy; press Escape a few times before calling it a failure.
	for attempt := 1; attempt <= 3; attempt++ {
		cmd := exec.Command("/usr/bin/osascript", "-e", "tell application \"System Events\" to key code 53")
		out, err := cmd.CombinedOutput()
		fmt.Printf("SELFTEST esc: attempt=%d osascript err=%v out=%s\n", attempt, err, strings.TrimSpace(string(out)))
		selftestCheckError(err)
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
	selftestRequire(!a.overlayVisible() && a.main != nil && a.main.IsVisible(), "Escape did not restore main and hide overlay")
	// Second capture with the history/settings window closed: cancelling must
	// leave it closed (it used to be reopened by cancel/failure paths).
	if a.main != nil {
		a.main.Hide()
	}
	time.Sleep(400 * time.Millisecond)
	if err := a.beginRegion(); err != nil {
		selftestFail("SELFTEST esc: second begin error %v\n", err)
	}
	time.Sleep(900 * time.Millisecond)
	a.cancelRegion()
	time.Sleep(600 * time.Millisecond)
	mainAfterHiddenCancel := a.main != nil && a.main.IsVisible()
	fmt.Printf("SELFTEST esc: main_after_hidden_cancel=%v\n", mainAfterHiddenCancel)
	if mainAfterHiddenCancel {
		selftestFail("SELFTEST esc: checks=FAILED\n")
	}
	selftestExit(0)
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

// syntheticScrollResultPNG tiles the synthetic screenshot into a tall image so
// the README preview shot only ever shows generated content.
func syntheticScrollResultPNG() []byte {
	frame := syntheticScreenshot()
	if len(frame) == 0 {
		return nil
	}
	img, err := png.Decode(bytes.NewReader(frame))
	if err != nil {
		return nil
	}
	b := img.Bounds()
	const copies = 3
	tall := image.NewRGBA(image.Rect(0, 0, b.Dx(), b.Dy()*copies))
	for i := 0; i < copies; i++ {
		draw.Draw(tall, image.Rect(0, i*b.Dy(), b.Dx(), (i+1)*b.Dy()), img, b.Min, draw.Src)
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, tall); err != nil {
		return nil
	}
	return buf.Bytes()
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

// drawOverlayAnnotation draws a red rectangle on the in-place annotation
// canvas, so the selftests prove annotations are composited into the result.
func (a *App) drawOverlayAnnotation() error {
	a.mu.Lock()
	overlay := a.overlay
	a.mu.Unlock()
	if overlay == nil {
		return errors.New("overlay window is not available")
	}
	js := `(() => {
		const canvas = document.getElementById('overlayCanvas');
		if (!canvas) return 'no-canvas';
		const button = document.querySelector('[data-tool="rect"]');
		if (button) button.click();
		const r = canvas.getBoundingClientRect();
		const fire = (type, x, y) => {
			canvas.dispatchEvent(new PointerEvent(type, {
				clientX: r.left + x, clientY: r.top + y, bubbles: true, cancelable: true,
				pointerId: 3, pointerType: 'mouse', button: 0,
				buttons: type === 'pointerup' ? 0 : 1,
			}));
		};
		fire('pointerdown', 18, 16);
		fire('pointermove', 140, 110);
		fire('pointerup', 140, 110);
		return 'drawn';
	})()`
	value, err := overlay.Page().Eval(js)
	if err != nil {
		return err
	}
	if value != "drawn" {
		return fmt.Errorf("annotation canvas not ready (%v)", value)
	}
	return nil
}

// overlayEval evaluates an expression in the overlay page, for selftest
// diagnostics.
func (a *App) overlayEval(js string) any {
	a.mu.Lock()
	overlay := a.overlay
	a.mu.Unlock()
	selftestRequire(overlay != nil, "overlay window missing")
	v, err := overlay.Page().Eval(js)
	if err != nil {
		selftestFail("eval: %v", err)
		return nil
	}
	return v
}

// overlayVisible reports whether the overlay window currently shows.
func (a *App) overlayVisible() bool {
	a.mu.Lock()
	overlay := a.overlay
	a.mu.Unlock()
	return overlay != nil && overlay.IsVisible()
}

// waitOverlayActionReady waits until the overlay's toolbar button can be
// clicked (the page disables the actions while it prepares the capture).
func (a *App) waitOverlayActionReady(action string, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		a.mu.Lock()
		overlay := a.overlay
		a.mu.Unlock()
		if overlay != nil {
			js := fmt.Sprintf(`(() => {
				const toolbar = document.getElementById('annotationToolbar');
				const b = document.querySelector('[data-action="%s"]');
				return !!toolbar && !toolbar.hidden && !!b && !b.disabled && b.offsetParent !== null;
			})()`, action)
			if v, err := overlay.Page().Eval(js); err == nil && v == true {
				return nil
			}
		}
		time.Sleep(120 * time.Millisecond)
	}
	return fmt.Errorf("overlay action %q never became ready", action)
}

// waitOverlayAnnotating waits until the overlay page reports its annotation
// canvas, which means PrepareSelection finished and the capture is shown.
func (a *App) waitOverlayAnnotating(timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		a.mu.Lock()
		overlay := a.overlay
		a.mu.Unlock()
		if overlay != nil {
			js := `(() => {
				const toolbar = document.getElementById('annotationToolbar');
				const canvas = document.getElementById('overlayCanvas');
				return !!toolbar && !toolbar.hidden && !!canvas && !canvas.hidden && canvas.width > 0;
			})()`
			if v, err := overlay.Page().Eval(js); err == nil && v == true {
				return nil
			}
		}
		time.Sleep(120 * time.Millisecond)
	}
	return errors.New("overlay never reached the annotation state")
}

// selftestFlow exercises the round-3 flow end to end: drag a selection on the
// overlay, annotate the capture in place, then run each toolbar action.
func (a *App) selftestFlow() {
	if a.main != nil {
		a.main.Show()
	}
	time.Sleep(1200 * time.Millisecond)
	prepare := func(name string) bool {
		// The overlay page can miss the first PrepareSelection right after a
		// launch; retrying once keeps the selftest deterministic.
		for attempt := 1; attempt <= 2; attempt++ {
			if err := a.beginRegion(); err != nil {
				selftestFail("SELFTEST flow: %s begin error %v\n", name, err)
				return false
			}
			time.Sleep(1200 * time.Millisecond)
			if err := a.dragOverlaySelection(80, 90, 400, 300); err != nil {
				selftestFail("SELFTEST flow: %s drag error %v\n", name, err)
				return false
			}
			err := a.waitOverlayAnnotating(10 * time.Second)
			if err == nil {
				if err := a.drawOverlayAnnotation(); err != nil {
					selftestFail("SELFTEST flow: %s draw error %v\n", name, err)
					return false
				}
				time.Sleep(400 * time.Millisecond)
				return true
			}
			selftestFail("SELFTEST flow: %s prepare attempt %d error %v %v\n", name, attempt, err, a.overlayEval(`(() => { const c=document.getElementById('overlayCanvas'); const t=document.getElementById('annotationToolbar'); return JSON.stringify({canvas: c ? (c.hidden?'hidden':(c.width+'x'+c.height)) : 'missing', toolbar: t ? (t.hidden?'hidden':'shown') : 'missing', status: document.getElementById('overlayStatus')?.textContent, body: document.body.innerText.slice(-120)}); })()`))
			a.cancelRegion()
			time.Sleep(800 * time.Millisecond)
		}
		return false
	}
	click := func(name, action string) bool {
		if err := a.waitOverlayActionReady(action, 8*time.Second); err != nil {
			selftestFail("SELFTEST flow: %s ready error %v\n", name, err)
			return false
		}
		if err := a.clickOverlayAction(action); err != nil {
			selftestFail("SELFTEST flow: %s click error %v\n", name, err)
			return false
		}
		time.Sleep(1900 * time.Millisecond)
		fmt.Printf("SELFTEST flow: %s clicked %s\n", name, action)
		return true
	}
	// copy: annotated image on the clipboard
	if prepare("copy") {
		// The annotation toolbar is draggable; actions must still work after it
		// was moved out of the way.
		dragDelta := a.overlayEval(`(() => {
			const handle = document.getElementById('annotationHandle');
			const bar = document.getElementById('annotationToolbar');
			if (!handle || !bar) return 'missing';
			const before = bar.getBoundingClientRect();
			const r = handle.getBoundingClientRect();
			const cx = r.x + r.width / 2, cy = r.y + r.height / 2;
			handle.dispatchEvent(new PointerEvent('pointerdown', { bubbles: true, cancelable: true, clientX: cx, clientY: cy, pointerId: 7, pointerType: 'mouse', button: 0 }));
			window.dispatchEvent(new PointerEvent('pointermove', { bubbles: true, cancelable: true, clientX: cx + 60, clientY: cy + 40, pointerId: 7, pointerType: 'mouse' }));
			window.dispatchEvent(new PointerEvent('pointerup', { bubbles: true, cancelable: true, clientX: cx + 60, clientY: cy + 40, pointerId: 7, pointerType: 'mouse', button: 0 }));
			const after = bar.getBoundingClientRect();
			return Math.round(after.x - before.x) + ',' + Math.round(after.y - before.y);
		})()`)
		fmt.Printf("SELFTEST flow: toolbar_drag=%v\n", dragDelta)
		selftestRequire(dragDelta == "60,40", "toolbar drag mismatch: %v", dragDelta)
		click("copy", "copy")
		if img := mygo.Clipboard.ReadImage(); len(img) > 0 {
			w, h := imageDimensions(img)
			annotated := annotationPresent(img)
			fmt.Printf("SELFTEST flow: copy clipboard %dx%d bytes=%d annotated=%v\n", w, h, len(img), annotated)
			selftestRequire(w > 0 && h > 0 && annotated, "copy did not contain annotated image")
		} else {
			selftestFail("SELFTEST flow: copy clipboard EMPTY\n")
		}
	}
	// pin: annotated image on a desktop pin
	if prepare("pin") {
		click("pin", "pin")
		a.mu.Lock()
		pins := len(a.pins)
		a.mu.Unlock()
		fmt.Printf("SELFTEST flow: pin pins=%d\n", pins)
		selftestRequire(pins == 1, "expected one pin, got %d", pins)
	}
	// afterCapture=clipboard must run the action without the toolbar.
	mygo.Clipboard.Clear()
	auto := DefaultSettings()
	auto.AfterCapture = "clipboard"
	selftestSettingsErrors(a.applySettings(auto))
	time.Sleep(1000 * time.Millisecond)
	if err := a.beginRegion(); err != nil {
		selftestFail("begin region: %v", err)
	} else {
		time.Sleep(1500 * time.Millisecond)
		if err := a.dragOverlaySelection(60, 60, 260, 200); err != nil {
			selftestFail("SELFTEST flow: auto drag error %v\n", err)
		}
		time.Sleep(4000 * time.Millisecond)
		if img := mygo.Clipboard.ReadImage(); len(img) > 0 {
			w, h := imageDimensions(img)
			fmt.Printf("SELFTEST flow: auto clipboard %dx%d\n", w, h)
			selftestRequire(w > 0 && h > 0, "automatic clipboard image invalid")
		} else {
			selftestFail("SELFTEST flow: auto clipboard EMPTY status=%v visible=%v\n", a.overlayEval(`document.getElementById('overlayStatus')?.textContent`), a.overlayVisible())
		}
	}
	selftestSettingsErrors(a.applySettings(DefaultSettings()))
	time.Sleep(300 * time.Millisecond)
	// cancel: no capture should be delivered
	before := len(a.historyEntries())
	if prepare("cancel") {
		click("cancel", "cancel")
	}
	time.Sleep(600 * time.Millisecond)
	after := len(a.historyEntries())
	fmt.Printf("SELFTEST flow: cancel history before=%d after=%d\n", before, after)
	selftestRequire(before == after, "cancel added a history entry")
	for id := range a.pins {
		selftestCheckError(a.closePin(id))
	}
	selftestExit(0)
}

// annotationPresent reports whether the PNG contains pixels of the default
// annotation color, proving the drawing was composited into the result.
func annotationPresent(pngBytes []byte) bool {
	img, err := png.Decode(bytes.NewReader(pngBytes))
	if err != nil {
		return false
	}
	b := img.Bounds()
	stepX := max(1, b.Dx()/300)
	stepY := max(1, b.Dy()/300)
	matches := 0
	for y := b.Min.Y; y < b.Max.Y; y += stepY {
		for x := b.Min.X; x < b.Max.X; x += stepX {
			r, g, bl, _ := img.At(x, y).RGBA()
			if r>>8 > 190 && g>>8 > 70 && g>>8 < 150 && bl>>8 < 110 {
				matches++
				if matches > 5 {
					return true
				}
			}
		}
	}
	return false
}

// selftestPinUI drives the real pin page: its context menu, wheel scaling and
// close button, through the same bindings a user's clicks reach.
func (a *App) selftestPinUI() {
	time.Sleep(1200 * time.Millisecond)
	pngBytes := syntheticScreenshot()
	if len(pngBytes) == 0 {
		selftestFail("SELFTEST pinui: no image\n")
	}
	mygo.Clipboard.Clear()
	id, err := a.pinPNG(pngBytes, 1280, 800)
	if err != nil {
		selftestFail("SELFTEST pinui: pin error %v\n", err)
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
		selftestRequire(page != nil, "pin page missing")
		v, err := page.Eval(js)
		if err != nil {
			selftestFail("SELFTEST pinui: eval %q error %v\n", js, err)
			return nil
		}
		selftestRequire(v == "clicked" || v == "sent", "pin action was not delivered: %v", v)
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
		selftestRequire(w == 1280 && h == 800, "pin clipboard dimensions wrong")
	} else {
		selftestFail("SELFTEST pinui: dblclick clipboard EMPTY\n")
	}
	a.mu.Lock()
	after := mygo.Rectangle{}
	if win := a.pins[id]; win != nil {
		after = win.Bounds()
	}
	a.mu.Unlock()
	fmt.Printf("SELFTEST pinui: bounds unchanged=%v before=%+v after=%+v\n", before == after, before, after)
	selftestRequire(before.Width > 0 && before == after, "double click changed pin bounds")
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
	selftestRequire(mapAfterCopy && pinsAfterCopy == 1 && !destroyed && visible && storedAfterCopy == 1, "copy lost the pin")
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
	selftestRequire(math.Abs(opacityAfter-(opacityBefore-10)) < 0.01, "pin opacity did not decrease by 10")
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
	selftestRequire(afterWheel.Width > after.Width && afterWheel.Height > after.Height, "pin wheel did not grow bounds")
	// close through the hover close button: state must disappear
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
	selftestRequire(remaining == 0 && stored == 0, "pin close left live or stored pins")
	selftestExit(0)
}

// selftestPinPersistSetup creates one desktop pin and quits through the real
// quit path, so the next launch must restore it.
func (a *App) selftestPinPersistSetup() {
	time.Sleep(1200 * time.Millisecond)
	pngBytes := syntheticScreenshot()
	if len(pngBytes) == 0 {
		selftestFail("SELFTEST pinpersist: setup could not render image\n")
	}
	id, err := a.pinPNG(pngBytes, 640, 400)
	if err != nil {
		selftestFail("SELFTEST pinpersist: setup pin error %v\n", err)
	}
	time.Sleep(900 * time.Millisecond)
	a.mu.Lock()
	win := a.pins[id]
	st := a.pinStates[id]
	a.mu.Unlock()
	if win != nil {
		b := win.Bounds()
		fmt.Printf("SELFTEST pinpersist: setup created id=%s bounds=%+v stored=%+v\n", id, b, st)
		selftestRequire(a.pinStore != nil && len(a.pinStore.List()) == 1, "pin setup did not persist exactly one pin")
	} else {
		selftestFail("SELFTEST pinpersist: setup created id=%s but window is missing\n", id)
	}
	fmt.Printf("SELFTEST pinpersist: setup quitting\n")
	selftestRealQuit("pinpersist-setup")
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
	selftestRequire(count == 1 && stored == 1, "pin persistence expected one restored and stored pin, got %d/%d", count, stored)
	for _, id := range ids {
		if err := a.closePin(id); err != nil {
			selftestFail("SELFTEST pinpersist: close %s error %v\n", id, err)
		}
	}
	time.Sleep(400 * time.Millisecond)
	if a.pinStore != nil {
		fmt.Printf("SELFTEST pinpersist: check after close stored=%d\n", len(a.pinStore.List()))
		selftestRequire(len(a.pinStore.List()) == 0, "pin persistence close left stored pins")
	}
	selftestExit(0)
}

// controlEval evaluates an expression in the scroll control window.
func (a *App) controlEval(js string) any {
	a.mu.Lock()
	control := a.control
	a.mu.Unlock()
	selftestRequire(control != nil, "scroll control missing")
	v, err := control.Page().Eval(js)
	if err != nil {
		selftestFail("eval: %v", err)
		return nil
	}
	return v
}

// waitControlReady waits until the scroll control page has loaded its UI.
func (a *App) waitControlReady(timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		v := a.controlEval("!!document.getElementById('scrollProgress')")
		if ready, ok := v.(bool); ok && ready {
			return nil
		}
		time.Sleep(120 * time.Millisecond)
	}
	return errors.New("scroll control window never became ready")
}

// selftestScrollShot captures the scroll control window (while running) and
// the result preview window for the README.
func (a *App) selftestScrollShot() {
	if a.main != nil {
		a.main.Show()
	}
	time.Sleep(1200 * time.Millisecond)
	if err := a.beginRegion(); err != nil {
		selftestFail("SELFTEST scrollshot: begin error %v\n", err)
	}
	time.Sleep(1000 * time.Millisecond)
	if err := a.dragOverlaySelection(80, 90, 420, 330); err != nil {
		selftestFail("SELFTEST scrollshot: drag error %v\n", err)
	}
	if err := a.waitOverlayAnnotating(10 * time.Second); err != nil {
		selftestFail("SELFTEST scrollshot: prepare error %v\n", err)
	}
	time.Sleep(300 * time.Millisecond)
	selftestRequire(selftestValue(a.overlay.Page().Eval(`(() => { const b = document.querySelector('[data-tool="scroll"]'); if (!b) return 'missing'; b.click(); return 'clicked'; })()`)) == "clicked", "scroll tool missing")
	if err := a.waitControlReady(8 * time.Second); err != nil {
		selftestFail("SELFTEST scrollshot: control error %v\n", err)
	}
	time.Sleep(1200 * time.Millisecond)
	a.mu.Lock()
	control := a.control
	a.mu.Unlock()
	selftestRequire(control != nil, "scroll control missing")
	if control != nil {
		selftestRequire(control.IsVisible(), "scroll control is hidden")
		if pagePNG, err := control.CapturePage(); err == nil {
			selftestArtifact("pinstill-showcase-scroll.png", pagePNG)
			w, h := imageDimensions(pagePNG)
			fmt.Printf("SELFTEST scrollshot: control %dx%d -> artifacts/pinstill-showcase-scroll.png\n", w, h)
		} else {
			selftestFail("SELFTEST scrollshot: control capture error %v\n", err)
		}
	}
	// Stop the simulated session and hand the preview window a generated tall
	// image (the simulated frames are random noise, ugly for the README).
	selftestCheckError(a.finishScroll("cancel"))
	time.Sleep(700 * time.Millisecond)
	tall := syntheticScrollResultPNG()
	selftestRequire(len(tall) > 0, "synthetic scroll image missing")
	if len(tall) > 0 {
		if err := a.showScrollResult(tall, 8, false); err != nil {
			selftestFail("SELFTEST scrollshot: preview error %v\n", err)
		}
	}
	a.mu.Lock()
	win := a.result
	a.mu.Unlock()
	selftestRequire(win != nil, "scroll preview missing")
	if win != nil {
		time.Sleep(900 * time.Millisecond)
		if pagePNG, err := win.CapturePage(); err == nil {
			selftestArtifact("pinstill-showcase-scroll-result.png", pagePNG)
			w, h := imageDimensions(pagePNG)
			fmt.Printf("SELFTEST scrollshot: result %dx%d -> artifacts/pinstill-showcase-scroll-result.png\n", w, h)
		} else {
			selftestFail("SELFTEST scrollshot: result capture error %v\n", err)
		}
		a.closeScrollResult()
	}
	time.Sleep(500 * time.Millisecond)
	selftestExit(0)
}

// selftestScrollFlow drives the real page: annotate a capture, click the
// 滚动截图 tool, and wait for the automatic finish to open the result preview
// window, whose DOM must expose the long image and its actions.
func (a *App) selftestScrollFlow() {
	if a.main != nil {
		a.main.Show()
	}
	time.Sleep(1200 * time.Millisecond)
	if err := a.beginRegion(); err != nil {
		selftestFail("SELFTEST scrollflow: begin error %v\n", err)
	}
	time.Sleep(1000 * time.Millisecond)
	if err := a.dragOverlaySelection(80, 90, 420, 330); err != nil {
		selftestFail("SELFTEST scrollflow: drag error %v\n", err)
	}
	if err := a.waitOverlayAnnotating(10 * time.Second); err != nil {
		selftestFail("SELFTEST scrollflow: prepare error %v\n", err)
	}
	time.Sleep(400 * time.Millisecond)
	selftestRequire(selftestValue(a.overlay.Page().Eval(`(() => { const b = document.querySelector('[data-tool="scroll"]'); if (!b) return 'missing'; b.click(); return 'clicked'; })()`)) == "clicked", "scroll tool missing")
	deadline := time.Now().Add(25 * time.Second)
	var win *mygo.Window
	for time.Now().Before(deadline) {
		a.mu.Lock()
		win = a.result
		a.mu.Unlock()
		if win != nil {
			break
		}
		time.Sleep(150 * time.Millisecond)
	}
	if win == nil {
		selftestFail("SELFTEST scrollflow: result window never opened\n")
	}
	time.Sleep(900 * time.Millisecond)
	eval := func(js string) string {
		v, err := win.Page().Eval(js)
		if err != nil {
			selftestFail("eval: %v", err)
			return ""
		}
		return strings.TrimSpace(fmt.Sprint(v))
	}
	imageState := eval(`String((() => { const img = document.getElementById('resultImage'); return !!img && img.complete && img.naturalWidth > 0 ? (img.naturalWidth + 'x' + img.naturalHeight) : 'missing'; })())`)
	meta := eval(`String(document.getElementById('resultMeta')?.textContent ?? '')`)
	actions := eval(`[...document.querySelectorAll('[data-result-action]')].map((b) => b.dataset.resultAction).join(',')`)
	metaInfo := a.scrollResultMeta()
	fmt.Printf("SELFTEST scrollflow: result=%dx%d frames=%d window=true\n", metaInfo.Width, metaInfo.Height, metaInfo.Frames)
	fmt.Printf("SELFTEST scrollflow: image=%s meta=%q actions=%s\n", imageState, meta, actions)
	selftestRequire(metaInfo.Width > 0 && metaInfo.Height > 0 && metaInfo.Frames > 0 && imageState == fmt.Sprintf("%dx%d", metaInfo.Width, metaInfo.Height) && meta != "", "scroll result image/meta mismatch")
	for _, action := range []string{"copy", "save", "pin", "close"} {
		selftestRequire(strings.Contains(","+actions+",", ","+action+","), "scroll result missing %s", action)
	}
	// Let Eval return before the click tears down its WebView.
	selftestRequire(eval(`(() => {
		const button = document.querySelector('[data-result-action="close"]');
		if (!button) return 'missing';
		setTimeout(() => button.click(), 100);
		return 'scheduled';
	})()`) == "scheduled", "scroll result close was not scheduled")
	time.Sleep(700 * time.Millisecond)
	a.mu.Lock()
	stillOpen := a.result != nil
	a.mu.Unlock()
	fmt.Printf("SELFTEST scrollflow: closed=%v\n", !stillOpen)
	selftestRequire(!stillOpen, "scroll result did not close")
	selftestExit(0)
}

// selftestScroll exercises the scrolling capture in automatic mode with
// simulated frames: the session must finish by itself, open the result
// preview window and let the result be copied.
func (a *App) selftestScroll() {
	accessibility := capture.AccessibilityGranted()
	fmt.Printf("SELFTEST scroll: accessibility=%v supported=%v\n", accessibility, capture.ScrollSupported())
	if a.main != nil {
		a.main.Hide()
	}
	if err := a.beginRegion(); err != nil {
		selftestFail("SELFTEST scroll: begin error %v\n", err)
	}
	time.Sleep(1000 * time.Millisecond)
	rect := Rect{X: 60, Y: 80, Width: 520, Height: 420}
	if err := a.startScroll(rect, true); err != nil {
		selftestFail("SELFTEST scroll: start error %v\n", err)
	}
	time.Sleep(500 * time.Millisecond)
	frameVisible := a.scrollFrameVisible()
	frameW, frameH := a.scrollFrameSize()
	fmt.Printf("SELFTEST scroll: frame_visible=%v frame_size=%dx%d\n", frameVisible, frameW, frameH)
	deadline := time.Now().Add(25 * time.Second)
	var win *mygo.Window
	for time.Now().Before(deadline) {
		a.mu.Lock()
		win = a.result
		a.mu.Unlock()
		if win != nil {
			break
		}
		time.Sleep(150 * time.Millisecond)
	}
	meta := a.scrollResultMeta()
	fmt.Printf("SELFTEST scroll: result=%dx%d frames=%d window=%v capped=%v\n", meta.Width, meta.Height, meta.Frames, win != nil, meta.Capped)
	if win == nil || meta.Width <= 0 || meta.Height <= 0 || meta.Frames <= 0 {
		selftestFail("SELFTEST scroll: checks=FAILED\n")
	}
	mygo.Clipboard.Clear()
	if err := a.copyScrollResult(); err != nil {
		selftestFail("SELFTEST scroll: copy error %v\n", err)
	}
	time.Sleep(700 * time.Millisecond)
	if img := mygo.Clipboard.ReadImage(); len(img) > 0 {
		width, height := imageDimensions(img)
		fmt.Printf("SELFTEST scroll: clipboard=%dx%d\n", width, height)
		selftestRequire(width == meta.Width && height == meta.Height, "scroll clipboard dimensions mismatch")
	} else {
		selftestFail("SELFTEST scroll: clipboard EMPTY\n")
	}
	a.mu.Lock()
	stillOpen := a.result != nil
	a.mu.Unlock()
	fmt.Printf("SELFTEST scroll: closed_after_copy=%v\n", !stillOpen)
	selftestRequire(!stillOpen, "scroll copy did not close result")
	fmt.Printf("SELFTEST scroll: frame_hidden=%v\n", !a.scrollFrameVisible())
	if !frameVisible || frameW != 524 || frameH != 424 || a.scrollFrameVisible() {
		selftestFail("SELFTEST scroll: checks=FAILED\n")
	}
	selftestExit(0)
}

// selftestScrollManual starts a manual scrolling session: the simulated frames
// stop changing, so the session must finish by itself and open the preview.
func (a *App) selftestScrollManual() {
	if a.main != nil {
		a.main.Hide()
	}
	if err := a.beginRegion(); err != nil {
		selftestFail("SELFTEST scrollmanual: begin error %v\n", err)
	}
	time.Sleep(1000 * time.Millisecond)
	rect := Rect{X: 60, Y: 80, Width: 520, Height: 420}
	if err := a.startScroll(rect, false); err != nil {
		selftestFail("SELFTEST scrollmanual: start error %v\n", err)
	}
	time.Sleep(500 * time.Millisecond)
	frameVisible := a.scrollFrameVisible()
	frameW, frameH := a.scrollFrameSize()
	fmt.Printf("SELFTEST scrollmanual: frame_visible=%v frame_size=%dx%d\n", frameVisible, frameW, frameH)
	deadline := time.Now().Add(25 * time.Second)
	var win *mygo.Window
	for time.Now().Before(deadline) {
		a.mu.Lock()
		win = a.result
		a.mu.Unlock()
		if win != nil {
			break
		}
		time.Sleep(150 * time.Millisecond)
	}
	meta := a.scrollResultMeta()
	fmt.Printf("SELFTEST scrollmanual: result=%dx%d frames=%d window=%v\n", meta.Width, meta.Height, meta.Frames, win != nil)
	if win != nil {
		a.closeScrollResult()
		time.Sleep(500 * time.Millisecond)
	}
	if win == nil || meta.Width <= 0 || meta.Height <= 0 || meta.Frames <= 0 {
		selftestFail("SELFTEST scrollmanual: checks=FAILED\n")
	}
	frameHidden := !a.scrollFrameVisible()
	fmt.Printf("SELFTEST scrollmanual: frame_hidden=%v\n", frameHidden)
	if !frameVisible || frameW != 524 || frameH != 424 || !frameHidden {
		selftestFail("SELFTEST scrollmanual: checks=FAILED\n")
	}
	selftestExit(0)
}

// selftestShowcase produces the UI screenshots used by the README. Every
// image it shows is generated, so no content from the user's desktop lands
// in the repository.
func (a *App) selftestShowcase() {
	if strings.TrimSpace(os.Getenv("PINSTILL_DATA_DIR")) == "" {
		fmt.Printf("SELFTEST showcase: PINSTILL_DATA_DIR must point at an isolated data directory\n")
		selftestExit(1)
		return
	}
	pngBytes := syntheticScreenshot()
	if len(pngBytes) == 0 {
		selftestFail("SELFTEST showcase: no synthetic image\n")
	}
	if a.main != nil {
		a.main.Hide()
	}
	// Seed the history grid with generated captures only.
	for i := 0; i < 3; i++ {
		if _, err := a.storeCapture(pngBytes); err != nil {
			selftestFail("SELFTEST showcase: seed error %v\n", err)
		}
	}
	a.showMain()
	time.Sleep(1600 * time.Millisecond)
	savePage := func(win *mygo.Window, name string) {
		selftestRequire(win != nil, "showcase %s window missing", name)
		pagePNG, err := win.CapturePage()
		if err != nil {
			selftestFail("SELFTEST showcase: %s capture error %v\n", name, err)
			return
		}
		path := "pinstill-showcase-" + name + ".png"
		selftestArtifact(path, pagePNG)
		w, h := imageDimensions(pagePNG)
		fmt.Printf("SELFTEST showcase: %s %dx%d -> %s\n", name, w, h, path)
	}
	savePage(a.main, "history")
	if a.main != nil {
		if _, err := a.main.Page().Eval(`document.querySelector('#historyGrid .history-card').click()`); err != nil {
			selftestFail("showcase preview: %v", err)
		} else {
			time.Sleep(800 * time.Millisecond)
			savePage(a.main, "preview")
			selftestValue(a.main.Page().Eval(`document.body.dispatchEvent(new KeyboardEvent('keydown', { key: 'Escape', bubbles: true, cancelable: true }))`))
			time.Sleep(400 * time.Millisecond)
		}
		selftestValue(a.main.Page().Eval(`document.querySelector('[data-view="settings"]').click()`))
		time.Sleep(800 * time.Millisecond)
		savePage(a.main, "settings")
	}
	// Overlay with a live selection and its annotation toolbar, captured from
	// the overlay's own page so the desktop behind it stays out of the image.
	if err := a.beginRegion(); err != nil {
		selftestFail("begin region: %v", err)
	} else {
		time.Sleep(900 * time.Millisecond)
		if err := a.dragOverlaySelection(120, 140, 620, 420); err != nil {
			selftestFail("SELFTEST showcase: overlay drag error %v\n", err)
		}
		if err := a.waitOverlayAnnotating(8 * time.Second); err != nil {
			selftestFail("SELFTEST showcase: overlay prepare error %v\n", err)
		} else {
			if png := syntheticScreenshot(); len(png) > 0 {
				dataURL := "data:image/png;base64," + base64.StdEncoding.EncodeToString(png)
				js := fmt.Sprintf("window.__pinstillDemoCapture && window.__pinstillDemoCapture(%q)", dataURL)
				if _, err := a.overlay.Page().Eval(js); err != nil {
					selftestFail("SELFTEST showcase: synthetic base error %v\n", err)
				}
			}
			if err := a.drawOverlayAnnotation(); err != nil {
				selftestFail("SELFTEST showcase: overlay draw error %v\n", err)
			}
			// Show the text formatting controls and a sample text annotation.
			selftestValue(a.overlay.Page().Eval(`document.querySelector('[data-tool="text"]')?.click()`))
			time.Sleep(250 * time.Millisecond)
			selftestValue(a.overlay.Page().Eval(`(() => {
				const c = document.getElementById('overlayCanvas');
				const r = c.getBoundingClientRect();
				c.dispatchEvent(new PointerEvent('pointerdown', { bubbles: true, cancelable: true, clientX: r.left + r.width * 0.14, clientY: r.top + r.height * 0.2, pointerId: 5, pointerType: 'mouse', button: 0 }));
			})()`))
			time.Sleep(300 * time.Millisecond)
			selftestValue(a.overlay.Page().Eval(`(() => {
				const t = document.getElementById('textInputOverlay');
				if (!t) return;
				t.value = 'Pinstill · 标注';
				t.dispatchEvent(new KeyboardEvent('keydown', { key: 'Enter', bubbles: true, cancelable: true }));
			})()`))
			time.Sleep(350 * time.Millisecond)
		}
		time.Sleep(600 * time.Millisecond)
		savePage(a.overlay, "overlay")
		// Watermark screenshot: the tiled watermark plus its settings row.
		// The stored defaults are restored afterwards so the README run never
		// changes the user's watermark text/opacity.
		if a.overlay != nil {
			savedWatermark := ""
			savedWatermark = fmt.Sprint(selftestValue(a.overlay.Page().Eval("localStorage.getItem('pinstill.watermark') ?? ''")))
			selftestValue(a.overlay.Page().Eval(`document.querySelector('[data-tool="watermark"]')?.click()`))
			time.Sleep(300 * time.Millisecond)
			selftestValue(a.overlay.Page().Eval(`(() => {
				const t = document.getElementById('watermarkText');
				if (t) { t.value = 'Pinstill'; t.dispatchEvent(new Event('input', { bubbles: true })); }
				const o = document.getElementById('watermarkOpacity');
				if (o) { o.value = '30'; o.dispatchEvent(new Event('input', { bubbles: true })); }
			})()`))
			time.Sleep(500 * time.Millisecond)
			savePage(a.overlay, "watermark")
			selftestValue(a.overlay.Page().Eval(`document.querySelector('[data-tool="watermark"]')?.click()`))
			if savedWatermark == "" {
				selftestValue(a.overlay.Page().Eval("localStorage.removeItem('pinstill.watermark')"))
			} else {
				selftestValue(a.overlay.Page().Eval(fmt.Sprintf("localStorage.setItem('pinstill.watermark', %q)", savedWatermark)))
			}
			time.Sleep(300 * time.Millisecond)
		}
		a.cancelRegion()
		time.Sleep(700 * time.Millisecond)
	}
	// A desktop pin showing the generated image.
	if id, err := a.pinPNG(pngBytes, 1280, 800); err == nil {
		time.Sleep(900 * time.Millisecond)
		a.mu.Lock()
		win := a.pins[id]
		a.mu.Unlock()
		savePage(win, "pin")
		selftestCheckError(a.closePin(id))
	} else {
		selftestFail("SELFTEST showcase: pin error %v\n", err)
	}
	time.Sleep(400 * time.Millisecond)
	selftestExit(0)
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
	selftestRealQuit("quit")
}

// selftestFullScreen exercises the whole-display capture from the tray: it
// stores the capture in history and copies it to the clipboard, without
// opening an editor.
func (a *App) selftestFullScreen() {
	if a.main != nil {
		a.main.Hide()
	}
	s := DefaultSettings()
	s.AfterCapture = "clipboard"
	selftestSettingsErrors(a.applySettings(s))
	time.Sleep(600 * time.Millisecond)
	mygo.Clipboard.Clear()
	before := len(a.historyEntries())
	a.captureFullScreen()
	deadline := time.Now().Add(12 * time.Second)
	var img []byte
	for time.Now().Before(deadline) {
		if img = mygo.Clipboard.ReadImage(); len(img) > 0 {
			break
		}
		time.Sleep(150 * time.Millisecond)
	}
	after := len(a.historyEntries())
	width, height := imageDimensions(img)
	fmt.Printf("SELFTEST fullscreen: clipboard=%dx%d history before=%d after=%d\n", width, height, before, after)
	selftestRequire(width > 0 && height > 0 && after == before+1, "fullscreen capture missing clipboard image or history entry")
	if len(img) == 0 {
		selftestFail("SELFTEST fullscreen: checks=FAILED\n")
	}
	selftestExit(0)
}

// selftestHistoryActions exercises the history preview actions that do not
// need a dialog (copy / pin / remove) through the History service bindings.
func (a *App) selftestHistoryActions() {
	pngBytes := syntheticScreenshot()
	if len(pngBytes) == 0 {
		selftestFail("SELFTEST historyactions: no synthetic image\n")
	}
	if _, err := a.storeCapture(pngBytes); err != nil {
		selftestFail("SELFTEST historyactions: seed error %v\n", err)
	}
	entries := a.historyEntries()
	if len(entries) == 0 {
		selftestFail("SELFTEST historyactions: no history entries\n")
	}
	id := entries[0].ID
	history := &HistoryService{app: a}
	ok := true
	mygo.Clipboard.Clear()
	if err := history.Copy(id); err != nil {
		ok = false
		selftestFail("SELFTEST historyactions: copy error %v\n", err)
	}
	copiedW, copiedH := imageDimensions(mygo.Clipboard.ReadImage())
	pinID, pinErr := history.Pin(id)
	a.mu.Lock()
	pinCount := len(a.pins)
	a.mu.Unlock()
	if pinErr != nil {
		ok = false
		selftestFail("SELFTEST historyactions: pin error %v\n", pinErr)
	} else if pinCount == 0 {
		ok = false
		selftestFail("SELFTEST historyactions: pin did not open a window\n")
	}
	if pinID != "" {
		selftestCheckError(a.closePin(pinID))
	}
	before := len(a.historyEntries())
	if err := history.Remove(id); err != nil {
		ok = false
		selftestFail("SELFTEST historyactions: remove error %v\n", err)
	}
	after := len(a.historyEntries())
	fmt.Printf("SELFTEST historyactions: clipboard=%dx%d pins=%d remove before=%d after=%d delta=%d\n", copiedW, copiedH, pinCount, before, after, before-after)
	selftestRequire(copiedW == 1280 && copiedH == 800 && pinCount == 1 && before-after == 1, "history action measurements mismatch")
	if copiedW == 0 {
		ok = false
	}
	if ok {
		selftestExit(0)
		return
	}
	selftestFail("SELFTEST historyactions: checks=FAILED\n")
	selftestExit(1)
}

// selftestTextStyle drives the text tool's formatting controls: they only show
// for the text tool, bold / italic / size / font apply, and typing commits the
// annotation with the chosen style.
func (a *App) selftestTextStyle() {
	if a.main != nil {
		a.main.Show()
	}
	time.Sleep(1200 * time.Millisecond)
	// The overlay page can miss the first PrepareSelection right after a
	// launch; retrying once keeps the selftest deterministic (same as flow).
	annotating := false
	for attempt := 1; attempt <= 2 && !annotating; attempt++ {
		if err := a.beginRegion(); err != nil {
			selftestFail("SELFTEST textstyle: begin error %v\n", err)
			break
		}
		time.Sleep(1200 * time.Millisecond)
		if err := a.dragOverlaySelection(80, 90, 520, 380); err != nil {
			selftestFail("SELFTEST textstyle: drag error %v\n", err)
		}
		if err := a.waitOverlayAnnotating(10 * time.Second); err == nil {
			annotating = true
			break
		}
		selftestFail("SELFTEST textstyle: annotating attempt %d failed %v\n", attempt, a.overlayEval(`(() => { const c = document.getElementById('overlayCanvas'); const t = document.getElementById('annotationToolbar'); return JSON.stringify({ canvas: c ? (c.hidden ? 'hidden' : c.width + 'x' + c.height) : 'missing', toolbar: t ? (t.hidden ? 'hidden' : 'shown') : 'missing', phase: document.getElementById('overlayStatus')?.textContent }); })()`))
		a.cancelRegion()
		time.Sleep(800 * time.Millisecond)
	}
	if !annotating {
		selftestFail("SELFTEST textstyle: checks=FAILED\n")
	}
	eval := func(js string) string {
		return strings.TrimSpace(fmt.Sprint(a.overlayEval(js)))
	}
	// Keep the temporary test session's text defaults stable between steps.
	// Selftest windows share an ephemeral store, separate from the user's app.
	savedStyle := ""
	if a.main != nil {
		savedStyle = fmt.Sprint(selftestValue(a.main.Page().Eval("localStorage.getItem('pinstill.textStyle') ?? ''")))
	}
	restoreStyle := func() {
		if a.main == nil {
			return
		}
		if savedStyle == "" {
			selftestValue(a.main.Page().Eval("localStorage.removeItem('pinstill.textStyle')"))
			return
		}
		selftestValue(a.main.Page().Eval(fmt.Sprintf("localStorage.setItem('pinstill.textStyle', %q)", savedStyle)))
	}
	ok := true
	failCheck := func(format string, args ...any) {
		ok = false
		selftestFail(format, args...)
	}
	eval(`document.querySelector('[data-tool="text"]')?.click(), 'ok'`)
	time.Sleep(350 * time.Millisecond)
	propsVisible := eval(`String((() => { const p = document.getElementById('overlayTextProps'); return !!p && !p.hidden; })())`)
	widthHidden := eval(`String((() => { const w = document.getElementById('overlayWidthLabel'); return !!w && w.hidden; })())`)
	eval(`(() => { const b = document.getElementById('textBold'); if (b && b.getAttribute('aria-pressed') !== 'true') b.click(); return 'ok'; })()`)
	eval(`(() => { const b = document.getElementById('textItalic'); if (b && b.getAttribute('aria-pressed') !== 'true') b.click(); return 'ok'; })()`)
	eval(`(() => { const s = document.getElementById('textSize'); s.value = '20'; s.dispatchEvent(new Event('change', { bubbles: true })); })()`)
	eval(`(() => { const f = document.getElementById('textFont'); f.selectedIndex = Math.min(3, f.options.length - 1); f.dispatchEvent(new Event('change', { bubbles: true })); })()`)
	bold := eval(`document.getElementById('textBold')?.getAttribute('aria-pressed') ?? ''`)
	italic := eval(`document.getElementById('textItalic')?.getAttribute('aria-pressed') ?? ''`)
	size := eval(`document.getElementById('textSize')?.value ?? ''`)
	fontOptions := eval(`String(document.getElementById('textFont')?.options.length ?? 0)`)
	eval(`(() => {
		const c = document.getElementById('overlayCanvas');
		const r = c.getBoundingClientRect();
		const opts = { bubbles: true, cancelable: true, clientX: r.left + r.width * 0.2, clientY: r.top + r.height * 0.3, pointerId: 3, pointerType: 'mouse', button: 0 };
		c.dispatchEvent(new PointerEvent('pointerdown', opts));
		return 'ok';
	})()`)
	time.Sleep(400 * time.Millisecond)
	inputShown := eval(`String(!!document.getElementById('textInputOverlay'))`)
	committed := eval(`(() => { const t = document.getElementById('textInputOverlay'); if (!t) return 'missing'; t.value = 'Pinstill 标注'; t.dispatchEvent(new KeyboardEvent('keydown', { key: 'Enter', bubbles: true, cancelable: true })); return 'sent'; })()`)
	time.Sleep(450 * time.Millisecond)
	inputGone := eval(`String(!document.getElementById('textInputOverlay'))`)
	fmt.Printf("SELFTEST textstyle: props=%s width_hidden=%s bold=%s italic=%s size=%s fonts=%s\n", propsVisible, widthHidden, bold, italic, size, fontOptions)
	fmt.Printf("SELFTEST textstyle: input=%s commit=%s input_gone=%s\n", inputShown, committed, inputGone)
	if propsVisible != "true" {
		failCheck("text style controls are not visible for the text tool")
	}
	if widthHidden != "true" {
		failCheck("line-width slider is still shown for the text tool")
	}
	if bold != "true" {
		failCheck("bold toggle did not turn on")
	}
	if italic != "true" {
		failCheck("italic toggle did not turn on")
	}
	if size != "20" {
		failCheck("font size did not change to 20pt (got %q)", size)
	}
	if fontOptions == "0" || fontOptions == "" {
		failCheck("font family select is empty")
	}
	if inputShown != "true" {
		failCheck("inline text editor did not open")
	}
	if committed != "sent" || inputGone != "true" {
		failCheck("text annotation did not commit (commit=%q gone=%q)", committed, inputGone)
	}
	restoreStyle()
	if ok {
		selftestExit(0)
		return
	}
	selftestFail("SELFTEST textstyle: checks=FAILED\n")
	selftestExit(1)
}

// selftestDoubleClick proves that double-clicking the captured image finishes
// the capture with a copy (the toolbar's 复制 button is a shortcut, not the
// only way out).
func (a *App) selftestDoubleClick() {
	if a.main != nil {
		a.main.Show()
	}
	time.Sleep(1200 * time.Millisecond)
	annotating := false
	for attempt := 1; attempt <= 2 && !annotating; attempt++ {
		if err := a.beginRegion(); err != nil {
			selftestFail("SELFTEST dblclick: begin error %v\n", err)
			break
		}
		time.Sleep(1200 * time.Millisecond)
		if err := a.dragOverlaySelection(80, 90, 400, 300); err != nil {
			selftestFail("SELFTEST dblclick: drag error %v\n", err)
		}
		if err := a.waitOverlayAnnotating(10 * time.Second); err == nil {
			annotating = true
			break
		}
		selftestFail("SELFTEST dblclick: annotating attempt %d failed\n", attempt)
		a.cancelRegion()
		time.Sleep(800 * time.Millisecond)
	}
	if !annotating {
		selftestFail("SELFTEST dblclick: checks=FAILED\n")
	}
	click := func() string {
		return strings.TrimSpace(fmt.Sprint(a.overlayEval(`(() => {
			const c = document.getElementById('overlayCanvas');
			if (!c) return 'no-canvas';
			const r = c.getBoundingClientRect();
			const opts = { bubbles: true, cancelable: true, clientX: r.left + r.width / 2, clientY: r.top + r.height / 2, pointerId: 9, pointerType: 'mouse', button: 0 };
			c.dispatchEvent(new PointerEvent('pointerdown', opts));
			window.dispatchEvent(new PointerEvent('pointerup', opts));
			return 'clicked';
		})()`)))
	}
	mygo.Clipboard.Clear()
	first := click()
	time.Sleep(120 * time.Millisecond)
	second := click()
	time.Sleep(2200 * time.Millisecond)
	img := mygo.Clipboard.ReadImage()
	width, height := imageDimensions(img)
	fmt.Printf("SELFTEST dblclick: first=%s second=%s clipboard=%dx%d\n", first, second, width, height)
	selftestRequire(first == "clicked" && second == "clicked" && width > 0 && height > 0, "double click did not copy image")
	if width == 0 {
		selftestFail("SELFTEST dblclick: checks=FAILED\n")
	}
	selftestExit(0)
}

// selftestResize proves the capture can be adjusted after it landed: dragging
// a corner handle of the selection re-captures the adjusted region.
func (a *App) selftestResize() {
	if a.main != nil {
		a.main.Show()
	}
	time.Sleep(1200 * time.Millisecond)
	annotating := false
	for attempt := 1; attempt <= 2 && !annotating; attempt++ {
		if err := a.beginRegion(); err != nil {
			selftestFail("SELFTEST resize: begin error %v\n", err)
			break
		}
		time.Sleep(1200 * time.Millisecond)
		if err := a.dragOverlaySelection(80, 90, 400, 300); err != nil {
			selftestFail("SELFTEST resize: drag error %v\n", err)
		}
		if err := a.waitOverlayAnnotating(10 * time.Second); err == nil {
			annotating = true
			break
		}
		selftestFail("SELFTEST resize: annotating attempt %d failed\n", attempt)
		a.cancelRegion()
		time.Sleep(800 * time.Millisecond)
	}
	if !annotating {
		selftestFail("SELFTEST resize: checks=FAILED\n")
	}
	eval := func(js string) string {
		return strings.TrimSpace(fmt.Sprint(a.overlayEval(js)))
	}
	handles := eval(`String(document.querySelectorAll('[data-resize]').length)`)
	before := eval(`String((() => { const c = document.getElementById('overlayCanvas'); return c ? c.width + 'x' + c.height : 'missing'; })())`)
	handlePoint := eval(`(() => {
		const handle = document.querySelector('[data-resize="se"]');
		if (!handle) return '';
		const r = handle.getBoundingClientRect();
		return (r.left + r.width / 2) + ',' + (r.top + r.height / 2);
	})()`)
	down := eval(`(() => {
		const handle = document.querySelector('[data-resize="se"]');
		if (!handle) return 'missing';
		const r = handle.getBoundingClientRect();
		handle.dispatchEvent(new PointerEvent('pointerdown', { bubbles: true, cancelable: true, clientX: r.left + r.width / 2, clientY: r.top + r.height / 2, pointerId: 11, pointerType: 'mouse', button: 0 }));
		return 'down';
	})()`)
	time.Sleep(300 * time.Millisecond)
	midHidden := eval(`String(document.getElementById('overlayCanvas')?.hidden ?? 'missing')`)
	midMask := eval(`String(!!document.getElementById('maskTop')?.hidden)`)
	fmt.Printf("SELFTEST resize: pressed=%s canvas_hidden_mid=%s masks_hidden_mid=%s point=%s\n", down, midHidden, midMask, handlePoint)
	if a.overlay != nil {
		if pagePNG, err := a.overlay.CapturePage(); err == nil {
			selftestArtifact("pinstill-resize-mid.png", pagePNG)
		} else {
			selftestFail("resize capture: %v", err)
		}
	}
	dragged := eval(`(() => {
		const handle = document.querySelector('[data-resize="se"]');
		if (!handle) return 'missing';
		const r = handle.getBoundingClientRect();
		const cx = r.left + r.width / 2;
		const cy = r.top + r.height / 2;
		window.dispatchEvent(new PointerEvent('pointermove', { bubbles: true, cancelable: true, clientX: cx + 120, clientY: cy + 80, pointerId: 11, pointerType: 'mouse' }));
		window.dispatchEvent(new PointerEvent('pointerup', { bubbles: true, cancelable: true, clientX: cx + 120, clientY: cy + 80, pointerId: 11, pointerType: 'mouse', button: 0 }));
		return 'dragged';
	})()`)
	dragDone := time.Now()
	// The page re-captures the adjusted region through Go; wait for the new
	// annotation state and read the canvas size again.
	recaptured := false
	deadline := time.Now().Add(12 * time.Second)
	for time.Now().Before(deadline) {
		if err := a.waitOverlayAnnotating(2 * time.Second); err == nil {
			after := eval(`String((() => { const c = document.getElementById('overlayCanvas'); return c ? c.width + 'x' + c.height : 'missing'; })())`)
			if after != before && after != "missing" {
				fmt.Printf("SELFTEST resize: handles=%s before=%s after=%s recapture_ms=%d dragged=%s\n", handles, before, after, time.Since(dragDone).Milliseconds(), dragged)
				recaptured = true
				break
			}
		}
		time.Sleep(250 * time.Millisecond)
	}
	if !recaptured {
		fmt.Printf("SELFTEST resize: handles=%s before=%s dragged=%s after=unchanged\n", handles, before, dragged)
	}
	// Now shrink the region (inside the current capture): the page crops
	// locally instead of grabbing the screen again, so this should be fast.
	beforeShrink := eval(`String((() => { const c = document.getElementById('overlayCanvas'); return c ? c.width + 'x' + c.height : 'missing'; })())`)
	shrinkStart := time.Now()
	shrinkDragged := eval(`(() => {
		const handle = document.querySelector('[data-resize="nw"]');
		if (!handle) return 'missing';
		const r = handle.getBoundingClientRect();
		const cx = r.left + r.width / 2;
		const cy = r.top + r.height / 2;
		handle.dispatchEvent(new PointerEvent('pointerdown', { bubbles: true, cancelable: true, clientX: cx, clientY: cy, pointerId: 12, pointerType: 'mouse', button: 0 }));
		window.dispatchEvent(new PointerEvent('pointermove', { bubbles: true, cancelable: true, clientX: cx + 100, clientY: cy + 60, pointerId: 12, pointerType: 'mouse' }));
		window.dispatchEvent(new PointerEvent('pointerup', { bubbles: true, cancelable: true, clientX: cx + 100, clientY: cy + 60, pointerId: 12, pointerType: 'mouse', button: 0 }));
		return 'dragged';
	})()`)
	afterShrink := beforeShrink
	shrinkDeadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(shrinkDeadline) {
		afterShrink = eval(`String((() => { const c = document.getElementById('overlayCanvas'); return c ? c.width + 'x' + c.height : 'missing'; })())`)
		if afterShrink != beforeShrink && afterShrink != "missing" {
			break
		}
		time.Sleep(50 * time.Millisecond)
	}
	shrinkMS := time.Since(shrinkStart).Milliseconds()
	fmt.Printf("SELFTEST resize: shrink before=%s after=%s shrink_ms=%d dragged=%s\n", beforeShrink, afterShrink, shrinkMS, shrinkDragged)
	if handles != "4" || before == "missing" || dragged != "dragged" || !recaptured || shrinkDragged != "dragged" || afterShrink == beforeShrink || afterShrink == "missing" || midHidden != "true" || midMask != "true" {
		selftestFail("SELFTEST resize: checks=FAILED\n")
		a.cancelRegion()
		selftestExit(1)
		return
	}
	a.cancelRegion()
	time.Sleep(300 * time.Millisecond)
	selftestExit(0)
}

// selftestWatermark drives the tiled watermark: the tool toggles a repeated
// watermark layer, its settings row edits text and opacity live, toggling it
// off restores the untouched capture, and copies include the watermark.
func (a *App) selftestWatermark() {
	if a.main != nil {
		a.main.Show()
	}
	time.Sleep(1200 * time.Millisecond)
	annotating := false
	for attempt := 1; attempt <= 2 && !annotating; attempt++ {
		if err := a.beginRegion(); err != nil {
			selftestFail("SELFTEST watermark: begin error %v\n", err)
			break
		}
		time.Sleep(1200 * time.Millisecond)
		if err := a.dragOverlaySelection(80, 90, 520, 380); err != nil {
			selftestFail("SELFTEST watermark: drag error %v\n", err)
		}
		if err := a.waitOverlayAnnotating(10 * time.Second); err == nil {
			annotating = true
			break
		}
		selftestFail("SELFTEST watermark: annotating attempt %d failed\n", attempt)
		a.cancelRegion()
		time.Sleep(800 * time.Millisecond)
	}
	if !annotating {
		selftestFail("SELFTEST watermark: checks=FAILED\n")
	}
	eval := func(js string) string {
		return strings.TrimSpace(fmt.Sprint(a.overlayEval(js)))
	}
	// Restore watermark defaults inside the temporary test session. Selftest
	// windows share an ephemeral store, separate from the user's app.
	savedWatermark := ""
	if a.main != nil {
		savedWatermark = fmt.Sprint(selftestValue(a.main.Page().Eval("localStorage.getItem('pinstill.watermark') ?? ''")))
	}
	restoreWatermark := func() {
		if a.main == nil {
			return
		}
		if savedWatermark == "" {
			selftestValue(a.main.Page().Eval("localStorage.removeItem('pinstill.watermark')"))
			return
		}
		selftestValue(a.main.Page().Eval(fmt.Sprintf("localStorage.setItem('pinstill.watermark', %q)", savedWatermark)))
	}
	canvasHash := `(() => {
		const c = document.getElementById('overlayCanvas');
		if (!c) return 'missing';
		const s = c.toDataURL();
		let h = 0;
		for (let i = 0; i < s.length; i++) h = (h * 31 + s.charCodeAt(i)) >>> 0;
		return String(h);
	})()`
	baseHash := eval(canvasHash)
	eval(`document.querySelector('[data-tool="watermark"]')?.click(), 'ok'`)
	time.Sleep(450 * time.Millisecond)
	shown := eval(`String(!document.getElementById('overlayWatermark').hidden)`)
	renderedHash := eval(canvasHash)
	defaultText := eval(`document.getElementById('watermarkText')?.value ?? ''`)
	defaultOpacity := eval(`document.getElementById('watermarkOpacityValue')?.textContent ?? ''`)
	tileCount := eval(`(() => {
		// The watermark is tiled: proof that more than one copy is drawn is that
		// the layer covers rows outside a single text line; report the canvas
		// height as a sanity value.
		const c = document.getElementById('overlayCanvas');
		return c ? String(c.height) : '0';
	})()`)
	eval(`(() => { const t = document.getElementById('watermarkText'); if (!t) return; t.value = (t.value || 'Pinstill') + ' ·'; t.dispatchEvent(new Event('input', { bubbles: true })); })()`)
	eval(`(() => { const o = document.getElementById('watermarkOpacity'); if (!o) return; o.value = '45'; o.dispatchEvent(new Event('input', { bubbles: true })); })()`)
	time.Sleep(350 * time.Millisecond)
	editedHash := eval(canvasHash)
	opacityLabel := eval(`document.getElementById('watermarkOpacityValue')?.textContent ?? ''`)
	eval(`document.querySelector('[data-tool="watermark"]')?.click(), 'ok'`)
	time.Sleep(350 * time.Millisecond)
	hiddenAfter := eval(`String(document.getElementById('overlayWatermark').hidden)`)
	offHash := eval(canvasHash)
	fmt.Printf("SELFTEST watermark: shown=%s default_text=%q default_opacity=%s height=%s\n", shown, defaultText, defaultOpacity, tileCount)
	fmt.Printf("SELFTEST watermark: rendered=%v edited=%v opacity=%s off_hidden=%s restored=%v\n", renderedHash != baseHash, editedHash != renderedHash, opacityLabel, hiddenAfter, offHash == baseHash)
	ok := shown == "true" && renderedHash != baseHash && editedHash != renderedHash && opacityLabel == "45%" && hiddenAfter == "true" && offHash == baseHash
	if !ok {
		restoreWatermark()
		selftestFail("SELFTEST watermark: checks=FAILED\n")
		a.cancelRegion()
		selftestExit(1)
		return
	}
	// Copy with the watermark enabled: the exported image must include it.
	mygo.Clipboard.Clear()
	eval(`document.querySelector('[data-tool="watermark"]')?.click(), 'ok'`)
	time.Sleep(300 * time.Millisecond)
	eval(`document.querySelector('[data-action="copy"]')?.click(), 'ok'`)
	time.Sleep(1800 * time.Millisecond)
	img := mygo.Clipboard.ReadImage()
	width, height := imageDimensions(img)
	fmt.Printf("SELFTEST watermark: clipboard=%dx%d\n", width, height)
	restoreWatermark()
	if width == 0 || height == 0 {
		selftestFail("SELFTEST watermark: checks=FAILED\n")
	}
	selftestExit(0)
}

// selftestResizeStress repeats grow re-captures the way a user refines the
// region, to catch recapture stalls (the "正在截取…" spinner must always go away).
func (a *App) selftestResizeStress() {
	if a.main != nil {
		a.main.Show()
	}
	time.Sleep(1200 * time.Millisecond)
	annotating := false
	for attempt := 1; attempt <= 3 && !annotating; attempt++ {
		if err := a.beginRegion(); err != nil {
			selftestFail("SELFTEST resizestress: begin error %v\n", err)
			break
		}
		time.Sleep(1200 * time.Millisecond)
		if err := a.dragOverlaySelection(80, 90, 400, 300); err != nil {
			selftestFail("SELFTEST resizestress: drag error %v\n", err)
		}
		if err := a.waitOverlayAnnotating(10 * time.Second); err == nil {
			annotating = true
			break
		}
		selftestFail("SELFTEST resizestress: annotating attempt %d failed\n", attempt)
		a.cancelRegion()
		time.Sleep(900 * time.Millisecond)
	}
	if !annotating {
		selftestFail("SELFTEST resizestress: checks=FAILED\n")
	}
	eval := func(js string) string {
		return strings.TrimSpace(fmt.Sprint(a.overlayEval(js)))
	}
	failures := 0
	for round := 1; round <= 5; round++ {
		delta := 40 + round*10
		eval(fmt.Sprintf(`(() => {
			const h = document.querySelector('[data-resize="se"]');
			if (!h) return 'missing';
			const r = h.getBoundingClientRect();
			const cx = r.left + r.width / 2, cy = r.top + r.height / 2;
			h.dispatchEvent(new PointerEvent('pointerdown', { bubbles: true, cancelable: true, clientX: cx, clientY: cy, pointerId: 20 + %d, pointerType: 'mouse', button: 0 }));
			window.dispatchEvent(new PointerEvent('pointermove', { bubbles: true, cancelable: true, clientX: cx + %d, clientY: cy + %d, pointerId: 20 + %d, pointerType: 'mouse' }));
			window.dispatchEvent(new PointerEvent('pointerup', { bubbles: true, cancelable: true, clientX: cx + %d, clientY: cy + %d, pointerId: 20 + %d, pointerType: 'mouse', button: 0 }));
			return 'ok';
		})()`, round, delta, delta/2, round, delta, delta/2, round))
		start := time.Now()
		recovered := false
		deadline := time.Now().Add(8 * time.Second)
		for time.Now().Before(deadline) {
			spinner := eval(`String(!document.getElementById('prepareState')?.hidden)`)
			toolbar := eval(`String(!document.getElementById('annotationToolbar')?.hidden)`)
			if spinner == "false" && toolbar == "true" {
				recovered = true
				break
			}
			time.Sleep(120 * time.Millisecond)
		}
		ms := time.Since(start).Milliseconds()
		fmt.Printf("SELFTEST resizestress: round=%d recovered=%v ms=%d\n", round, recovered, ms)
		if !recovered {
			failures++
			break
		}
		time.Sleep(250 * time.Millisecond)
	}
	if failures > 0 {
		selftestFail("SELFTEST resizestress: checks=FAILED\n")
		a.cancelRegion()
		selftestExit(1)
		return
	}
	a.cancelRegion()
	time.Sleep(300 * time.Millisecond)
	selftestExit(0)
}

// selftestHold keeps the app running so its native shell can be inspected
// from outside (menu bar icon, status item menus).
func (a *App) selftestHold() {
	fmt.Printf("SELFTEST hold: READY pid=%d\n", os.Getpid())
	time.Sleep(25 * time.Second)
	selftestExit(0)
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
	selftestCheckError(err)
	time.Sleep(1500 * time.Millisecond)
	visible := a.overlay != nil && a.overlay.IsVisible()
	fmt.Printf("SELFTEST hotkeypress: overlay visible after synthetic press=%v\n", visible)
	selftestRequire(mygo.GlobalShortcut.IsRegistered(captureKey) && visible, "synthetic hotkey was not registered or delivered")
	if visible {
		a.cancelRegion()
		time.Sleep(400 * time.Millisecond)
	}
	selftestExit(0)
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
	selftestSettingsErrors(a.applySettings(a.settingsSnapshot()))
	time.Sleep(150 * time.Millisecond)
	for _, key := range []string{"captureKey", "pinKey", "ocrKey"} {
		a.mu.Lock()
		acc := a.shortcutKeys[key]
		a.mu.Unlock()
		fmt.Printf("SELFTEST hotkeys: %s %q registered=%v\n", key, acc, mygo.GlobalShortcut.IsRegistered(acc))
		selftestRequire(acc != "" && mygo.GlobalShortcut.IsRegistered(acc), "shortcut %s is not registered", key)
	}
	selftestExit(0)
}

func (a *App) selftestSettings() {
	errs := a.applySettings(DefaultSettings())
	fmt.Printf("SELFTEST settings: apply errors=%v\n", errs)
	selftestSettingsErrors(errs)
	a.mu.Lock()
	keys := a.shortcutKeys
	a.mu.Unlock()
	for key, acc := range keys {
		fmt.Printf("SELFTEST settings: %s=%s\n", key, acc)
	}
	selftestExit(0)
}

// selftestPerms drives the main window into Settings > Permissions and checks
// the permission center contract from plan R7.1: seven categories with
// data-settings-cat, three live status lines, the six permission actions and
// the banner actions.
func (a *App) selftestPerms() {
	if a.main == nil {
		selftestFail("SELFTEST perms: no main window\n")
	}
	a.showMain()
	time.Sleep(1500 * time.Millisecond)
	eval := func(js string) string {
		v, err := a.main.Page().Eval(js)
		if err != nil {
			selftestFail("SELFTEST perms: eval error %v\n", err)
			return ""
		}
		return fmt.Sprint(v)
	}
	// Settings view, then the Permissions category (both are page clicks so
	// the test proves wiring, not just markup).
	eval(`document.querySelector('[data-view="settings"]')?.click(), 'ok'`)
	time.Sleep(700 * time.Millisecond)
	eval(`document.querySelector('#settingsNav button[data-settings-cat="permissions"]')?.click(), 'ok'`)
	time.Sleep(900 * time.Millisecond)

	categories := eval(`String(document.querySelectorAll('#settingsNav button[data-settings-cat]').length)`)
	status := func(id string) string {
		return strings.TrimSpace(eval(`String(document.getElementById('` + id + `')?.textContent ?? '')`))
	}
	screen := status("permScreenStatus")
	accessibility := status("permAccessibilityStatus")
	login := status("permLaunchStatus")
	actions := eval(`[...document.querySelectorAll('[data-perm-action]')].map((e) => e.dataset.permAction).join(',')`)
	bannerActions := eval(`[...document.querySelectorAll('[data-banner-action]')].map((e) => e.dataset.bannerAction).join(',')`)
	bannerExists := eval(`String(!!document.getElementById('permissionBanner'))`)
	bannerHidden := eval(`String(document.getElementById('permissionBanner')?.hidden ?? false)`)

	fmt.Printf("SELFTEST perms: categories=%s\n", categories)
	fmt.Printf("SELFTEST perms: screen=%q accessibility=%q login=%q\n", screen, accessibility, login)
	fmt.Printf("SELFTEST perms: actions=%s\n", actions)
	fmt.Printf("SELFTEST perms: banner=%s hidden=%s banner_actions=%s\n", bannerExists, bannerHidden, bannerActions)

	ok := true
	failCheck := func(format string, args ...any) {
		ok = false
		selftestFail(format, args...)
	}
	hasStatusToken := func(text string) bool {
		return strings.Contains(text, "已授权") || strings.Contains(text, "未授权") || strings.Contains(text, "浏览器预览")
	}
	if categories != "7" {
		failCheck("want 7 settings categories, got %q", categories)
	}
	if !hasStatusToken(screen) {
		failCheck("screen status %q has no known token", screen)
	}
	if !hasStatusToken(accessibility) {
		failCheck("accessibility status %q has no known token", accessibility)
	}
	if login == "" {
		failCheck("login status is empty")
	}
	actionSet := strings.Split(actions, ",")
	for _, want := range []string{"screen-request", "screen-open", "screen-check", "accessibility-open", "accessibility-check", "login-open"} {
		found := false
		for _, got := range actionSet {
			if got == want {
				found = true
				break
			}
		}
		if !found {
			failCheck("missing permission action %q (got %q)", want, actions)
		}
	}
	if bannerExists != "true" {
		failCheck("permission banner element missing")
	}
	// The banner must agree with the live screen-permission state (NIT-2 from
	// the R7 review: it was printed but not asserted). The page starts a fresh
	// session, so nothing has been dismissed yet.
	if strings.Contains(screen, "已授权") && bannerHidden != "true" {
		failCheck("banner should be hidden when screen recording is granted (hidden=%q)", bannerHidden)
	}
	if strings.Contains(screen, "未授权") && bannerHidden != "false" {
		failCheck("banner should be visible when screen recording is missing (hidden=%q)", bannerHidden)
	}
	for _, want := range []string{"grant", "settings", "dismiss"} {
		found := false
		for _, got := range strings.Split(bannerActions, ",") {
			if got == want {
				found = true
				break
			}
		}
		if !found {
			failCheck("missing banner action %q (got %q)", want, bannerActions)
		}
	}
	if ok {
		selftestExit(0)
		return
	}
	selftestFail("SELFTEST perms: checks=FAILED\n")
	selftestExit(1)
}

// selftestPalette drives the R7.8 in-app colour palette panel: the overlay
// toolbar opens it, a preset applies its colour and updates the hex/preview,
// Esc closes only the panel (the annotation stays), the close button works,
// and the main editor exposes the same panel on a delivered capture.
func (a *App) selftestPalette() {
	ok := true
	failCheck := func(format string, args ...any) {
		ok = false
		selftestFail(format, args...)
	}
	evalOverlay := func(js string) string {
		v := a.overlayEval(js)
		return strings.TrimSpace(fmt.Sprint(v))
	}
	evalMain := func(js string) string {
		if a.main == nil {
			return ""
		}
		v, err := a.main.Page().Eval(js)
		if err != nil {
			selftestFail("eval: %v", err)
			return ""
		}
		return strings.TrimSpace(fmt.Sprint(v))
	}

	// 1. Overlay: drag a selection, get into the annotating state.
	annotated := false
	for attempt := 1; attempt <= 2 && !annotated; attempt++ {
		if err := a.beginRegion(); err != nil {
			selftestFail("SELFTEST palette: begin error %v\n", err)
			break
		}
		time.Sleep(1200 * time.Millisecond)
		if err := a.dragOverlaySelection(80, 90, 420, 320); err != nil {
			selftestFail("SELFTEST palette: drag error %v\n", err)
		}
		if err := a.waitOverlayAnnotating(10 * time.Second); err == nil {
			annotated = true
			break
		}
		failCheck("annotating attempt %d failed", attempt)
		a.cancelRegion()
		time.Sleep(800 * time.Millisecond)
	}
	if !annotated {
		selftestFail("SELFTEST palette: checks=FAILED\n")
	}

	// 2. Open the panel from the overlay toolbar.
	evalOverlay(`document.querySelector('[data-palette-toggle]')?.click(), 'ok'`)
	time.Sleep(500 * time.Millisecond)
	panelVisible := evalOverlay(`String((() => { const p = document.getElementById('colorPalettePanel'); return !!p && !p.hidden && p.offsetParent !== null; })())`)
	presets := evalOverlay(`String(document.querySelectorAll('#colorPalettePanel button[data-palette-color]').length)`)
	hasControls := evalOverlay(`String(!!document.getElementById('paletteSV') && !!document.getElementById('paletteHue') && !!document.getElementById('paletteHex') && !!document.getElementById('palettePreview'))`)
	// Hover tooltips must be active by default (regression: the init order once
	// added "no-tooltips" to every toolbar).
	tooltips := evalOverlay(`String((() => { const t = document.getElementById('annotationToolbar'); const tip = document.querySelector('.tool-btn .tip'); const delay = getComputedStyle(document.documentElement).getPropertyValue('--tooltip-delay').trim(); return !!t && !t.classList.contains('no-tooltips') && !!tip && delay !== '' && delay !== 'undefinedms'; })())`)
	fmt.Printf("SELFTEST palette: open=%s presets=%s controls=%s tooltips=%s\n", panelVisible, presets, hasControls, tooltips)
	if panelVisible != "true" {
		failCheck("overlay palette panel did not open")
	}
	if n, err := strconv.Atoi(presets); err != nil || n < 32 {
		failCheck("want >= 32 preset colours, got %q", presets)
	}
	if tooltips != "true" {
		failCheck("toolbar hover tooltips are disabled by default")
	}
	if hasControls != "true" {
		failCheck("palette controls (#paletteSV/#paletteHue/#paletteHex/#palettePreview) missing")
	}

	// 3. Pick the first preset: the hex field and the preview must follow.
	picked := evalOverlay(`(() => { const b = document.querySelector('#colorPalettePanel button[data-palette-color]'); if (!b) return ''; const v = b.dataset.paletteColor; b.click(); return v; })()`)
	time.Sleep(400 * time.Millisecond)
	pickedHex := strings.ToLower(strings.TrimSpace(picked))
	hexValue := strings.ToLower(evalOverlay(`String(document.getElementById('paletteHex')?.value ?? '')`))
	previewBg := evalOverlay(`String(getComputedStyle(document.getElementById('palettePreview')).backgroundColor)`)
	panelStillOpen := evalOverlay(`String(!document.getElementById('colorPalettePanel')?.hidden)`)
	fmt.Printf("SELFTEST palette: picked=%s hex=%s preview=%s panel_open=%s\n", pickedHex, hexValue, previewBg, panelStillOpen)
	if pickedHex == "" || hexValue != pickedHex {
		failCheck("hex field did not follow the picked colour (picked %q, hex %q)", pickedHex, hexValue)
	}
	if previewBg == "" || previewBg == "rgba(0, 0, 0, 0)" || previewBg == "transparent" {
		failCheck("palette preview did not update (background %q)", previewBg)
	}
	if panelStillOpen != "true" {
		failCheck("panel closed after picking a colour")
	}

	// 4. Esc closes the panel only: the annotation toolbar must stay.
	evalOverlay(`document.body.dispatchEvent(new KeyboardEvent('keydown', { key: 'Escape', bubbles: true, cancelable: true })), 'ok'`)
	time.Sleep(400 * time.Millisecond)
	panelAfterEsc := evalOverlay(`String(!!document.getElementById('colorPalettePanel') && !document.getElementById('colorPalettePanel').hidden)`)
	toolbarStillShown := evalOverlay(`String((() => { const t = document.getElementById('annotationToolbar'); const c = document.getElementById('overlayCanvas'); return !!t && !t.hidden && !!c && !c.hidden; })())`)
	fmt.Printf("SELFTEST palette: esc_panel_open=%s esc_kept_annotating=%s overlay_visible=%v\n", panelAfterEsc, toolbarStillShown, a.overlayVisible())
	if panelAfterEsc != "false" {
		failCheck("Esc did not close the palette panel")
	}
	if toolbarStillShown != "true" || !a.overlayVisible() || a.overlay == nil || !a.overlay.IsVisible() {
		failCheck("Esc closed the capture instead of only the palette")
	}

	// 5. Reopen and close with the close button; the capture must survive.
	evalOverlay(`document.querySelector('[data-palette-toggle]')?.click(), 'ok'`)
	time.Sleep(400 * time.Millisecond)
	reopened := evalOverlay(`String(!document.getElementById('colorPalettePanel')?.hidden)`)
	evalOverlay(`document.querySelector('[data-palette-close]')?.click(), 'ok'`)
	time.Sleep(400 * time.Millisecond)
	closedByButton := evalOverlay(`String(!!document.getElementById('colorPalettePanel') && !document.getElementById('colorPalettePanel').hidden)`)
	stillAnnotating := evalOverlay(`String((() => { const t = document.getElementById('annotationToolbar'); return !!t && !t.hidden; })())`)
	fmt.Printf("SELFTEST palette: reopen=%s close_button_open=%s still_annotating=%s\n", reopened, closedByButton, stillAnnotating)
	if reopened != "true" {
		failCheck("palette panel did not reopen")
	}
	if closedByButton != "false" {
		failCheck("close button did not hide the palette panel")
	}
	if stillAnnotating != "true" {
		failCheck("closing the palette ended the annotation")
	}
	a.cancelRegion()
	time.Sleep(900 * time.Millisecond)

	// 6. The main window no longer has an editor, so it must not expose a
	// palette toggle or panel: the palette lives on the overlay only.
	if a.main == nil {
		failCheck("no main window")
	} else {
		a.showMain()
		time.Sleep(1200 * time.Millisecond)
		toggles := evalMain(`String(document.querySelectorAll('[data-palette-toggle]').length)`)
		panels := evalMain(`String(document.querySelectorAll('#colorPalettePanel').length)`)
		fmt.Printf("SELFTEST palette: main_toggles=%s main_panels=%s\n", toggles, panels)
		if toggles != "0" || panels != "0" {
			failCheck("main window still exposes the palette; the editor should be gone")
		}
	}

	if ok {
		selftestExit(0)
		return
	}
	selftestFail("SELFTEST palette: checks=FAILED\n")
	selftestExit(1)
}
