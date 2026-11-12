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
	"os"
	"sync"
	"sync/atomic"
	"time"

	"github.com/echoVic/Pinstill/internal/capture"
	"github.com/echoVic/Pinstill/internal/stitch"
	"github.com/egoist/mygo"
)

const (
	scrollFrameInterval = 340 * time.Millisecond
	scrollAutoInterval  = 420 * time.Millisecond
	scrollInitialDelay  = 260 * time.Millisecond
	scrollMaxFrames     = 120
	scrollMaxHeight     = 30000
	scrollDeltaPixels   = -220
	// scrollStableFrames is how many captures in a row have to look identical
	// before the scrolling capture finishes itself (the end of the page, or a
	// pause long enough to mean "done").
	scrollStableFrames  = 3
	scrollControlWidth  = 380
	scrollControlHeight = 64
)

// scrollSession is one in-progress scrolling capture.
type scrollSession struct {
	mu        sync.Mutex
	rect      Rect
	region    capture.Rect
	auto      bool
	paused    bool
	acc       *stitch.Accumulator
	frames    int
	height    int
	status    string
	stop      chan struct{}
	done      chan struct{}
	stopped   bool
	finalized bool
}

func (s *scrollSession) snapshot() (frames, height int, status string, auto bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.frames, s.height, s.status, s.auto
}

func (s *scrollSession) setStatus(status string) {
	s.mu.Lock()
	s.status = status
	s.mu.Unlock()
}

func (s *scrollSession) isAuto() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.auto
}

func (s *scrollSession) isPaused() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.paused
}

func (s *scrollSession) isStopped() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.stopped
}

func (s *scrollSession) isFinalized() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.finalized
}

// createScrollControl creates the small window that shows scroll progress and
// actions while the overlay is hidden.
func (a *App) createScrollControl() {
	win := mygo.NewWindow(mygo.WindowOptions{
		Title:             "Pinstill 滚动截图",
		Width:             scrollControlWidth,
		Height:            scrollControlHeight,
		Hidden:            true,
		Panel:             true,
		Frameless:         true,
		AlwaysOnTop:       true,
		DisableResize:     true,
		DisableMinimize:   true,
		DisableMaximize:   true,
		DisableFullScreen: true,
		BackgroundColor:   "#141412",
		URL:               "/scroll.html",
	})
	win.OnClose(func(e *mygo.CloseEvent) {
		a.mu.Lock()
		quitting := a.quitting
		a.mu.Unlock()
		if quitting {
			return
		}
		e.PreventDefault()
		go func() {
			if err := a.finishScroll("cancel"); err != nil {
				log.Printf("pinstill: cancel scroll: %v", err)
			}
		}()
	})
	a.control = win
}

// createScrollFrame creates the thin, click-through border that marks the
// scrolling-capture region while the overlay is hidden: the user keeps seeing
// which part of the screen is being captured.
func (a *App) createScrollFrame() {
	win := mygo.NewWindow(mygo.WindowOptions{
		Title:             "Pinstill 滚动截图范围",
		Width:             320,
		Height:            240,
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
		URL:               "/frame.html",
	})
	win.SetIgnoreMouseEvents(true)
	a.mu.Lock()
	a.scrollFrame = win
	a.mu.Unlock()
}

// scrollFrameBorder is how far outside the capture region the marker is drawn.
// The border has to stay outside the captured pixels: a frame pixel inside the
// region would end up in every stitched screenshot.
const scrollFrameBorder = 2

// showScrollFrame places the region marker just outside the capture region and
// shows it; the window stays click-through so manual scrolling still reaches
// the app underneath.
func (a *App) showScrollFrame(region capture.Rect) {
	a.mu.Lock()
	win := a.scrollFrame
	a.mu.Unlock()
	if win == nil || win.IsDestroyed() {
		return
	}
	win.SetBounds(mygo.Rectangle{
		X:      int(region.X) - scrollFrameBorder,
		Y:      int(region.Y) - scrollFrameBorder,
		Width:  int(region.Width) + scrollFrameBorder*2,
		Height: int(region.Height) + scrollFrameBorder*2,
	})
	win.SetIgnoreMouseEvents(true)
	win.Show()
}

// hideScrollFrame removes the region marker when the session ends.
func (a *App) hideScrollFrame() {
	a.mu.Lock()
	win := a.scrollFrame
	a.mu.Unlock()
	if win != nil && !win.IsDestroyed() {
		win.Hide()
	}
}

// scrollFrameSize reports the region marker's window size (selftests check that
// it is the capture region plus the 2px border on every side).
func (a *App) scrollFrameSize() (int, int) {
	a.mu.Lock()
	win := a.scrollFrame
	a.mu.Unlock()
	if win == nil || win.IsDestroyed() {
		return 0, 0
	}
	bounds := win.Bounds()
	return bounds.Width, bounds.Height
}

// scrollFrameVisible reports whether the scrolling-capture region marker is on
// screen (used by the selftests).
func (a *App) scrollFrameVisible() bool {
	a.mu.Lock()
	win := a.scrollFrame
	a.mu.Unlock()
	return win != nil && win.IsVisible()
}

// placeScrollControl puts the control bar next to the capture region, without
// covering it.
func (a *App) placeScrollControl(region capture.Rect) {
	a.mu.Lock()
	win := a.control
	a.mu.Unlock()
	if win == nil || win.IsDestroyed() {
		return
	}
	width, height := scrollControlWidth, scrollControlHeight
	display := mygo.Screen.DisplayMatching(mygo.Rectangle{
		X: int(region.X), Y: int(region.Y), Width: int(region.Width), Height: int(region.Height),
	})
	area := display.WorkArea
	if area.Width <= 0 {
		area = display.Bounds
	}
	x := int(region.X+region.Width/2) - width/2
	y := int(region.Y+region.Height) + 12
	clamp := func(v, lo, hi int) int {
		if hi < lo {
			return lo
		}
		if v < lo {
			return lo
		}
		if v > hi {
			return hi
		}
		return v
	}
	x = clamp(x, area.X, area.X+area.Width-width)
	y = clamp(y, area.Y, area.Y+area.Height-height)
	// If it would still cover the selection (small screens), move it above.
	if regionIntersects(x, y, width, height, region) {
		y = int(region.Y) - height - 12
		y = clamp(y, area.Y, area.Y+area.Height-height)
	}
	if regionIntersects(x, y, width, height, region) {
		x = area.X + 16
		y = area.Y + 16
	}
	win.SetBounds(mygo.Rectangle{X: x, Y: y, Width: width, Height: height})
}

func regionIntersects(x, y, w, h int, region capture.Rect) bool {
	return float64(x) < region.X+region.Width && float64(x+w) > region.X &&
		float64(y) < region.Y+region.Height && float64(y+h) > region.Y
}

// startScroll begins a scrolling capture for the current overlay selection.
// It hides the overlay and shows the control bar; the frames are captured
// behind it. Automatic scrolling is the default; when the platform or the
// Accessibility permission cannot deliver it the session starts in manual
// mode and the control bar offers the permission shortcut instead of failing.
func (a *App) startScroll(rect Rect, autoScroll bool) error {
	a.scrollMu.Lock()
	defer a.scrollMu.Unlock()
	a.mu.Lock()
	capturing := a.capturing
	overlay := a.overlay
	disp := a.overlayDisplay
	origin := a.overlayRect
	active := a.scroll != nil
	control := a.control
	a.mu.Unlock()
	if active {
		return errors.New("滚动截图已在进行中")
	}
	if !capturing {
		return errors.New("没有进行中的截图")
	}
	autoDenied := autoScroll && (!capture.ScrollSupported() || !capture.AccessibilityGranted())
	effectiveAuto := autoScroll && !autoDenied
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
	if region.Empty() || region.Width < 16 || region.Height < 16 {
		return errors.New("滚动截图选区太小")
	}
	if control == nil || control.IsDestroyed() {
		return errors.New("滚动控制窗口不可用")
	}
	if os.Getenv("PINSTILL_SELFTEST_SCROLL_SIM") != "" {
		simulatedScrollCounter.Store(0)
	}
	if overlay != nil {
		overlay.Hide()
	}
	session := &scrollSession{
		rect:   rect,
		region: region,
		auto:   effectiveAuto,
		acc:    stitch.New(stitch.Options{MaxHeight: scrollMaxHeight}),
		status: "running",
		stop:   make(chan struct{}),
		done:   make(chan struct{}),
	}
	a.mu.Lock()
	a.scroll = session
	a.mu.Unlock()
	a.placeScrollControl(region)
	control.Show()
	a.showScrollFrame(region)
	emitToWindow(a, control, EventScrollInit, ScrollInitPayload{
		Rect:                 rect,
		Auto:                 effectiveAuto,
		AutoDenied:           autoDenied,
		AccessibilityGranted: capture.AccessibilityGranted(),
	})
	go a.scrollLoop(session)
	return nil
}

// scrollLoop captures frames and stitches them until the content stops moving,
// a limit is reached or the user finishes/cancels. A natural end finalizes on
// its own: the stitched image opens in the result preview window.
func (a *App) scrollLoop(session *scrollSession) {
	defer close(session.done)
	time.Sleep(scrollInitialDelay)
	stable := 0
	for {
		if session.isStopped() {
			return
		}
		if session.isPaused() {
			session.setStatus("paused")
			a.emitScrollProgress(session)
			time.Sleep(150 * time.Millisecond)
			continue
		}
		if _, _, status, _ := session.snapshot(); status != "running" {
			session.setStatus("running")
			a.emitScrollProgress(session)
		}
		frame, err := a.captureRegion(session.region, capture.CaptureOptions{IncludeCursor: false})
		if err != nil {
			a.failScroll(session, "截图失败: "+err.Error())
			return
		}
		img, err := pngToRGBA(frame)
		if err != nil {
			a.failScroll(session, "截图帧解码失败: "+err.Error())
			return
		}
		static := false
		if session.frames == 0 {
			if _, err := session.acc.Add(img); err != nil {
				a.failScroll(session, "拼接失败: "+err.Error())
				return
			}
		} else {
			static = session.acc.SameAsPrevious(img)
			if static {
				stable++
			} else {
				stable = 0
				if _, err := session.acc.Add(img); err != nil {
					a.failScroll(session, "拼接失败: "+err.Error())
					return
				}
			}
		}
		session.mu.Lock()
		session.frames++
		session.height, _ = session.acc.Size()
		session.mu.Unlock()
		a.emitScrollProgress(session)
		if static && stable >= scrollStableFrames-1 {
			// The page ended (or the user paused long enough): finish like the
			// mainstream tools do, and open the preview.
			a.finalizeScroll(session, "stalled")
			return
		}
		if session.frames >= scrollMaxFrames || session.height >= scrollMaxHeight {
			a.finalizeScroll(session, "max")
			return
		}
		if session.isAuto() && capture.ScrollSupported() && os.Getenv("PINSTILL_SELFTEST_SCROLL_SIM") == "" {
			center := capture.Point{X: session.region.X + session.region.Width/2, Y: session.region.Y + session.region.Height/2}
			if err := capture.ScrollWheel(center, scrollDeltaPixels); err != nil {
				a.failScroll(session, "滚动失败: "+err.Error())
				return
			}
			time.Sleep(scrollAutoInterval)
			continue
		}
		select {
		case <-session.stop:
			return
		case <-time.After(scrollFrameInterval):
		}
	}
}

// failScroll reports a scroll session that could not continue.
func (a *App) failScroll(session *scrollSession, message string) {
	session.setStatus("error")
	a.emitScrollProgress(session)
	a.mu.Lock()
	control := a.control
	a.mu.Unlock()
	time.Sleep(700 * time.Millisecond)
	if control != nil && !control.IsDestroyed() {
		control.Hide()
	}
	a.toast(message, "error")
	a.clearScrollSession()
}

// finalizeScroll stitches, stores and previews a finished scrolling capture.
// It runs at the end of the capture loop (never for a cancel); the session is
// cleared once the preview window is up.
func (a *App) finalizeScroll(session *scrollSession, status string) {
	session.mu.Lock()
	if session.finalized || session.stopped {
		session.mu.Unlock()
		return
	}
	session.finalized = true
	frames := session.frames
	session.mu.Unlock()
	a.mu.Lock()
	control := a.control
	a.mu.Unlock()
	if control != nil && !control.IsDestroyed() {
		control.Hide()
	}
	res := session.acc.Result()
	if res.Image == nil || res.Image.Bounds().Dy() < 2 {
		a.toast("滚动截图没有抓到有效画面", "error")
		a.clearScrollSession()
		return
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, res.Image); err != nil {
		a.toast("长图编码失败: "+err.Error(), "error")
		a.clearScrollSession()
		return
	}
	capped := status == "max"
	if err := a.showScrollResult(buf.Bytes(), frames, capped); err != nil {
		a.toast("滚动截图保存失败: "+err.Error(), "error")
	}
	a.clearScrollSession()
}

// clearScrollSession releases the session and the capture state.
func (a *App) clearScrollSession() {
	a.hideScrollFrame()
	a.mu.Lock()
	a.scroll = nil
	a.capturing = false
	a.mu.Unlock()
}

// emitScrollProgress sends the session state to the control window.
func (a *App) emitScrollProgress(session *scrollSession) {
	a.mu.Lock()
	control := a.control
	a.mu.Unlock()
	frames, height, status, auto := session.snapshot()
	emitToWindow(a, control, EventScrollProgress, ScrollProgressPayload{
		Frames: frames,
		Height: height,
		Status: status,
		Auto:   auto,
	})
}

// setScrollAuto switches the running session between auto and manual scrolling.
func (a *App) setScrollAuto(enabled bool) error {
	a.mu.Lock()
	session := a.scroll
	a.mu.Unlock()
	if session == nil {
		return errors.New("没有进行中的滚动截图")
	}
	if enabled && !capture.AccessibilityGranted() {
		return errors.New("自动滚动需要「辅助功能」权限：请到系统设置 → 隐私与安全性 → 辅助功能中允许 Pinstill")
	}
	if enabled && !capture.ScrollSupported() {
		return errors.New("当前平台暂不支持自动滚动")
	}
	session.mu.Lock()
	session.auto = enabled
	session.mu.Unlock()
	a.emitScrollProgress(session)
	return nil
}

// setScrollPaused pauses or resumes frame capturing for the running session.
func (a *App) setScrollPaused(paused bool) error {
	a.mu.Lock()
	session := a.scroll
	a.mu.Unlock()
	if session == nil {
		return errors.New("没有进行中的滚动截图")
	}
	session.mu.Lock()
	session.paused = paused
	if paused {
		session.status = "paused"
	} else {
		session.status = "running"
	}
	session.mu.Unlock()
	a.emitScrollProgress(session)
	return nil
}

// finishScroll ends the session: "finish" stitches and previews immediately,
// "cancel" discards it.
func (a *App) finishScroll(action string) error {
	a.scrollMu.Lock()
	defer a.scrollMu.Unlock()
	a.mu.Lock()
	session := a.scroll
	control := a.control
	a.mu.Unlock()
	if session == nil {
		return errors.New("没有进行中的滚动截图")
	}
	if action != "finish" && action != "cancel" {
		return fmt.Errorf("未知的滚动截图动作 %q", action)
	}
	session.mu.Lock()
	if !session.stopped {
		session.stopped = true
		close(session.stop)
	}
	session.mu.Unlock()
	select {
	case <-session.done:
	case <-time.After(10 * time.Second):
		log.Printf("pinstill: scroll loop did not stop in time")
	}
	if action == "cancel" {
		if control != nil && !control.IsDestroyed() {
			control.Hide()
		}
		a.clearScrollSession()
		return nil
	}
	if session.isFinalized() {
		// The loop reached the end of the page and already opened the preview.
		return nil
	}
	a.finalizeScroll(session, "finish")
	return nil
}

// applyCaptureAction performs one of the capture actions on finished pixels.
func (a *App) applyCaptureAction(action string, pngBytes []byte, payload capturePayload) {
	a.playSoundIfEnabled(a.settingsSnapshot())
	switch action {
	case "copy":
		if err := mygo.Clipboard.WriteImage(pngBytes); err != nil {
			a.toast("写入剪贴板失败: "+err.Error(), "error")
		} else {
			a.toast("截图已复制到剪贴板", "success")
		}
	case "pin":
		if _, err := a.pinPNG(pngBytes, payload.Width, payload.Height); err != nil {
			a.toast("固定到屏幕失败: "+err.Error(), "error")
		} else {
			a.toast("已固定到屏幕", "success")
		}
	case "save":
		if path, err := a.savePNGBytes(pngBytes); err != nil {
			a.toast("保存失败: "+err.Error(), "error")
		} else if path != "" {
			a.toast("已保存到 "+path, "success")
		}
	}
}

// pngToRGBA decodes a PNG frame into an RGBA image.
func pngToRGBA(data []byte) (*image.RGBA, error) {
	img, err := png.Decode(bytes.NewReader(data))
	if err != nil {
		return nil, err
	}
	if rgba, ok := img.(*image.RGBA); ok {
		return rgba, nil
	}
	out := image.NewRGBA(img.Bounds())
	draw.Draw(out, out.Bounds(), img, img.Bounds().Min, draw.Src)
	return out, nil
}

// simulatedScrollFrame produces synthetic frames for the scroll selftest.
var (
	simulatedScrollCounter atomic.Int64
	simulatedTallOnce      sync.Once
	simulatedTall          *image.RGBA
)

// simulatedTallImage draws the tall page the scroll selftest scrolls over.
func simulatedTallImage() *image.RGBA {
	simulatedTallOnce.Do(func() {
		const width, height = 1280, 2600
		img := image.NewRGBA(image.Rect(0, 0, width, height))
		for y := 0; y < height; y++ {
			for x := 0; x < width; x++ {
				h := uint32(y)*2654435761 ^ uint32(x)*2246822519
				img.SetRGBA(x, y, color.RGBA{
					R: uint8(h >> 24),
					G: uint8(h >> 16),
					B: uint8(h >> 8),
					A: 255,
				})
			}
		}
		simulatedTall = img
	})
	return simulatedTall
}

// simulatedScrollFrame returns the next frame of the simulated page.
func simulatedScrollFrame() []byte {
	tall := simulatedTallImage()
	idx := int(simulatedScrollCounter.Add(1) - 1)
	maxIdx := (tall.Bounds().Dy() - 800) / 300
	if idx > maxIdx {
		idx = maxIdx
	}
	top := idx * 300
	frame := image.NewRGBA(image.Rect(0, 0, 1280, 800))
	draw.Draw(frame, frame.Bounds(), tall, image.Pt(0, top), draw.Src)
	var buf bytes.Buffer
	if err := png.Encode(&buf, frame); err != nil {
		return nil
	}
	return buf.Bytes()
}
