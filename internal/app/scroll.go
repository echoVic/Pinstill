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
)

// scrollSession is one in-progress scrolling capture.
type scrollSession struct {
	mu      sync.Mutex
	rect    Rect
	region  capture.Rect
	auto    bool
	acc     *stitch.Accumulator
	frames  int
	height  int
	status  string
	stop    chan struct{}
	done    chan struct{}
	stopped bool
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

// createScrollControl creates the small window that shows scroll progress and
// actions while the overlay is hidden.
func (a *App) createScrollControl() {
	win := mygo.NewWindow(mygo.WindowOptions{
		Title:             "Pinstill 滚动截图",
		Width:             470,
		Height:            84,
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

// placeScrollControl puts the control bar next to the capture region, without
// covering it.
func (a *App) placeScrollControl(region capture.Rect) {
	a.mu.Lock()
	win := a.control
	a.mu.Unlock()
	if win == nil || win.IsDestroyed() {
		return
	}
	width, height := 470, 84
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

// StartScroll begins a scrolling capture for the current overlay selection.
// It hides the overlay and shows the scroll control window; the frames are
// captured behind it.
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
	if autoScroll && !capture.ScrollSupported() {
		return errors.New("当前平台暂不支持自动滚动，请使用手动滚动拼接")
	}
	if autoScroll && !capture.AccessibilityGranted() {
		return errors.New("自动滚动需要「辅助功能」权限：请到系统设置 → 隐私与安全性 → 辅助功能中允许 Pinstill")
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
		auto:   autoScroll,
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
	emitToWindow(a, control, EventScrollInit, ScrollInitPayload{
		Rect:                 rect,
		Auto:                 autoScroll,
		AccessibilityGranted: capture.AccessibilityGranted(),
	})
	go a.scrollLoop(session)
	return nil
}

// scrollLoop captures frames and stitches them until the user stops it, the
// content stops moving, or a limit is reached.
func (a *App) scrollLoop(session *scrollSession) {
	defer close(session.done)
	time.Sleep(scrollInitialDelay)
	stable := 0
	for {
		select {
		case <-session.stop:
			session.setStatus("stopped")
			a.emitScrollProgress(session)
			return
		default:
		}
		frame, err := a.captureRegion(session.region, capture.CaptureOptions{IncludeCursor: false})
		if err != nil {
			log.Printf("pinstill: scroll capture frame: %v", err)
			session.setStatus("stopped")
			a.emitScrollProgress(session)
			return
		}
		img, err := pngToRGBA(frame)
		if err != nil {
			log.Printf("pinstill: scroll frame decode: %v", err)
			session.setStatus("stopped")
			a.emitScrollProgress(session)
			return
		}
		static := false
		if session.frames == 0 {
			if _, err := session.acc.Add(img); err != nil {
				session.setStatus("stopped")
				a.emitScrollProgress(session)
				return
			}
		} else {
			static = session.acc.SameAsPrevious(img)
			if static {
				stable++
			} else {
				stable = 0
				if _, err := session.acc.Add(img); err != nil {
					session.setStatus("stopped")
					a.emitScrollProgress(session)
					return
				}
			}
		}
		session.mu.Lock()
		session.frames++
		session.height, _ = session.acc.Size()
		session.mu.Unlock()
		a.emitScrollProgress(session)
		if static && stable >= 2 {
			// The user paused or reached the end: keep the session alive so
			// they can scroll on, but stop automatic scrolling until new
			// content appears.
			session.mu.Lock()
			session.auto = false
			session.mu.Unlock()
			session.setStatus("stalled")
			a.emitScrollProgress(session)
			time.Sleep(500 * time.Millisecond)
			continue
		}
		if !static {
			session.mu.Lock()
			if session.status != "running" {
				session.status = "running"
			}
			session.mu.Unlock()
		}
		if session.frames >= scrollMaxFrames || session.height >= scrollMaxHeight {
			session.setStatus("max")
			a.emitScrollProgress(session)
			return
		}
		if session.isAuto() && capture.ScrollSupported() && os.Getenv("PINSTILL_SELFTEST_SCROLL_SIM") == "" {
			center := capture.Point{X: session.region.X + session.region.Width/2, Y: session.region.Y + session.region.Height/2}
			if err := capture.ScrollWheel(center, scrollDeltaPixels); err != nil {
				log.Printf("pinstill: scroll wheel: %v", err)
				session.setStatus("stopped")
				a.emitScrollProgress(session)
				return
			}
			time.Sleep(scrollAutoInterval)
			continue
		}
		select {
		case <-session.stop:
			session.setStatus("stopped")
			a.emitScrollProgress(session)
			return
		case <-time.After(scrollFrameInterval):
		}
	}
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

// setScrollAuto switches the running session to automatic scrolling.
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

// finishScroll stops the session, stitches the final image and runs action.
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
	hideControl := func() {
		if control != nil && !control.IsDestroyed() {
			control.Hide()
		}
	}
	clearSession := func() {
		a.mu.Lock()
		a.scroll = nil
		a.capturing = false
		a.mu.Unlock()
	}
	if action == "cancel" {
		hideControl()
		clearSession()
		emitToMain(a, EventCaptureCancelled, CaptureCancelledPayload{Reason: "user"})
		return nil
	}
	fail := func(err error) error {
		hideControl()
		clearSession()
		a.showMain()
		emitToMain(a, EventCaptureFailed, CaptureFailedPayload{Message: err.Error(), Permission: false})
		a.toast(err.Error(), "error")
		return err
	}
	switch action {
	case "annotate", "copy", "pin", "save":
	default:
		return fail(fmt.Errorf("未知的滚动截图动作 %q", action))
	}
	res := session.acc.Result()
	if res.Image == nil || res.Image.Bounds().Dy() < 2 {
		return fail(errors.New("滚动截图没有抓到有效画面"))
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, res.Image); err != nil {
		return fail(fmt.Errorf("长图编码失败: %w", err))
	}
	payload, err := a.storeCapture(buf.Bytes(), "scroll")
	if err != nil {
		return fail(err)
	}
	hideControl()
	a.mu.Lock()
	a.scroll = nil
	a.mu.Unlock()
	a.applyCaptureAction(action, buf.Bytes(), payload)
	a.mu.Lock()
	a.capturing = false
	a.mu.Unlock()
	return nil
}

// applyCaptureAction performs one of the capture actions on finished pixels.
func (a *App) applyCaptureAction(action string, pngBytes []byte, payload CaptureCompletedPayload) {
	a.playSoundIfEnabled(a.settingsSnapshot())
	switch action {
	case "annotate":
		a.showMain()
		emitToMain(a, EventCaptureCompleted, payload)
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
