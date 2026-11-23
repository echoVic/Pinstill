package app

import (
	"errors"
	"fmt"
	"time"

	"github.com/egoist/mygo"
)

// scrollResultWindowSize returns the preview window size for a stitched long
// image: the page fits the image to the window width and scrolls vertically,
// so the window itself stays a comfortable fixed size on the display.
func scrollResultWindowSize(display mygo.Display) (int, int) {
	area := display.WorkArea
	if area.Width <= 0 || area.Height <= 0 {
		area = display.Bounds
	}
	width := min(980, max(520, area.Width-120))
	height := min(720, max(340, area.Height-120))
	return width, height
}

// showScrollResult opens the preview window for a finished scrolling capture.
// The stitched image is stored in the session cache (and history, when the
// user keeps it), then handed to the window; the bytes are kept in memory so
// the copy/save/pin actions do not read the file again.
func (a *App) showScrollResult(pngBytes []byte, frames int, capped bool) error {
	return a.showScrollResultFor(nil, pngBytes, frames, capped)
}

func (a *App) showScrollResultFor(session *scrollSession, pngBytes []byte, frames int, capped bool) error {
	if session != nil && !a.scrollResultIsCurrent(session) {
		return nil
	}
	if len(pngBytes) == 0 {
		return errors.New("滚动截图结果为空")
	}
	width, height := imageDimensions(pngBytes)
	if width == 0 || height == 0 {
		return errors.New("滚动截图结果无效")
	}
	payload, err := a.storeCapture(pngBytes)
	if err != nil {
		return err
	}
	if session != nil && !a.scrollResultIsCurrent(session) {
		return nil
	}
	a.mu.Lock()
	if err := a.releasePendingPreviewsLocked(); err != nil {
		a.mu.Unlock()
		return err
	}
	// Stage separately: a cancelled finalizer must not evict the open result.
	var staged string
	sessionFile, err := a.writePreviewLocked(&staged, pngBytes)
	a.mu.Unlock()
	if err != nil {
		return fmt.Errorf("长图预览写入失败: %w", err)
	}
	url := a.shotURL("/s/" + sessionFile)
	meta := ScrollResultPayload{
		ID:        sessionFile,
		HistoryID: payload.HistoryID,
		URL:       url,
		Width:     width,
		Height:    height,
		Frames:    frames,
		Capped:    capped,
	}
	var publishErr error
	a.runScrollUI(func() {
		if session != nil && !a.scrollResultIsCurrent(session) {
			a.mu.Lock()
			if err := a.releaseCacheFileLocked(&staged); err != nil {
				a.pendingPreviewFiles = append(a.pendingPreviewFiles, staged)
				reportCacheCleanup(err)
			}
			a.mu.Unlock()
			return
		}
		a.mu.Lock()
		if err := a.releaseCacheFileLocked(&a.resultSessionFile); err != nil {
			if cleanupErr := a.releaseCacheFileLocked(&staged); cleanupErr != nil {
				a.pendingPreviewFiles = append(a.pendingPreviewFiles, staged)
			}
			a.mu.Unlock()
			publishErr = err
			return
		}
		a.resultSessionFile = staged
		a.mu.Unlock()
		a.publishScrollResult(pngBytes, meta)
	})
	return publishErr
}

// Window replacement is one UI-thread operation. Disk writes happen before it
// so settings/native callbacks never wait on a lock held by this callback.
func (a *App) publishScrollResult(pngBytes []byte, meta ScrollResultPayload) {

	// Only one preview window at a time.
	a.mu.Lock()
	old := a.result
	a.result = nil
	a.resultPNG = nil
	a.mu.Unlock()
	if old != nil && !old.IsDestroyed() {
		old.Close()
	}

	display := mygo.Screen.DisplayNearestPoint(mygo.Screen.CursorScreenPoint())
	area := display.WorkArea
	if area.Width <= 0 || area.Height <= 0 {
		area = display.Bounds
	}
	winWidth, winHeight := scrollResultWindowSize(display)
	x := area.X + (area.Width-winWidth)/2
	y := area.Y + (area.Height-winHeight)/2
	win := mygo.NewWindow(mygo.WindowOptions{
		Page:              mygo.PageOptions{Session: a.pageSession},
		Title:             "Pinstill 滚动截图",
		Width:             winWidth,
		Height:            winHeight,
		X:                 x,
		Y:                 y,
		Hidden:            true,
		AlwaysOnTop:       true,
		BackgroundColor:   "#141412",
		TitleBarStyle:     mygo.TitleBarHiddenInset,
		TitleBarHeight:    48,
		MinWidth:          480,
		MinHeight:         320,
		DisableMinimize:   true,
		DisableMaximize:   true,
		DisableFullScreen: true,
		URL:               "/result.html",
	})
	win.OnReadyToShow(func() {
		a.mu.Lock()
		current := a.result == win && !a.quitting
		a.mu.Unlock()
		if current {
			win.Show()
			win.Focus()
		}
	})
	win.OnClosed(func() {
		a.mu.Lock()
		if a.result == win {
			if a.resultSessionFile == meta.ID {
				reportCacheCleanup(a.releaseCacheFileLocked(&a.resultSessionFile))
			}
			a.result = nil
			a.resultPNG = nil
			a.resultMeta = ScrollResultPayload{}
		}
		a.mu.Unlock()
	})

	a.mu.Lock()
	a.result = win
	a.resultMeta = meta
	a.resultPNG = pngBytes
	a.mu.Unlock()

	_ = EventScrollResult.Emit(win, meta)
}

type scrollResultSnapshot struct {
	png        []byte
	window     *mygo.Window
	meta       ScrollResultPayload
	generation uint64
}

func (a *App) scrollResultSnapshot(id string) (scrollResultSnapshot, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if id == "" || id != a.resultMeta.ID || len(a.resultPNG) == 0 {
		return scrollResultSnapshot{}, errors.New("没有可用的滚动截图结果")
	}
	return scrollResultSnapshot{png: a.resultPNG, window: a.result, meta: a.resultMeta, generation: a.captureGeneration}, nil
}

// scrollResultMeta returns the preview metadata (empty when no preview is up).
func (a *App) scrollResultMeta() ScrollResultPayload {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.resultMeta
}

// copyScrollResult puts the stitched image on the clipboard and closes the
// preview window afterwards.
func (a *App) copyScrollResult() error {
	return a.copyScrollResultFor(a.scrollResultMeta().ID)
}

func (a *App) copyScrollResultFor(id string) error {
	result, err := a.scrollResultSnapshot(id)
	if err != nil {
		return err
	}
	if err := a.copyImagePNG(result.png); err != nil {
		return err
	}
	a.toast("长图已复制到剪贴板", "success")
	a.completeScrollResultQuick(result, "copy")
	return nil
}

// saveScrollResult saves the stitched image through the system dialog and
// closes the preview once a file was written.
func (a *App) saveScrollResult() (string, error) {
	return a.saveScrollResultFor(a.scrollResultMeta().ID)
}

func (a *App) saveScrollResultFor(id string) (string, error) {
	result, err := a.scrollResultSnapshot(id)
	if err != nil {
		return "", err
	}
	path, err := a.savePNGBytes(result.png)
	if err != nil {
		return "", err
	}
	if path != "" {
		a.toast("长图已保存到 "+path, "success")
		a.completeScrollResultQuick(result, "save")
	}
	return path, nil
}

func (a *App) completeScrollResultQuick(result scrollResultSnapshot, action string) {
	a.closeScrollResultSoon(result.window)
	if a.scrollResultMeta().ID != result.meta.ID {
		return
	}
	a.postQuickCapture(result.generation, action, result.png, capturePayload{
		Width: result.meta.Width, Height: result.meta.Height, HistoryID: result.meta.HistoryID,
	})
}

// pinScrollResult opens the stitched image as a desktop pin.
func (a *App) pinScrollResult() (string, error) {
	return a.pinScrollResultFor(a.scrollResultMeta().ID)
}

func (a *App) pinScrollResultFor(id string) (string, error) {
	result, err := a.scrollResultSnapshot(id)
	if err != nil {
		return "", err
	}
	width, height := imageDimensions(result.png)
	pinID, err := a.pinPNG(result.png, width, height)
	if err != nil {
		return "", err
	}
	a.toast("长图已贴到桌面", "success")
	a.closeScrollResultSoon(result.window)
	return pinID, nil
}

// closeScrollResult closes the preview window (its close handler clears the
// cached image).
func (a *App) closeScrollResult() {
	_ = a.closeScrollResultFor(a.scrollResultMeta().ID)
}

func (a *App) closeScrollResultFor(id string) error {
	result, err := a.scrollResultSnapshot(id)
	if err != nil {
		return err
	}
	if result.window != nil && !result.window.IsDestroyed() {
		result.window.Close()
	}
	return nil
}

// closeScrollResultSoon closes the preview after the page action returned, so
// the binding call can still resolve before its window goes away.
func (a *App) closeScrollResultSoon(win *mygo.Window) {
	go func() {
		time.Sleep(180 * time.Millisecond)
		if win != nil && !win.IsDestroyed() {
			win.Close()
		}
	}()
}
