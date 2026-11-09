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

	"github.com/echoVic/Tupen/internal/capture"
	"github.com/echoVic/Tupen/internal/store"
	"github.com/egoist/mygo"
)

// App owns the desktop windows and the capture workflow.
type App struct {
	mu sync.Mutex

	historyDir string
	sessionDir string
	shotBase   string
	store      *store.Store

	main           *mygo.Window
	overlay        *mygo.Window
	overlayDisplay mygo.Display
	overlayRect    mygo.Rectangle

	pins   map[string]*mygo.Window
	pinSeq int

	tray         *mygo.Tray
	hotkeysFired int
	settings     Settings
	shortcutKeys map[string]string
	capturing    bool
	quitting     bool
}

// New creates the app. Bind must be called before App.Run, Start once the
// application is ready.
func New() *App {
	return &App{
		settings:     DefaultSettings(),
		pins:         map[string]*mygo.Window{},
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
		log.Printf("tupen: storage: %v", err)
	}
	a.registerProtocol()
	a.createOverlay()
	a.createMainWindow()
	a.setMenu()
	a.createTray()
	if errs := a.registerShortcuts(DefaultSettings()); len(errs) > 0 {
		log.Printf("tupen: default shortcuts: %v", errs)
	}
	// History pruning waits for ApplySettings: the page owns the user's
	// retention settings, and pruning with the defaults first could delete
	// captures the user asked to keep.
	mygo.App.OnBeforeQuit(func(*mygo.QuitEvent) {
		a.mu.Lock()
		a.quitting = true
		a.mu.Unlock()
	})
	mygo.App.OnActivate(func(hasVisibleWindows bool) {
		if !hasVisibleWindows {
			a.showMain()
		}
	})
	if mode := os.Getenv("TUPEN_SELFTEST"); mode != "" {
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
	st, err := store.Open(a.historyDir)
	if err != nil {
		return err
	}
	a.store = st
	a.shotBase = "shot://localhost"
	if runtime.GOOS == "windows" {
		// WebView2 serves custom schemes from http://<scheme>.localhost/.
		a.shotBase = "http://shot.localhost"
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
	if err := mygo.Protocol.Handle("shot", mux); err != nil {
		log.Printf("tupen: protocol: %v", err)
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
		Title:           "Tupen",
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
		Title:             "Tupen 选区",
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
		{Label: "显示主窗口", Click: func(*mygo.MenuItem, *mygo.Window) { go a.showMain() }},
		{Type: mygo.MenuItemSeparator},
		{Role: mygo.RoleQuit},
	})
	tray, err := mygo.NewTray(mygo.TrayOptions{
		ID:             "tupen",
		Icon:           trayIconPNG(),
		IconIsTemplate: true,
		ToolTip:        "Tupen 截图与标注",
		Menu:           menu,
	})
	if err != nil {
		log.Printf("tupen: tray: %v", err)
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
			log.Printf("tupen: disable login item: %v", err)
		}
	}
	a.mu.Lock()
	keep := s.KeepHistory
	a.mu.Unlock()
	if keep {
		a.pruneHistory()
	} else if a.store != nil {
		if err := a.store.Clear(); err != nil {
			log.Printf("tupen: clear history: %v", err)
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
		log.Printf("tupen: prune history: %v", err)
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
		mainWin.Hide()
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
	a.mu.Unlock()
	if overlay != nil {
		overlay.Hide()
	}
	a.showMain()
	a.mu.Lock()
	a.capturing = false
	a.mu.Unlock()
	emitToMain(a, EventCaptureCancelled, CaptureCancelledPayload{Reason: "user"})
}

// finishRegion maps an overlay-local selection to screen coordinates and
// starts the capture.
func (a *App) finishRegion(rect Rect) error {
	a.mu.Lock()
	disp := a.overlayDisplay
	overlay := a.overlay
	origin := a.overlayRect
	a.mu.Unlock()
	// The overlay's real frame, not the one asked for: AppKit may constrain
	// a panel to the screen's visible frame. Read it while the window still
	// shows, since it is the page's coordinate origin.
	if overlay != nil {
		if live := overlay.ContentBounds(); live.Width > 0 && live.Height > 0 {
			origin = live
		}
		overlay.Hide()
	}
	region := capture.Rect{
		X:      float64(origin.X) + rect.X,
		Y:      float64(origin.Y) + rect.Y,
		Width:  rect.Width,
		Height: rect.Height,
	}
	region = clampToDisplay(region, disp.Bounds)
	if region.Empty() || region.Width < 2 || region.Height < 2 {
		a.cancelRegion()
		return nil
	}
	go a.captureDeliver(region, "region")
	return nil
}

// captureFullScreen captures the whole display under the pointer.
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
	if mainWin != nil {
		mainWin.Hide()
	}
	disp := mygo.Screen.DisplayNearestPoint(mygo.Screen.CursorScreenPoint())
	b := disp.Bounds
	region := capture.Rect{X: float64(b.X), Y: float64(b.Y), Width: float64(b.Width), Height: float64(b.Height)}
	go a.captureDeliver(region, "screen")
}

// captureDeliver captures the region, stores it, and hands it to the
// editor. It runs on its own goroutine.
func (a *App) captureDeliver(region capture.Rect, source string) {
	// Let the overlay, the tray menu and the main window leave the
	// compositor before the screen is grabbed.
	time.Sleep(250 * time.Millisecond)
	s := a.settingsSnapshot()
	pngBytes, err := capture.CaptureRegion(region, capture.CaptureOptions{IncludeCursor: s.IncludeCursor})
	if err != nil {
		a.failCapture(err, false)
		return
	}
	if blankPNG(pngBytes) {
		a.failCapture(errors.New("截图内容为空，可能缺少屏幕录制权限"), true)
		return
	}
	a.deliverPNG(pngBytes, source)
}

// deliverPNG stores an image and hands it to the editor. It runs after the
// capture itself, and selftests reuse it to deliver generated images.
func (a *App) deliverPNG(pngBytes []byte, source string) {
	s := a.settingsSnapshot()
	width, height := imageDimensions(pngBytes)
	if width == 0 || height == 0 {
		a.failCapture(errors.New("截图数据无效"), false)
		return
	}
	id := captureID(time.Now())
	sessionFile := id + ".png"
	if err := os.WriteFile(filepath.Join(a.sessionDir, sessionFile), pngBytes, 0o644); err != nil {
		a.failCapture(fmt.Errorf("截图写入失败: %w", err), false)
		return
	}
	payload := CaptureCompletedPayload{
		ID:     id,
		URL:    a.shotURL("/s/" + sessionFile),
		Width:  width,
		Height: height,
		Source: source,
	}
	if s.KeepHistory && a.store != nil {
		if entry, err := a.store.Save(pngBytes, time.Now()); err != nil {
			log.Printf("tupen: save history: %v", err)
		} else {
			payload.HistoryID = entry.ID
			payload.ThumbURL = a.shotURL("/h/" + entry.Thumb)
			a.emitHistoryChanged()
		}
	}
	a.playSoundIfEnabled(s)
	a.showMain()
	emitToMain(a, EventCaptureCompleted, payload)
	switch s.AfterCapture {
	case "clipboard":
		if err := mygo.Clipboard.WriteImage(pngBytes); err != nil {
			a.toast("写入剪贴板失败: "+err.Error(), "error")
		} else {
			a.toast("截图已复制到剪贴板", "success")
		}
	case "pin":
		if _, err := a.pinPNG(pngBytes, width, height); err != nil {
			a.toast("固定到屏幕失败: "+err.Error(), "error")
		}
	}
	a.mu.Lock()
	a.capturing = false
	a.mu.Unlock()
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
		message += "。请在系统设置中允许 Tupen 录制屏幕"
	}
	a.showMain()
	a.mu.Lock()
	a.capturing = false
	a.mu.Unlock()
	emitToMain(a, EventCaptureFailed, CaptureFailedPayload{Message: message, Permission: permission})
	a.toast(message, "error")
}

// showMain reveals and focuses the main window.
func (a *App) showMain() {
	a.mu.Lock()
	win := a.main
	a.mu.Unlock()
	if win == nil || win.IsDestroyed() {
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

// pinPNG writes the PNG for a pin window and opens the always-on-top pin.
func (a *App) pinPNG(pngBytes []byte, width, height int) (string, error) {
	if width == 0 || height == 0 {
		width, height = imageDimensions(pngBytes)
	}
	if width == 0 || height == 0 {
		return "", errors.New("贴图数据无效")
	}
	s := a.settingsSnapshot()
	a.mu.Lock()
	a.pinSeq++
	id := fmt.Sprintf("pin-%d", a.pinSeq)
	a.mu.Unlock()
	file := id + ".png"
	if err := os.WriteFile(filepath.Join(a.sessionDir, file), pngBytes, 0o644); err != nil {
		return "", fmt.Errorf("贴图文件写入失败: %w", err)
	}
	disp := mygo.Screen.DisplayNearestPoint(mygo.Screen.CursorScreenPoint())
	scale := disp.ScaleFactor
	if scale <= 0 {
		scale = 1
	}
	maxW, maxH := 640.0, 720.0
	widthDip := float64(width) / scale
	heightDip := float64(height) / scale
	if widthDip > maxW {
		ratio := maxW / widthDip
		widthDip = maxW
		heightDip *= ratio
	}
	if heightDip > maxH {
		ratio := maxH / heightDip
		heightDip = maxH
		widthDip *= ratio
	}
	x := disp.WorkArea.X + disp.WorkArea.Width/2 - int(widthDip)/2
	y := disp.WorkArea.Y + disp.WorkArea.Height/2 - int(heightDip)/2
	offset := ((a.pinSeq - 1) % 6) * 24
	x += offset
	y += offset
	opacity := s.PinOpacity
	if opacity <= 0 {
		opacity = 100
	}
	win := mygo.NewWindow(mygo.WindowOptions{
		Title:             "Tupen 贴图",
		Width:             int(widthDip),
		Height:            int(heightDip),
		X:                 x,
		Y:                 y,
		Panel:             true,
		Frameless:         true,
		Hidden:            true,
		DisableResize:     true,
		DisableMinimize:   true,
		DisableMaximize:   true,
		DisableFullScreen: true,
		BackgroundColor:   "#f2eee4",
		URL:               "/pin.html",
	})
	win.SetOpacity(float64(opacity) / 100)
	win.OnClosed(func() {
		a.mu.Lock()
		delete(a.pins, id)
		a.mu.Unlock()
	})
	win.OnReadyToShow(func() { win.Show() })
	a.mu.Lock()
	a.pins[id] = win
	a.mu.Unlock()
	_ = EventPinInit.Emit(win, PinInitPayload{
		ID:      id,
		URL:     a.shotURL("/s/" + file),
		Width:   widthDip,
		Height:  heightDip,
		Opacity: float64(opacity),
		Border:  s.PinBorder,
	})
	return id, nil
}

func (a *App) closePin(id string) error {
	a.mu.Lock()
	win := a.pins[id]
	a.mu.Unlock()
	if win == nil {
		return fmt.Errorf("贴图 %s 不存在", id)
	}
	win.Close()
	return nil
}

// pinClipboard pins the image currently on the clipboard.
func (a *App) pinClipboard() (string, error) {
	pngBytes := mygo.Clipboard.ReadImage()
	if len(pngBytes) == 0 {
		return "", errors.New("剪贴板中没有图片")
	}
	return a.pinPNG(pngBytes, 0, 0)
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
