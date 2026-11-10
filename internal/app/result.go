package app

import (
	"errors"
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
	url := a.shotURL("/s/" + payload.ID + ".png")
	meta := ScrollResultPayload{
		URL:    url,
		Width:  width,
		Height: height,
		Frames: frames,
		Capped: capped,
	}
	if a.store != nil {
		if entries := a.store.List(); len(entries) > 0 {
			meta.HistoryID = entries[0].ID
		}
	}

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
		win.Show()
		win.Focus()
	})
	win.OnClosed(func() {
		a.mu.Lock()
		a.result = nil
		a.resultPNG = nil
		a.resultMeta = ScrollResultPayload{}
		a.mu.Unlock()
	})

	a.mu.Lock()
	a.result = win
	a.resultMeta = meta
	a.resultPNG = pngBytes
	a.mu.Unlock()

	_ = EventScrollResult.Emit(win, meta)
	return nil
}

// scrollResultPNG returns the stitched image the preview window shows.
func (a *App) scrollResultPNG() ([]byte, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if len(a.resultPNG) == 0 {
		return nil, errors.New("没有可用的滚动截图结果")
	}
	return a.resultPNG, nil
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
	pngBytes, err := a.scrollResultPNG()
	if err != nil {
		return err
	}
	if err := a.copyImagePNG(pngBytes); err != nil {
		return err
	}
	a.toast("长图已复制到剪贴板", "success")
	a.closeScrollResultSoon()
	return nil
}

// saveScrollResult saves the stitched image through the system dialog and
// closes the preview once a file was written.
func (a *App) saveScrollResult() (string, error) {
	pngBytes, err := a.scrollResultPNG()
	if err != nil {
		return "", err
	}
	path, err := a.savePNGBytes(pngBytes)
	if err != nil {
		return "", err
	}
	if path != "" {
		a.toast("长图已保存到 "+path, "success")
		a.closeScrollResultSoon()
	}
	return path, nil
}

// pinScrollResult opens the stitched image as a desktop pin.
func (a *App) pinScrollResult() (string, error) {
	pngBytes, err := a.scrollResultPNG()
	if err != nil {
		return "", err
	}
	width, height := imageDimensions(pngBytes)
	id, err := a.pinPNG(pngBytes, width, height)
	if err != nil {
		return "", err
	}
	a.toast("长图已贴到桌面", "success")
	a.closeScrollResultSoon()
	return id, nil
}

// closeScrollResult closes the preview window (its close handler clears the
// cached image).
func (a *App) closeScrollResult() {
	a.mu.Lock()
	win := a.result
	a.mu.Unlock()
	if win != nil && !win.IsDestroyed() {
		win.Close()
	}
}

// closeScrollResultSoon closes the preview after the page action returned, so
// the binding call can still resolve before its window goes away.
func (a *App) closeScrollResultSoon() {
	go func() {
		time.Sleep(180 * time.Millisecond)
		a.closeScrollResult()
	}()
}
