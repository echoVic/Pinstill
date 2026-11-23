package app

import (
	"context"
	"errors"
	"fmt"
	"image"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/echoVic/Pinstill/internal/capture"
	"github.com/echoVic/Pinstill/internal/clothesline"
	"github.com/echoVic/Pinstill/internal/imagebudget"
	"github.com/echoVic/Pinstill/internal/pin"
	"github.com/echoVic/Pinstill/internal/store"
	"github.com/egoist/mygo"
	"github.com/egoist/mygo/plugins/updater"
)

// App owns the desktop windows and the capture workflow.
type App struct {
	mu         sync.Mutex
	settingsMu sync.Mutex
	finishMu   sync.Mutex

	historyDir  string
	sessionDir  string
	shotBase    string
	store       *store.Store
	pinStore    *pin.Store
	pageSession *mygo.Session

	main               *mygo.Window
	overlay            *mygo.Window
	control            *mygo.Window
	scrollFrame        *mygo.Window
	result             *mygo.Window
	overlayDisplay     mygo.Display
	overlayRect        mygo.Rectangle
	overlayWindows     []OverlayWindow
	overlayCursorX     float64
	overlayCursorY     float64
	overlayHasCursor   bool
	overlaySession     uint64
	overlayRevealTimer *time.Timer
	overlayPool        []*mygo.Window
	overlaySurfaces    []overlaySurface
	overlayInteraction uint64
	overlayDesktop     *capture.DesktopSnapshot
	overlayFreeze      context.CancelFunc
	overlaySource      *mygo.Window
	overlayCommitted   bool
	setOverlayOpacity  func(float64)
	overlayUI          func(func())
	visibleWindows     func(excludePID int) []capture.WindowInfo

	// resultPNG/resultMeta back the scrolling-capture preview window.
	resultPNG           []byte
	resultMeta          ScrollResultPayload
	resultSessionFile   string
	overlaySessionFile  string
	pendingPreviewFiles []string
	annotationDigest    [32]byte
	annotationPayload   capturePayload
	captureAction       func(string, []byte, capturePayload) (CaptureActionResult, error)

	pins       map[string]*mygo.Window
	pinStates  map[string]pin.State
	pinTimers  map[string]*time.Timer
	pinSeq     int
	pinManager *mygo.Window

	lineStore  *clothesline.Store
	lineWindow *mygo.Window
	lineReady  bool
	lineState  clothesline.State

	scroll         *scrollSession
	scrollMu       sync.Mutex
	scrollSequence uint64
	scrollUI       func(func()) // optional synthetic dispatcher; native transitions run on the UI thread

	tray         *mygo.Tray
	hotkeysFired int
	settings     Settings
	shortcutKeys map[string]string
	capturing    bool
	quitting     bool
	// Invalidates delayed selection work when its request or session ends.
	captureGeneration uint64

	// restoreMainAfterCapture tracks whether a whole-screen capture hid the workbench.
	restoreMainAfterCapture bool
}

// New creates the app. Bind must be called before App.Run, Start once the
// application is ready.
func New() *App {
	a := &App{
		settings:     DefaultSettings(),
		pins:         map[string]*mygo.Window{},
		pinStates:    map[string]pin.State{},
		pinTimers:    map[string]*time.Timer{},
		shortcutKeys: map[string]string{},
	}
	if os.Getenv("PINSTILL_SELFTEST") != "" {
		// All test windows share preferences with each other, but never with
		// the installed app's persistent WebView data store.
		a.pageSession = mygo.NewEphemeralSession()
	}
	return a
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
		mygo.App.Exit(1)
		return
	}
	a.registerProtocol()
	a.createOverlay()
	a.createMainWindow()
	mygo.Screen.OnDisplaysChanged(func() {
		a.mu.Lock()
		active := a.overlayInteraction != 0 || a.overlayFreeze != nil
		a.mu.Unlock()
		if active {
			a.cancelRegion()
		}
		if a.lineWindow != nil && !a.lineWindow.IsDestroyed() {
			a.lineWindow.SetBounds(lineBounds(mygo.Screen.PrimaryDisplay()))
		}
	})
	a.createScrollControl()
	a.createScrollFrame()
	a.setMenu()
	a.createTray()
	if errs := a.registerShortcuts(DefaultSettings()); len(errs) > 0 {
		log.Printf("pinstill: default shortcuts: %v", errs)
	}
	a.restorePins()
	a.restoreClothesline()
	// History pruning waits for ApplySettings: the page owns the user's
	// retention settings, and pruning with the defaults first could delete
	// captures the user asked to keep.
	mygo.App.OnBeforeQuit(func(e *mygo.QuitEvent) {
		if err := a.prepareQuit(a.snapshotAllPins); err != nil {
			e.PreventDefault()
			a.toast("贴图状态保存失败，退出已取消: "+err.Error(), "error")
		}
	})
	// Activating the app (clicking the menu bar icon, the Dock, ...) must not
	// open the history/settings window: only the tray menu item, the second
	// launch path and explicit buttons in the UI do that.
	if mode := os.Getenv("PINSTILL_SELFTEST"); mode != "" {
		go a.runSelfTest(mode)
	}
}

func (a *App) prepareQuit(snapshot func() error) error {
	a.mu.Lock()
	a.quitting = true
	a.mu.Unlock()
	if err := snapshot(); err != nil {
		a.mu.Lock()
		a.quitting = false
		a.mu.Unlock()
		return err
	}
	a.mu.Lock()
	session := a.scroll
	a.captureGeneration++
	a.clearOverlayFreezeLocked()
	a.clearOverlayRevealLocked()
	a.mu.Unlock()
	if session != nil {
		session.mu.Lock()
		if !session.stopped {
			session.stopped = true
			close(session.stop)
		}
		session.mu.Unlock()
	}
	return nil
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
	if lineStore, err := clothesline.Open(filepath.Join(userData, "clothesline")); err != nil {
		log.Printf("pinstill: clothesline: %v", err)
	} else {
		a.lineStore = lineStore
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
	mux.HandleFunc("/d/", a.serveOverlayBackdrop)
	if a.pinStore != nil {
		mux.Handle("/p/", http.StripPrefix("/p/", imagesOnly(mygo.FileServer(os.DirFS(a.pinStore.Dir())))))
	}
	if a.lineStore != nil {
		mux.Handle("/l/", http.StripPrefix("/l/", imagesOnly(mygo.FileServer(os.DirFS(a.lineStore.Dir())))))
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
		Page:            mygo.PageOptions{Session: a.pageSession},
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
	a.overlay = a.newOverlayWindow()
	a.overlayPool = []*mygo.Window{a.overlay}
}

func (a *App) newOverlayWindow() *mygo.Window {
	win := mygo.NewWindow(mygo.WindowOptions{
		Page:              mygo.PageOptions{Session: a.pageSession},
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
	configureCaptureOverlay(win)
	win.SetContentProtection(true)
	win.SetHasShadow(false)
	return win
}

func (a *App) createTray() {
	items := []*mygo.MenuItem{
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
		{Label: "贴图管理…", Click: func(*mygo.MenuItem, *mygo.Window) { go a.showPinManager() }},
		{Label: "显示 / 隐藏桌面晾绳", Click: func(*mygo.MenuItem, *mygo.Window) { go a.toggleClothesline() }},
		{Label: "历史与设置", Click: func(*mygo.MenuItem, *mygo.Window) { go a.showMain() }},
	}
	if item := updateMenuItem(); item != nil {
		items = append(items, item)
	}
	menu := mygo.NewMenu(append(items, mygo.Separator(), &mygo.MenuItem{Role: mygo.RoleQuit}))
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
	appMenu := &mygo.MenuItem{Role: mygo.RoleAppMenu}
	if item := updateMenuItem(); item != nil {
		appMenu.Submenu = []*mygo.MenuItem{
			{Role: mygo.RoleAbout}, item, mygo.Separator(),
			{Role: mygo.RoleServices}, mygo.Separator(),
			{Role: mygo.RoleHide}, {Role: mygo.RoleHideOthers}, {Role: mygo.RoleUnhide},
			mygo.Separator(), {Role: mygo.RoleQuit},
		}
	}
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
		appMenu,
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

func updateMenuItem() *mygo.MenuItem {
	if os.Getenv("PINSTILL_SELFTEST") != "" {
		return nil
	}
	return &mygo.MenuItem{
		ID:    "check-update",
		Label: "检查更新…",
		Click: func(*mygo.MenuItem, *mygo.Window) { updater.CheckForUpdates() },
	}
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
	return a.reconcileShortcuts(s)
}

// applySettings stores the settings and applies everything native.
func (a *App) applySettings(s Settings) map[string]string {
	return a.applyEffectiveSettings(s)
}

// beginRegion hides the main window and shows the selection overlays.
func (a *App) beginRegion() error {
	a.mu.Lock()
	if a.quitting {
		a.mu.Unlock()
		return errors.New("应用正在退出")
	}
	if a.capturing {
		a.mu.Unlock()
		return nil
	}
	a.capturing = true
	a.captureGeneration++
	session := a.captureGeneration
	ctx, cancel := context.WithCancel(context.Background())
	a.overlayFreeze = cancel
	a.mu.Unlock()
	defer func() {
		cancel()
		a.mu.Lock()
		if a.captureGeneration == session {
			a.overlayFreeze = nil
		}
		a.mu.Unlock()
	}()

	if !capture.Supported() {
		a.mu.Lock()
		a.capturing = false
		a.mu.Unlock()
		a.failCapture(capture.ErrUnsupported, false)
		return capture.ErrUnsupported
	}
	cursor := mygo.Screen.CursorScreenPoint()
	displays := activeOverlayDisplays(mygo.Screen.Displays(), cursor)
	rawWindows := a.listVisibleWindows()
	var showErr error
	var hidMain bool
	a.runOverlayUI(func() {
		if !a.selectionIsCurrent(session) {
			return
		}
		a.mu.Lock()
		mainWin := a.main
		if a.overlay == nil || len(displays) == 0 {
			a.capturing = false
			a.mu.Unlock()
			showErr = errors.New("选区窗口不可用")
			return
		}
		a.mu.Unlock()

		if mainWin != nil {
			hidMain = hideForCapture(mainWin)
		}
	})
	if showErr != nil || ctx.Err() != nil {
		return showErr
	}
	desktop := a.freezeOverlayDesktop(ctx, displays, hidMain)
	a.runOverlayUI(func() {
		if !a.selectionIsCurrent(session) {
			return
		}
		a.mu.Lock()
		a.overlayFreeze = nil
		a.overlayDesktop = desktop
		a.mu.Unlock()
		a.openOverlaySurfaces(displays, rawWindows, cursor, session)
	})
	return showErr
}

// Serialize the session checks with native show, hide and reveal calls.
func (a *App) runOverlayUI(fn func()) {
	if a.overlayUI != nil {
		a.overlayUI(fn)
		return
	}
	a.mu.Lock()
	native := a.overlay != nil || a.main != nil
	a.mu.Unlock()
	if native {
		mygo.RunOnMain(fn)
	} else {
		fn()
	}
}

// Caller holds a.mu.
func (a *App) clearOverlayRevealLocked() {
	if a.overlayRevealTimer != nil {
		a.overlayRevealTimer.Stop()
		a.overlayRevealTimer = nil
	}
	a.overlaySession = 0
}

func (a *App) revealOverlay(session uint64) {
	a.revealOverlayFor(nil, session)
}

func (a *App) revealOverlayFor(caller *mygo.Window, session uint64) {
	if session == 0 {
		return
	}
	a.runOverlayUI(func() {
		a.mu.Lock()
		if !a.capturing || a.quitting || a.scroll != nil || a.overlaySession != session {
			a.mu.Unlock()
			return
		}
		if len(a.overlaySurfaces) > 0 {
			var windows []*mygo.Window
			allReady := true
			for index := range a.overlaySurfaces {
				surface := &a.overlaySurfaces[index]
				if !surface.ready && (caller == nil || caller == surface.window) {
					surface.ready = true
					windows = append(windows, surface.window)
				}
				allReady = allReady && surface.ready
			}
			if allReady {
				a.clearOverlayRevealLocked()
			}
			a.mu.Unlock()
			for _, window := range windows {
				window.SetOpacity(1)
			}
			return
		}
		a.clearOverlayRevealLocked()
		overlay := a.overlay
		setOpacity := a.setOverlayOpacity
		a.mu.Unlock()
		if setOpacity != nil {
			setOpacity(1)
			return
		}
		if overlay != nil && !overlay.IsDestroyed() {
			overlay.SetOpacity(1)
		}
	})
}

func (a *App) listVisibleWindows() []capture.WindowInfo {
	a.mu.Lock()
	fn := a.visibleWindows
	a.mu.Unlock()
	if fn == nil {
		fn = capture.VisibleWindows
	}
	return fn(os.Getpid())
}

func projectOverlayWindows(raw []capture.WindowInfo, surface capture.Rect) []OverlayWindow {
	projected := capture.ProjectWindowsToSurface(raw, surface, 12, 128)
	if len(projected) == 0 {
		return nil
	}
	out := make([]OverlayWindow, len(projected))
	for i, w := range projected {
		out[i] = OverlayWindow{
			X:      w.Rect.X,
			Y:      w.Rect.Y,
			Width:  w.Rect.Width,
			Height: w.Rect.Height,
			Owner:  w.Owner,
			Title:  w.Title,
		}
	}
	return out
}

// overlayInfo describes the display the overlay currently covers.
func (a *App) overlayInfo() OverlayInfo {
	return a.overlayInfoFor(nil)
}

func (a *App) overlayInfoFor(caller *mygo.Window) OverlayInfo {
	a.mu.Lock()
	if caller == nil {
		caller = a.overlay
	}
	for _, surface := range a.overlaySurfaces {
		if surface.window == caller {
			info := a.overlayInfoOnSurfaceLocked(surface)
			a.mu.Unlock()
			return info
		}
	}
	disp := a.overlayDisplay
	rect := a.overlayRect
	overlay := a.overlay
	windows := slices.Clone(a.overlayWindows)
	cursorX := a.overlayCursorX
	cursorY := a.overlayCursorY
	hasCursor := a.overlayHasCursor
	session := a.overlaySession
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
		Session:   session,
		DisplayX:  rect.X,
		DisplayY:  rect.Y,
		Width:     rect.Width,
		Height:    rect.Height,
		Scale:     disp.ScaleFactor,
		CursorX:   cursorX,
		CursorY:   cursorY,
		HasCursor: hasCursor,
		Windows:   windows,
	}
}

// cancelRegion closes the overlay and ends the active capture.
func (a *App) cancelRegion() bool {
	completed := false
	a.runOverlayUI(func() {
		a.mu.Lock()
		if a.scroll != nil {
			a.mu.Unlock()
			return
		}
		windows := a.overlayPanesLocked()
		session := a.overlayInteraction
		a.restoreMainAfterCapture = false
		a.capturing = false
		a.captureGeneration++
		reportCacheCleanup(a.releaseOverlayCacheLocked())
		a.mu.Unlock()
		for _, window := range windows {
			hideOverlayPane(window, session)
		}
		emitToMain(a, EventCaptureCancelled, CaptureCancelledPayload{Reason: "user"})
		completed = true
	})
	return completed
}

// captureRegion wraps the native capture. Selftests can run without the
// screen recording permission by asking for a generated image instead.
func (a *App) captureRegion(region capture.Rect, opts capture.CaptureOptions) ([]byte, error) {
	if runtime.GOOS == "darwin" && opts.BackingScale == 0 && mygo.App.IsReady() {
		for _, display := range mygo.Screen.Displays() {
			if region.Intersect(asCaptureRect(display.Bounds)) == region {
				opts.BackingScale = display.ScaleFactor
				break
			}
		}
	}
	return a.captureRegionOnDisplays(context.Background(), region, opts, nil)
}

func (a *App) captureRegionOnDisplays(ctx context.Context, region capture.Rect, opts capture.CaptureOptions, displays []capture.DisplaySurface) ([]byte, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
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
	if runtime.GOOS == "darwin" && len(displays) > 0 {
		return capture.CaptureDisplaysContext(ctx, region, opts, displays)
	}
	return capture.CaptureRegionContext(ctx, region, opts)
}

// prepareSelection hands back the selected pixels for in-place annotation.
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
	return a.prepareSelectionFrom(context.Background(), nil, rect, chromeDelay)
}

func (a *App) prepareSelectionFrom(ctx context.Context, caller *mygo.Window, rect Rect, chromeDelay time.Duration) (OverlayCapturePayload, error) {
	if caller != nil && !a.commitOverlaySurface(ctx, caller) {
		if err := ctx.Err(); err != nil {
			return OverlayCapturePayload{}, err
		}
		return OverlayCapturePayload{}, errors.New("截图请求已取消")
	}
	a.mu.Lock()
	if err := ctx.Err(); err != nil {
		a.mu.Unlock()
		return OverlayCapturePayload{}, err
	}
	if !a.capturing || a.quitting || a.scroll != nil {
		a.mu.Unlock()
		return OverlayCapturePayload{}, errors.New("没有进行中的截图")
	}
	disp := a.overlayDisplay
	origin := a.overlayRect
	overlay := a.overlay
	displays := a.captureDisplaysLocked()
	desktop := a.overlayDesktop
	a.captureGeneration++
	generation := a.captureGeneration
	a.mu.Unlock()
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
	if len(displays) > 0 {
		region = clampToDisplay(region, displayUnion(displays))
	} else {
		region = clampToDisplay(region, disp.Bounds)
	}
	if region.Empty() || region.Width < 2 || region.Height < 2 {
		return OverlayCapturePayload{}, errors.New("选区太小")
	}
	img, pngBytes, err := a.selectionImage(ctx, desktop, region, displays, generation, chromeDelay)
	if err := ctx.Err(); err != nil {
		return OverlayCapturePayload{}, err
	}
	if !a.selectionIsCurrent(generation) {
		return OverlayCapturePayload{}, errors.New("截图请求已取消")
	}
	if err != nil {
		a.failSelection(err, false)
		return OverlayCapturePayload{}, err
	}
	if blankCapture(img) {
		err := errors.New("截图内容为空，可能缺少屏幕录制权限")
		a.failSelection(err, true)
		return OverlayCapturePayload{}, err
	}
	width, height := img.Bounds().Dx(), img.Bounds().Dy()
	// Cancellation and publication must not cross: either this write belongs
	// to the active request, or no session file is created for it.
	a.mu.Lock()
	if err := ctx.Err(); err != nil {
		a.mu.Unlock()
		return OverlayCapturePayload{}, err
	}
	if !a.capturing || a.quitting || a.captureGeneration != generation {
		a.mu.Unlock()
		return OverlayCapturePayload{}, errors.New("截图请求已取消")
	}
	sessionFile, err := a.writePreviewLocked(ctx, &a.overlaySessionFile, pngBytes)
	if err == nil {
		a.annotationDigest = [32]byte{}
		a.annotationPayload = capturePayload{}
	}
	a.mu.Unlock()
	if err != nil {
		if ctx.Err() != nil {
			return OverlayCapturePayload{}, err
		}
		err = fmt.Errorf("截图写入失败: %w", err)
		a.failSelection(err, false)
		return OverlayCapturePayload{}, err
	}
	payload := OverlayCapturePayload{
		ID:     strings.TrimSuffix(sessionFile, ".png"),
		URL:    a.shotURL("/s/" + sessionFile),
		Width:  width,
		Height: height,
		Rect:   rect,
	}
	return payload, nil
}

func (a *App) selectionIsCurrent(generation uint64) bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.capturing && !a.quitting && a.captureGeneration == generation
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
	overlay := a.overlay
	a.mu.Unlock()
	emitToWindow(a, overlay, EventCaptureFailed, CaptureFailedPayload{Message: message, Permission: permission})
}

// finishAnnotated completes the in-place annotation: the page sends the
// composited PNG and the action the user picked.
func (a *App) finishAnnotated(action string, dataBase64 string) (CaptureActionResult, error) {
	return a.finishAnnotation(action, dataBase64)
}

// captureFullScreen captures the whole display under the pointer, stores it
// in history and runs the configured after-capture action. There is no
// overlay and no editor, so "inplace" falls back to the clipboard.
func (a *App) captureFullScreen() {
	a.mu.Lock()
	if a.capturing || a.quitting {
		a.mu.Unlock()
		return
	}
	a.capturing = true
	a.captureGeneration++
	generation := a.captureGeneration
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
	go a.captureFullScreenAction(region, mainVisible, generation)
}

// captureFullScreenAction grabs the display after the app windows left the
// compositor, stores the capture and runs the after-capture action.
func (a *App) captureFullScreenAction(region capture.Rect, restoreMain bool, generation uint64) {
	time.Sleep(250 * time.Millisecond)
	if !a.selectionIsCurrent(generation) {
		return
	}
	s := a.settingsSnapshot()
	pngBytes, err := a.captureRegion(region, capture.CaptureOptions{IncludeCursor: s.IncludeCursor})
	if !a.selectionIsCurrent(generation) {
		return
	}
	if err != nil {
		a.failCapture(err, false)
		return
	}
	payload, err := a.storeScreenCapture(pngBytes)
	if !a.selectionIsCurrent(generation) {
		return
	}
	if errors.Is(err, errEmptyScreenCapture) {
		a.failCapture(err, true)
		return
	}
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
	action := "copy"
	if s.AfterCapture == "pin" {
		action = "pin"
	}
	actionFn := a.captureAction
	if actionFn == nil {
		actionFn = a.applyCaptureAction
	}
	if _, err := actionFn(action, pngBytes, payload); err != nil {
		a.toast(err.Error(), "error")
	}
	a.mu.Lock()
	if a.captureGeneration == generation {
		a.capturing = false
	}
	a.mu.Unlock()
}

// storeCapture validates a capture and, when the user keeps history, writes
// it to persistent history. Preview files have their own ownership. Shared by the overlay
// actions, full-screen captures and the selftests.
func (a *App) storeCapture(pngBytes []byte) (capturePayload, error) {
	return a.storeCapturePNG(pngBytes, nil)
}

var errEmptyScreenCapture = errors.New("截图内容为空，可能缺少屏幕录制权限")

func (a *App) storeScreenCapture(data []byte) (capturePayload, error) {
	return a.storeCapturePNG(data, func(img image.Image) error {
		if blankCapture(img) {
			return errEmptyScreenCapture
		}
		return nil
	})
}

func (a *App) storeCapturePNG(pngBytes []byte, check func(image.Image) error) (capturePayload, error) {
	// Keep a history write and its retention decision atomic with settings
	// changes, especially disabling and clearing history.
	a.settingsMu.Lock()
	defer a.settingsMu.Unlock()
	s := a.settingsSnapshot()
	createdAt := time.Now()
	payload := capturePayload{ID: captureID(createdAt)}
	if s.KeepHistory {
		if a.store == nil {
			return capturePayload{}, errors.New("历史记录存储不可用，请重试或在设置中关闭历史记录")
		}
		entry, err := a.store.SaveCheckedPNG(pngBytes, createdAt, check)
		if err != nil && entry.ID == "" {
			if errors.Is(err, errEmptyScreenCapture) {
				return capturePayload{}, err
			}
			return capturePayload{}, fmt.Errorf("历史记录写入失败，请重试或关闭历史记录: %w", err)
		}
		payload.Width, payload.Height = entry.Width, entry.Height
		// A nonempty entry on error means the index was committed. Preserve
		// its identity so a retry never saves the same annotation again.
		if err != nil {
			a.toast("截图已保存，但磁盘同步失败: "+err.Error(), "error")
		}
		{
			_, pruneErr := a.store.Prune(s.RetentionDays, historyByteLimit(s.HistoryLimitMB))
			if pruneErr != nil {
				a.toast("截图已保存，但历史清理失败: "+pruneErr.Error(), "error")
			}
			if _, retained := a.store.Get(entry.ID); retained {
				payload.HistoryID = entry.ID
			}
			a.emitHistoryChanged()
		}
	} else {
		img, err := imagebudget.DecodePNG(pngBytes)
		if err != nil {
			return capturePayload{}, fmt.Errorf("截图数据无效: %w", err)
		}
		if check != nil {
			if err := check(img); err != nil {
				return capturePayload{}, err
			}
		}
		payload.Width, payload.Height = img.Bounds().Dx(), img.Bounds().Dy()
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
	if _, err := imagebudget.DecodePNG(pngBytes); err != nil {
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
