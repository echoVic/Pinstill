package app

import (
	"bytes"
	"errors"
	"fmt"
	"image/png"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"

	"github.com/echoVic/Pinstill/internal/capture"
	"github.com/echoVic/Pinstill/internal/pin"
	"github.com/echoVic/Pinstill/internal/store"
	"github.com/egoist/mygo"
)

// App owns the desktop windows and the capture workflow.
type App struct {
	mu sync.Mutex

	historyDir string
	sessionDir string
	shotBase   string
	store      *store.Store
	pinStore   *pin.Store

	main           *mygo.Window
	overlay        *mygo.Window
	control        *mygo.Window
	scrollFrame    *mygo.Window
	result         *mygo.Window
	overlayDisplay mygo.Display
	overlayRect    mygo.Rectangle

	// resultPNG/resultMeta back the scrolling-capture preview window.
	resultPNG  []byte
	resultMeta ScrollResultPayload

	pins      map[string]*mygo.Window
	pinStates map[string]pin.State
	pinTimers map[string]*time.Timer
	pinSeq    int

	scroll   *scrollSession
	scrollMu sync.Mutex

	tray         *mygo.Tray
	hotkeysFired int
	settings     Settings
	shortcutKeys map[string]string
	capturing    bool
	quitting     bool

	// restoreMainAfterCapture is true when the history/settings window was
	// visible before the capture: only then may cancel/failure reopen it.
	restoreMainAfterCapture bool
}

// New creates the app. Bind must be called before App.Run, Start once the
// application is ready.
func New() *App {
	return &App{
		settings:     DefaultSettings(),
		pins:         map[string]*mygo.Window{},
		pinStates:    map[string]pin.State{},
		pinTimers:    map[string]*time.Timer{},
		shortcutKeys: map[string]string{},
	}
}

// Bind registers the services pages call.
func (a *App) Bind() {
	mygo.BindAs("Capture", &CaptureService{app: a})
	mygo.BindAs("History", &HistoryService{app: a})
}

// Start opens the app's windows and native integrations. Call it from
// App.WhenReady.
func (a *App) Start() {
	if err := a.initStorage(); err != nil {
		log.Printf("pinstill: storage: %v", err)
	}
	a.registerProtocol()
	a.createOverlay()
	a.createMainWindow()
	a.createScrollControl()
	a.createScrollFrame()
	a.setMenu()
	a.createTray()
	if errs := a.registerShortcuts(DefaultSettings()); len(errs) > 0 {
		log.Printf("pinstill: default shortcuts: %v", errs)
	}
	a.restorePins()
	// History pruning waits for ApplySettings: the page owns the user's
	// retention settings, and pruning with the defaults first could delete
	// captures the user asked to keep.
	mygo.App.OnBeforeQuit(func(*mygo.QuitEvent) {
		a.mu.Lock()
		a.quitting = true
		session := a.scroll
		a.mu.Unlock()
		if session != nil {
			session.mu.Lock()
			if !session.stopped {
				session.stopped = true
				close(session.stop)
			}
			session.mu.Unlock()
		}
		a.snapshotAllPins()
	})
	// Activating the app (clicking the menu bar icon, the Dock, ...) must not
	// open the history/settings window: only the tray menu item, the second
	// launch path and explicit buttons in the UI do that.
	if mode := os.Getenv("PINSTILL_SELFTEST"); mode != "" {
		go a.runSelfTest(mode)
	}
}

// ShowMain reveals the main window; it is what a second launch does.
func (a *App) ShowMain() { a.showMain() }

func (a *App) initStorage() error {
	userData, err := mygo.App.Path(mygo.PathUserData)
	if err != nil {
		return err
	}
	cacheDir, err := mygo.App.Path(mygo.PathCache)
	if err != nil {
		return err
	}
	// Development and the screenshot selftests can point every store at an
	// isolated directory, so they never read or write the user's captures.
	if dir := strings.TrimSpace(os.Getenv("PINSTILL_DATA_DIR")); dir != "" {
		userData = dir
		cacheDir = dir
	}
	a.shotBase = "shot://localhost"
	if runtime.GOOS == "windows" {
		// WebView2 serves custom schemes from http://<scheme>.localhost/.
		a.shotBase = "http://shot.localhost"
	}
	a.historyDir = filepath.Join(userData, "captures")
	a.sessionDir = filepath.Join(cacheDir, "session")
	if err := os.RemoveAll(a.sessionDir); err != nil {
		return err
	}
	if err := os.MkdirAll(a.sessionDir, 0o755); err != nil {
		return err
	}
	if err := os.MkdirAll(a.historyDir, 0o755); err != nil {
		return err
	}
	// A failed store only disables its feature: the app keeps capturing.
	if st, err := store.Open(a.historyDir); err != nil {
		log.Printf("pinstill: history store: %v", err)
	} else {
		a.store = st
	}
	if pinStore, err := pin.Open(filepath.Join(userData, "pins")); err != nil {
		log.Printf("pinstill: pin store: %v", err)
	} else {
		a.pinStore = pinStore
	}
	return nil
}

// shotURL resolves a path under the captures protocol handler.
func (a *App) shotURL(path string) string {
	if path == "" {
		return ""
	}
	return a.shotBase + "/" + strings.TrimPrefix(path, "/")
}

func (a *App) registerProtocol() {
	mux := http.NewServeMux()
	if a.store != nil {
		mux.Handle("/h/", http.StripPrefix("/h/", imagesOnly(mygo.FileServer(os.DirFS(a.store.Dir())))))
	}
	mux.Handle("/s/", http.StripPrefix("/s/", imagesOnly(mygo.FileServer(os.DirFS(a.sessionDir)))))
	if a.pinStore != nil {
		mux.Handle("/p/", http.StripPrefix("/p/", imagesOnly(mygo.FileServer(os.DirFS(a.pinStore.Dir())))))
	}
	if err := mygo.Protocol.Handle("shot", mux); err != nil {
		log.Printf("pinstill: protocol: %v", err)
	}
}

// imagesOnly keeps the captures protocol to the pictures it serves: the
// history index and anything else in the directories stay out of reach of
// the pages.
func imagesOnly(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		path := strings.ToLower(r.URL.Path)
		if !strings.HasSuffix(path, ".png") && !strings.HasSuffix(path, ".jpg") && !strings.HasSuffix(path, ".jpeg") {
			http.NotFound(w, r)
			return
		}
		next.ServeHTTP(w, r)
	})
}

func (a *App) createMainWindow() {
	win := mygo.NewWindow(mygo.WindowOptions{
		Title:           "Pinstill",
		Width:           1360,
		Height:          880,
		MinWidth:        820,
		MinHeight:       560,
		TitleBarStyle:   mygo.TitleBarHiddenInset,
		TitleBarHeight:  52,
		BackgroundColor: "#141412",
		URL:             "/",
	})
	win.OnClose(func(e *mygo.CloseEvent) {
		a.mu.Lock()
		quitting := a.quitting
		a.mu.Unlock()
		if quitting {
			return
		}
		// A screenshot utility lives in the menu bar: closing its window
		// puts it away instead of quitting.
		e.PreventDefault()
		win.Hide()
	})
	a.main = win
}

func (a *App) createOverlay() {
	win := mygo.NewWindow(mygo.WindowOptions{
		Title:             "Pinstill 选区",
		Width:             960,
		Height:            600,
		Hidden:            true,
		Frameless:         true,
		Transparent:       true,
		Panel:             true,
		AlwaysOnTop:       true,
		DisableResize:     true,
		DisableMinimize:   true,
		DisableMaximize:   true,
		DisableClose:      true,
		DisableFullScreen: true,
		BackgroundColor:   "rgba(0,0,0,0)",
		URL:               "/overlay.html",
	})
	win.OnClose(func(e *mygo.CloseEvent) {
		a.mu.Lock()
		quitting := a.quitting
		a.mu.Unlock()
		if quitting {
			return
		}
		e.PreventDefault()
		go a.cancelRegion()
	})
	a.overlay = win
}

func (a *App) createTray() {
	menu := mygo.NewMenu([]*mygo.MenuItem{
		{Label: "截取区域", Click: func(*mygo.MenuItem, *mygo.Window) { go a.beginRegion() }},
		{Label: "截取整个屏幕", Click: func(*mygo.MenuItem, *mygo.Window) { go a.captureFullScreen() }},
		{Label: "固定剪贴板图片", Click: func(*mygo.MenuItem, *mygo.Window) {
			go func() {
				if _, err := a.pinClipboard(); err != nil {
					a.toast(err.Error(), "error")
				}
			}()
		}},
		{Type: mygo.MenuItemSeparator},
		{Label: "历史与设置", Click: func(*mygo.MenuItem, *mygo.Window) { go a.showMain() }},
		{Type: mygo.MenuItemSeparator},
		{Role: mygo.RoleQuit},
	})
	tray, err := mygo.NewTray(mygo.TrayOptions{
		ID:             "pinstill",
		Icon:           trayIconPNG(),
		IconIsTemplate: true,
		ToolTip:        "Pinstill 截图与标注",
		Menu:           menu,
	})
	if err != nil {
		log.Printf("pinstill: tray: %v", err)
		return
	}
	a.mu.Lock()
	a.tray = tray
	a.mu.Unlock()
}

func (a *App) setMenu() {
	screenshotItems := []*mygo.MenuItem{
		{Label: "截取区域", Click: func(*mygo.MenuItem, *mygo.Window) { go a.beginRegion() }},
		{Label: "截取整个屏幕", Click: func(*mygo.MenuItem, *mygo.Window) { go a.captureFullScreen() }},
		{Label: "固定剪贴板图片", Click: func(*mygo.MenuItem, *mygo.Window) {
			go func() {
				if _, err := a.pinClipboard(); err != nil {
					a.toast(err.Error(), "error")
				}
			}()
		}},
	}
	menu := mygo.NewMenu([]*mygo.MenuItem{
		{Role: mygo.RoleAppMenu},
		{Label: "文件", Submenu: append(screenshotItems,
			&mygo.MenuItem{Type: mygo.MenuItemSeparator},
			&mygo.MenuItem{Role: mygo.RoleClose},
			&mygo.MenuItem{Type: mygo.MenuItemSeparator},
			&mygo.MenuItem{Role: mygo.RoleQuit},
		)},
		{Role: mygo.RoleEditMenu},
		{Role: mygo.RoleWindowMenu},
	})
	mygo.App.SetMenu(menu)
}

// settingsSnapshot returns the current settings.
func (a *App) settingsSnapshot() Settings {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.settings
}

// registerShortcuts replaces the app's global shortcuts with the ones in s
// and returns the failures keyed by setting name.
func (a *App) registerShortcuts(s Settings) map[string]string {
	a.mu.Lock()
	old := a.shortcutKeys
	a.shortcutKeys = map[string]string{}
	a.mu.Unlock()
	for _, acc := range old {
		mygo.GlobalShortcut.Unregister(acc)
	}
	errs := map[string]string{}
	add := func(key, accelerator string, fn func()) {
		accelerator = strings.TrimSpace(accelerator)
		if accelerator == "" {
			return
		}
		if err := mygo.GlobalShortcut.Register(accelerator, fn); err != nil {
			errs[key] = err.Error()
			return
		}
		a.mu.Lock()
		a.shortcutKeys[key] = accelerator
		a.mu.Unlock()
	}
	add("captureKey", s.CaptureKey, func() {
		a.mu.Lock()
		a.hotkeysFired++
		a.mu.Unlock()
		go a.beginRegion()
	})
	add("pinKey", s.PinKey, func() {
		go func() {
			if _, err := a.pinClipboard(); err != nil {
				a.toast(err.Error(), "error")
			}
		}()
	})
	add("ocrKey", s.OcrKey, func() { go a.toast("文字识别服务待接入，可先使用截图与标注", "info") })
	return errs
}

// applySettings stores the settings and applies everything native.
func (a *App) applySettings(s Settings) map[string]string {
	a.mu.Lock()
	a.settings = s
	a.mu.Unlock()
	errs := a.registerShortcuts(s)
	if mygo.App.OpenAtLogin() != s.LaunchAtLogin {
		if err := mygo.App.SetOpenAtLogin(s.LaunchAtLogin); err != nil && s.LaunchAtLogin {
			errs["launchAtLogin"] = err.Error()
		} else if err != nil {
			log.Printf("pinstill: disable login item: %v", err)
		}
	}
	a.mu.Lock()
	keep := s.KeepHistory
	a.mu.Unlock()
	if keep {
		a.pruneHistory()
	} else if a.store != nil {
		if err := a.store.Clear(); err != nil {
			log.Printf("pinstill: clear history: %v", err)
		}
		a.emitHistoryChanged()
	}
	return errs
}

func (a *App) pruneHistory() {
	if a.store == nil {
		return
	}
	s := a.settingsSnapshot()
	if !s.KeepHistory {
		return
	}
	limit := int64(s.HistoryLimitMB) * 1024 * 1024
	if _, err := a.store.Prune(s.RetentionDays, limit); err != nil {
		log.Printf("pinstill: prune history: %v", err)
	}
}

// beginRegion hides the main window and shows the selection overlay on the
// display under the pointer.
func (a *App) beginRegion() error {
	a.mu.Lock()
	if a.capturing {
		a.mu.Unlock()
		return nil
	}
	a.capturing = true
	a.mu.Unlock()

	if !capture.Supported() {
		a.mu.Lock()
		a.capturing = false
		a.mu.Unlock()
		a.failCapture(capture.ErrUnsupported, false)
		return capture.ErrUnsupported
	}
	disp := mygo.Screen.DisplayNearestPoint(mygo.Screen.CursorScreenPoint())
	a.mu.Lock()
	a.overlayDisplay = disp
	mainWin := a.main
	overlay := a.overlay
	a.mu.Unlock()
	if mainWin != nil {
		// Restore the main window after a cancel only when it was visible
		// before the capture: closing the window and cancelling a shot must
		// not pop the history/settings panel back up.
		visible := hideForCapture(mainWin)
		a.mu.Lock()
		a.restoreMainAfterCapture = visible
		a.mu.Unlock()
	}
	if overlay == nil {
		a.mu.Lock()
		a.capturing = false
		a.mu.Unlock()
		return errors.New("选区窗口不可用")
	}
	// A panel is a titled window underneath: AppKit keeps its frame inside
	// the screen's visible frame, which excludes the menu bar and the Dock.
	// Ask for the work area and read back the frame the window really got,
	// so the selection maps to the right pixels either way.
	area := disp.WorkArea
	if area.Width <= 0 || area.Height <= 0 {
		area = disp.Bounds
	}
	overlay.SetBounds(mygo.Rectangle{X: area.X, Y: area.Y, Width: area.Width, Height: area.Height})
	overlay.Show()
	actual := overlay.ContentBounds()
	if actual.Width <= 0 || actual.Height <= 0 {
		actual = area
	}
	a.mu.Lock()
	a.overlayRect = actual
	a.mu.Unlock()
	_ = EventOverlayShown.Emit(overlay, a.overlayInfo())
	return nil
}

// overlayInfo describes the display the overlay currently covers.
func (a *App) overlayInfo() OverlayInfo {
	a.mu.Lock()
	disp := a.overlayDisplay
	rect := a.overlayRect
	overlay := a.overlay
	a.mu.Unlock()
	if overlay != nil {
		if live := overlay.ContentBounds(); live.Width > 0 && live.Height > 0 {
			rect = live
		}
	}
	if disp.WorkArea.Width == 0 {
		disp = mygo.Screen.PrimaryDisplay()
	}
	if rect.Width <= 0 || rect.Height <= 0 {
		rect = disp.WorkArea
		if rect.Width <= 0 || rect.Height <= 0 {
			rect = disp.Bounds
		}
	}
	return OverlayInfo{
		DisplayX: rect.X,
		DisplayY: rect.Y,
		Width:    rect.Width,
		Height:   rect.Height,
		Scale:    disp.ScaleFactor,
	}
}

// cancelRegion hides the overlay and brings the main window back.
func (a *App) cancelRegion() {
	a.mu.Lock()
	overlay := a.overlay
	restoreMain := a.restoreMainAfterCapture
	a.restoreMainAfterCapture = false
	a.capturing = false
	a.mu.Unlock()
	if overlay != nil {
		overlay.Hide()
	}
	if restoreMain {
		a.showMain()
	}
	emitToMain(a, EventCaptureCancelled, CaptureCancelledPayload{Reason: "user"})
}

// captureRegion wraps the native capture. Selftests can run without the
// screen recording permission by asking for a generated image instead.
func (a *App) captureRegion(region capture.Rect, opts capture.CaptureOptions) ([]byte, error) {
	if os.Getenv("PINSTILL_SELFTEST_SCROLL_SIM") != "" {
		if pngBytes := simulatedScrollFrame(); len(pngBytes) > 0 {
			return pngBytes, nil
		}
	}
	if os.Getenv("PINSTILL_SELFTEST_FAKE_CAPTURE") != "" {
		if pngBytes := syntheticScreenshot(); len(pngBytes) > 0 {
			return pngBytes, nil
		}
	}
	return capture.CaptureRegion(region, opts)
}

// prepareSelection captures the user's selection and hands the image back to
// the overlay, which then annotates it in place on the desktop. The overlay
// stays on screen: the page hides the selection chrome and the selection's
// interior is transparent, so the capture sees the screen behind it. This
// keeps the flow flicker-free.
func (a *App) prepareSelection(rect Rect) (OverlayCapturePayload, error) {
	return a.prepareSelectionWithDelay(rect, 90*time.Millisecond)
}

// prepareSelectionFast is used when the page already hid its chrome and waited
// for a frame: it only keeps a short compositor delay so the re-captured image
// shows up quickly after a region adjustment.
func (a *App) prepareSelectionFast(rect Rect) (OverlayCapturePayload, error) {
	return a.prepareSelectionWithDelay(rect, 20*time.Millisecond)
}

func (a *App) prepareSelectionWithDelay(rect Rect, chromeDelay time.Duration) (OverlayCapturePayload, error) {
	a.mu.Lock()
	disp := a.overlayDisplay
	origin := a.overlayRect
	capturing := a.capturing
	overlay := a.overlay
	a.mu.Unlock()
	if !capturing {
		return OverlayCapturePayload{}, errors.New("没有进行中的截图")
	}
	if overlay != nil {
		if live := overlay.ContentBounds(); live.Width > 0 && live.Height > 0 {
			origin = live
		}
	}
	region := capture.Rect{
		X:      float64(origin.X) + rect.X,
		Y:      float64(origin.Y) + rect.Y,
		Width:  rect.Width,
		Height: rect.Height,
	}
	region = clampToDisplay(region, disp.Bounds)
	if region.Empty() || region.Width < 2 || region.Height < 2 {
		return OverlayCapturePayload{}, errors.New("选区太小")
	}
	// Give the page a moment to hide its selection border and badges so they
	// are not part of the capture.
	time.Sleep(chromeDelay)
	s := a.settingsSnapshot()
	pngBytes, err := a.captureRegion(region, capture.CaptureOptions{IncludeCursor: s.IncludeCursor})
	if err != nil {
		a.failSelection(err, false)
		return OverlayCapturePayload{}, err
	}
	if blankPNG(pngBytes) {
		err := errors.New("截图内容为空，可能缺少屏幕录制权限")
		a.failSelection(err, true)
		return OverlayCapturePayload{}, err
	}
	width, height := imageDimensions(pngBytes)
	if width == 0 || height == 0 {
		err := errors.New("截图数据无效")
		a.failSelection(err, false)
		return OverlayCapturePayload{}, err
	}
	id := captureID(time.Now())
	sessionFile := id + ".png"
	if err := os.WriteFile(filepath.Join(a.sessionDir, sessionFile), pngBytes, 0o644); err != nil {
		err = fmt.Errorf("截图写入失败: %w", err)
		a.failSelection(err, false)
		return OverlayCapturePayload{}, err
	}
	payload := OverlayCapturePayload{
		ID:     id,
		URL:    a.shotURL("/s/" + sessionFile),
		Width:  width,
		Height: height,
		Rect:   rect,
	}
	if overlay != nil {
		emitToWindow(a, overlay, EventOverlayCapture, payload)
	}
	return payload, nil
}

// failSelection reports a failed in-place capture to the overlay, which stays
// on screen so the user can retry or cancel it (unlike a failed whole-region
// capture, nothing opens the main window).
func (a *App) failSelection(err error, permission bool) {
	message := "截图失败"
	if err != nil {
		message = err.Error()
	}
	if permission {
		message = "截图内容为空，可能缺少屏幕录制权限。请在系统设置 → 隐私与安全性 → 屏幕录制中允许 Pinstill"
	}
	a.mu.Lock()
	a.capturing = false
	overlay := a.overlay
	a.mu.Unlock()
	emitToWindow(a, overlay, EventCaptureFailed, CaptureFailedPayload{Message: message, Permission: permission})
}

// finishAnnotated completes the in-place annotation: the page sends the
// composited PNG and the action the user picked.
func (a *App) finishAnnotated(action string, dataBase64 string) error {
	a.mu.Lock()
	overlay := a.overlay
	a.mu.Unlock()
	if overlay != nil {
		overlay.Hide()
	}
	reset := func() {
		a.mu.Lock()
		a.capturing = false
		a.restoreMainAfterCapture = false
		a.mu.Unlock()
	}
	if action == "cancel" {
		reset()
		emitToMain(a, EventCaptureCancelled, CaptureCancelledPayload{Reason: "user"})
		return nil
	}
	switch action {
	case "copy", "pin", "save":
	default:
		reset()
		return fmt.Errorf("未知的标注动作 %q", action)
	}
	raw, err := decodeImageBase64(dataBase64)
	if err != nil {
		log.Printf("pinstill: finishAnnotated %s: bad base64 (%d chars): %v", action, len(dataBase64), err)
		reset()
		a.toast("标注数据无法解码: "+err.Error(), "error")
		return err
	}
	if _, err := png.Decode(bytes.NewReader(raw)); err != nil {
		reset()
		err = errors.New("标注后的图片不是有效 PNG")
		a.toast(err.Error(), "error")
		return err
	}
	payload, err := a.storeCapture(raw)
	if err != nil {
		reset()
		a.toast(err.Error(), "error")
		return err
	}
	a.applyCaptureAction(action, raw, payload)
	reset()
	return nil
}

// captureFullScreen captures the whole display under the pointer, stores it
// in history and runs the configured after-capture action. There is no
// overlay and no editor, so "inplace" falls back to the clipboard.
func (a *App) captureFullScreen() {
	a.mu.Lock()
	if a.capturing {
		a.mu.Unlock()
		return
	}
	a.capturing = true
	mainWin := a.main
	a.mu.Unlock()
	if !capture.Supported() {
		a.failCapture(capture.ErrUnsupported, false)
		return
	}
	// Keep the app's own windows out of the shot; reopening the window is
	// only for the case it was visible when the user asked for the capture.
	mainVisible := mainWin != nil && hideForCapture(mainWin)
	a.mu.Lock()
	a.restoreMainAfterCapture = mainVisible
	a.mu.Unlock()
	disp := mygo.Screen.DisplayNearestPoint(mygo.Screen.CursorScreenPoint())
	b := disp.Bounds
	region := capture.Rect{X: float64(b.X), Y: float64(b.Y), Width: float64(b.Width), Height: float64(b.Height)}
	go a.captureFullScreenAction(region, mainVisible)
}

// captureFullScreenAction grabs the display after the app windows left the
// compositor, stores the capture and runs the after-capture action.
func (a *App) captureFullScreenAction(region capture.Rect, restoreMain bool) {
	time.Sleep(250 * time.Millisecond)
	s := a.settingsSnapshot()
	pngBytes, err := a.captureRegion(region, capture.CaptureOptions{IncludeCursor: s.IncludeCursor})
	if err != nil {
		a.failCapture(err, false)
		return
	}
	if blankPNG(pngBytes) {
		a.failCapture(errors.New("截图内容为空，可能缺少屏幕录制权限"), true)
		return
	}
	payload, err := a.storeCapture(pngBytes)
	if restoreMain {
		a.showMain()
	}
	a.mu.Lock()
	a.restoreMainAfterCapture = false
	a.mu.Unlock()
	if err != nil {
		a.failCapture(err, false)
		return
	}
	a.playSoundIfEnabled(s)
	if s.AfterCapture == "pin" {
		if _, err := a.pinPNG(pngBytes, payload.Width, payload.Height); err != nil {
			a.toast("固定到屏幕失败: "+err.Error(), "error")
		} else {
			a.toast("整屏截图已固定到屏幕", "success")
		}
	} else if err := mygo.Clipboard.WriteImage(pngBytes); err != nil {
		a.toast("写入剪贴板失败: "+err.Error(), "error")
	} else {
		a.toast("整屏截图已复制到剪贴板", "success")
	}
	a.mu.Lock()
	a.capturing = false
	a.mu.Unlock()
}

// storeCapture writes a capture to the session cache and, when the user
// keeps history, to the persistent history. It is shared by the overlay
// actions, full-screen captures and the selftests.
func (a *App) storeCapture(pngBytes []byte) (capturePayload, error) {
	s := a.settingsSnapshot()
	width, height := imageDimensions(pngBytes)
	if width == 0 || height == 0 {
		return capturePayload{}, errors.New("截图数据无效")
	}
	id := captureID(time.Now())
	sessionFile := id + ".png"
	if err := os.WriteFile(filepath.Join(a.sessionDir, sessionFile), pngBytes, 0o644); err != nil {
		return capturePayload{}, fmt.Errorf("截图写入失败: %w", err)
	}
	payload := capturePayload{ID: id, Width: width, Height: height}
	if s.KeepHistory && a.store != nil {
		if _, err := a.store.Save(pngBytes, time.Now()); err != nil {
			log.Printf("pinstill: save history: %v", err)
		} else {
			a.emitHistoryChanged()
		}
	}
	return payload, nil
}

// historyPNG loads a stored capture's original PNG for the history actions.
func (a *App) historyPNG(id string) ([]byte, error) {
	if a.store == nil {
		return nil, errors.New("历史记录不可用")
	}
	return a.store.ReadPNG(id)
}

func (a *App) playSoundIfEnabled(s Settings) {
	if s.Sound {
		playCaptureSound()
	}
}

// failCapture reports a failed capture and brings the main window back.
func (a *App) failCapture(err error, permission bool) {
	message := "截图失败"
	if err != nil {
		message = err.Error()
	}
	if permission {
		message += "。请在系统设置中允许 Pinstill 录制屏幕"
	}
	a.mu.Lock()
	restoreMain := a.restoreMainAfterCapture
	a.restoreMainAfterCapture = false
	a.capturing = false
	a.mu.Unlock()
	if restoreMain {
		a.showMain()
	} else {
		// The window stays closed; a notification is the only visible place
		// for the failure of an explicit capture action.
		a.notifyCaptureFailure(message)
	}
	emitToMain(a, EventCaptureFailed, CaptureFailedPayload{Message: message, Permission: permission})
	a.toast(message, "error")
}

// notifyCaptureFailure reports a capture failure without opening the main
// window (which stays closed unless the user asks for it).
func (a *App) notifyCaptureFailure(message string) {
	notification := mygo.NewNotification(mygo.NotificationOptions{
		ID:    "pinstill-capture-failure",
		Title: "Pinstill 截图失败",
		Body:  message,
	})
	go func() {
		if err := notification.Show(); err != nil {
			log.Printf("pinstill: notification: %v", err)
		}
	}()
}

// showMain reveals and focuses the main window.
// Minimized and hidden windows already stay out of the capture. Leaving them
// alone preserves their presentation without showing/focusing them afterward.
func hideForCapture(win interface {
	IsVisible() bool
	IsMinimized() bool
	Hide()
}) bool {
	if !win.IsVisible() || win.IsMinimized() {
		return false
	}
	win.Hide()
	return true
}

func (a *App) showMain() {
	a.mu.Lock()
	win := a.main
	quitting := a.quitting
	a.mu.Unlock()
	if win == nil || win.IsDestroyed() || quitting {
		return
	}
	mygo.App.Focus()
	win.Show()
	win.Focus()
}

func emitToMain[T any](a *App, event *mygo.Event[T], payload T) {
	a.mu.Lock()
	win := a.main
	a.mu.Unlock()
	emitToWindow(a, win, event, payload)
}

// emitToWindow sends an event to one window, if it still exists.
func emitToWindow[T any](a *App, win *mygo.Window, event *mygo.Event[T], payload T) {
	if win == nil || win.IsDestroyed() {
		return
	}
	_ = event.Emit(win, payload)
}

func (a *App) emitHistoryChanged() {
	count := 0
	if a.store != nil {
		count = len(a.store.List())
	}
	emitToMain(a, EventHistoryChanged, HistoryChangedPayload{Count: count})
}

func (a *App) toast(text, kind string) {
	emitToMain(a, EventToast, ToastPayload{Text: text, Kind: kind})
}

// clampToDisplay keeps a screen rectangle inside the display's bounds.
func clampToDisplay(r capture.Rect, b mygo.Rectangle) capture.Rect {
	x0 := max(r.X, float64(b.X))
	y0 := max(r.Y, float64(b.Y))
	x1 := min(r.X+r.Width, float64(b.X+b.Width))
	y1 := min(r.Y+r.Height, float64(b.Y+b.Height))
	if x1 <= x0 || y1 <= y0 {
		return capture.Rect{}
	}
	return capture.Rect{X: x0, Y: y0, Width: x1 - x0, Height: y1 - y0}
}

// copyImagePNG writes a PNG to the system clipboard.
func (a *App) copyImagePNG(pngBytes []byte) error {
	if _, err := png.Decode(bytes.NewReader(pngBytes)); err != nil {
		return errors.New("剪贴板数据不是有效 PNG")
	}
	return mygo.Clipboard.WriteImage(pngBytes)
}

// historyEntries lists persisted captures, newest first.
func (a *App) historyEntries() []HistoryEntry {
	s := a.settingsSnapshot()
	if !s.KeepHistory || a.store == nil {
		return []HistoryEntry{}
	}
	entries := a.store.List()
	out := make([]HistoryEntry, 0, len(entries))
	for _, e := range entries {
		out = append(out, HistoryEntry{
			ID:        e.ID,
			CreatedAt: e.CreatedAt.Format(time.RFC3339),
			Width:     e.Width,
			Height:    e.Height,
			Size:      e.Size,
			URL:       a.shotURL("/h/" + e.File),
			ThumbURL:  a.shotURL("/h/" + e.Thumb),
		})
	}
	return out
}

func (a *App) historyCapture(id string) (HistoryCapture, error) {
	if a.store == nil {
		return HistoryCapture{}, errors.New("历史记录不可用")
	}
	entry, ok := a.store.Get(id)
	if !ok {
		return HistoryCapture{}, errors.New("截图不存在")
	}
	return HistoryCapture{
		ID:     entry.ID,
		Width:  entry.Width,
		Height: entry.Height,
		URL:    a.shotURL("/h/" + entry.File),
	}, nil
}

func (a *App) removeHistory(id string) error {
	if a.store == nil {
		return errors.New("历史记录不可用")
	}
	if err := a.store.Delete(id); err != nil {
		return err
	}
	a.emitHistoryChanged()
	return nil
}

func (a *App) clearHistory() error {
	if a.store == nil {
		return errors.New("历史记录不可用")
	}
	if err := a.store.Clear(); err != nil {
		return err
	}
	a.emitHistoryChanged()
	return nil
}
