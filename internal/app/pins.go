package app

import (
	"errors"
	"fmt"
	"log"
	"path/filepath"
	"time"

	"github.com/echoVic/Pinstill/internal/pin"
	"github.com/egoist/mygo"
)

// maxRestoredPins caps how many desktop pins are reopened at launch.
const maxRestoredPins = 60

// pinWindowID makes a filename-safe, sortable id for a desktop pin.
func pinWindowID(t time.Time) string {
	return "pin-" + t.Format("20060102-150405") + "-" + randomSuffix()
}

// pinURL resolves the image a pin window shows.
func (a *App) pinURL(file string) string { return a.shotURL("/p/" + file) }

// pinState returns the stored state of one pin.
func (a *App) pinState(id string) (pin.State, bool) {
	a.mu.Lock()
	defer a.mu.Unlock()
	st, ok := a.pinStates[id]
	return st, ok
}

// restorePins recreates the pins the user left on the desktop.
func (a *App) restorePins() {
	if a.pinStore == nil {
		return
	}
	states := a.pinStore.List()
	if len(states) == 0 {
		return
	}
	// Keep the newest pins when the store holds more than the restore cap.
	start := 0
	if len(states) > maxRestoredPins {
		start = len(states) - maxRestoredPins
	}
	displays := mygo.Screen.Displays()
	for _, st := range states[start:] {
		st = clampPinToDisplays(st, displays)
		if err := a.createPinWindow(st); err != nil {
			log.Printf("pinstill: restore pin %s: %v", st.ID, err)
		}
	}
}

// clampPinToDisplays keeps a restored pin fully inside the work area of the
// display it overlaps most, so it stays reachable after display changes.
func clampPinToDisplays(st pin.State, displays []mygo.Display) pin.State {
	if len(displays) == 0 {
		return st
	}
	centerX, centerY := st.X+st.Width/2, st.Y+st.Height/2
	best := displays[0]
	bestDX, bestDY := 1<<31-1, 1<<31-1
	for _, d := range displays {
		b := d.Bounds
		dx := 0
		if centerX < b.X {
			dx = b.X - centerX
		} else if centerX > b.X+b.Width {
			dx = centerX - (b.X + b.Width)
		}
		dy := 0
		if centerY < b.Y {
			dy = b.Y - centerY
		} else if centerY > b.Y+b.Height {
			dy = centerY - (b.Y + b.Height)
		}
		if dx+dy < bestDX+bestDY {
			best, bestDX, bestDY = d, dx, dy
		}
	}
	area := best.WorkArea
	if area.Width <= 0 || area.Height <= 0 {
		area = best.Bounds
	}
	st.Width = min(max(st.Width, 32), max(area.Width, 32))
	st.Height = min(max(st.Height, 32), max(area.Height, 32))
	st.X = min(max(st.X, area.X), area.X+area.Width-st.Width)
	st.Y = min(max(st.Y, area.Y), area.Y+area.Height-st.Height)
	return st
}

// createPinWindow opens one pin window from persisted state and asks the
// page to show its image.
func (a *App) createPinWindow(st pin.State) error {
	if st.Opacity <= 0 {
		st.Opacity = 100
	}
	opacity := st.Opacity
	border := a.settingsSnapshot().PinBorder
	win := mygo.NewWindow(mygo.WindowOptions{
		Title:             "Pinstill 贴图",
		Width:             st.Width,
		Height:            st.Height,
		X:                 st.X,
		Y:                 st.Y,
		Panel:             true,
		Frameless:         true,
		AlwaysOnTop:       true,
		Hidden:            true,
		DisableResize:     true,
		DisableMinimize:   true,
		DisableMaximize:   true,
		DisableFullScreen: true,
		BackgroundColor:   "#f2eee4",
		URL:               "/pin.html",
	})
	win.SetOpacity(opacity / 100)
	id := st.ID
	win.OnClose(func(*mygo.CloseEvent) {
		a.mu.Lock()
		quitting := a.quitting
		a.mu.Unlock()
		if !quitting {
			a.removePinState(id)
		}
	})
	win.OnClosed(func() {
		a.mu.Lock()
		delete(a.pins, id)
		delete(a.pinStates, id)
		if timer := a.pinTimers[id]; timer != nil {
			timer.Stop()
			delete(a.pinTimers, id)
		}
		a.mu.Unlock()
	})
	win.OnReadyToShow(func() { win.Show() })
	win.OnMove(func() { a.persistPin(id) })
	win.OnResize(func() { a.persistPin(id) })

	a.mu.Lock()
	a.pins[id] = win
	a.pinStates[id] = st
	a.mu.Unlock()

	_ = EventPinInit.Emit(win, PinInitPayload{
		ID:      id,
		URL:     a.pinURL(st.File),
		Width:   float64(st.Width),
		Height:  float64(st.Height),
		Opacity: opacity,
		Border:  border,
	})
	return nil
}

// persistPin debounces the bookkeeping of a pin the user moved or resized.
func (a *App) persistPin(id string) {
	a.mu.Lock()
	if timer := a.pinTimers[id]; timer != nil {
		timer.Stop()
	}
	a.pinTimers[id] = time.AfterFunc(260*time.Millisecond, func() { a.snapshotPin(id) })
	a.mu.Unlock()
}

// snapshotPin stores the current window geometry of one pin.
func (a *App) snapshotPin(id string) {
	a.mu.Lock()
	win := a.pins[id]
	st, ok := a.pinStates[id]
	a.mu.Unlock()
	if !ok {
		return
	}
	if win == nil || win.IsDestroyed() {
		return
	}
	b := win.Bounds()
	st.X, st.Y, st.Width, st.Height = b.X, b.Y, b.Width, b.Height
	st.Opacity = win.Opacity() * 100
	a.storePinState(st)
}

// storePinState writes one pin's state to the store.
func (a *App) storePinState(st pin.State) {
	if a.pinStore == nil {
		return
	}
	a.mu.Lock()
	a.pinStates[st.ID] = st
	a.mu.Unlock()
	if _, err := a.pinStore.Upsert(st); err != nil {
		log.Printf("pinstill: save pin state: %v", err)
	}
}

// snapshotAllPins records every pin's geometry, e.g. before the app quits.
func (a *App) snapshotAllPins() {
	a.mu.Lock()
	ids := make([]string, 0, len(a.pinStates))
	for id := range a.pinStates {
		ids = append(ids, id)
	}
	a.mu.Unlock()
	for _, id := range ids {
		a.snapshotPin(id)
	}
}

// pinPNG creates a new desktop pin showing pngBytes.
func (a *App) pinPNG(pngBytes []byte, width, height int) (string, error) {
	if a.pinStore == nil {
		return "", errors.New("贴图存储不可用")
	}
	if width == 0 || height == 0 {
		width, height = imageDimensions(pngBytes)
	}
	if width == 0 || height == 0 {
		return "", errors.New("贴图数据无效")
	}
	id := pinWindowID(time.Now())
	file, err := a.pinStore.SaveImage(id, pngBytes)
	if err != nil {
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
	s := a.settingsSnapshot()
	a.mu.Lock()
	a.pinSeq++
	offset := ((a.pinSeq - 1) % 6) * 24
	a.mu.Unlock()
	x := disp.WorkArea.X + disp.WorkArea.Width/2 - int(widthDip)/2 + offset
	y := disp.WorkArea.Y + disp.WorkArea.Height/2 - int(heightDip)/2 + offset
	opacity := s.PinOpacity
	if opacity <= 0 {
		opacity = 100
	}
	st := pin.State{
		ID:        id,
		File:      file,
		X:         x,
		Y:         y,
		Width:     max(int(widthDip), 32),
		Height:    max(int(heightDip), 32),
		Opacity:   opacity,
		CreatedAt: time.Now(),
	}
	a.storePinState(st)
	if err := a.createPinWindow(st); err != nil {
		return "", err
	}
	return id, nil
}

// removePinState forgets a pin and deletes its image.
func (a *App) removePinState(id string) {
	a.mu.Lock()
	delete(a.pinTimers, id)
	a.mu.Unlock()
	if a.pinStore != nil {
		if err := a.pinStore.Remove(id); err != nil {
			log.Printf("pinstill: remove pin %s: %v", id, err)
		}
	}
	a.mu.Lock()
	delete(a.pinStates, id)
	a.mu.Unlock()
}

// closePin closes a pin window; the user deleted it, so it is not restored
// on the next launch.
func (a *App) closePin(id string) error {
	a.mu.Lock()
	win := a.pins[id]
	a.mu.Unlock()
	if win == nil {
		return fmt.Errorf("贴图 %s 不存在", id)
	}
	a.removePinState(id)
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

// resizePin sets a pin's window size in DIPs and persists it.
func (a *App) resizePin(id string, width, height int) error {
	a.mu.Lock()
	win := a.pins[id]
	a.mu.Unlock()
	if win == nil {
		return fmt.Errorf("贴图 %s 不存在", id)
	}
	width = max(width, 32)
	height = max(height, 32)
	win.SetContentSize(width, height)
	a.snapshotPin(id)
	return nil
}

// updatePinState stores the size and opacity the page reports.
func (a *App) updatePinState(id string, state PinWindowState) error {
	st, ok := a.pinState(id)
	if !ok {
		return fmt.Errorf("贴图 %s 不存在", id)
	}
	if state.Width > 0 {
		st.Width = max(state.Width, 32)
	}
	if state.Height > 0 {
		st.Height = max(state.Height, 32)
	}
	if state.Opacity > 0 {
		st.Opacity = min(max(state.Opacity, 10), 100)
	}
	a.storePinState(st)
	return nil
}

// setPinOpacity changes a pin's opacity (10..100) and persists it.
func (a *App) setPinOpacity(id string, opacity float64) error {
	a.mu.Lock()
	win := a.pins[id]
	a.mu.Unlock()
	if win == nil {
		return fmt.Errorf("贴图 %s 不存在", id)
	}
	opacity = min(max(opacity, 10), 100)
	win.SetOpacity(opacity / 100)
	st, ok := a.pinState(id)
	if ok {
		st.Opacity = opacity
		a.storePinState(st)
	}
	return nil
}

// copyPinImage puts a pin's image on the clipboard.
func (a *App) copyPinImage(id string) error {
	img, err := a.pinImage(id)
	if err != nil {
		return err
	}
	if err := mygo.Clipboard.WriteImage(img); err != nil {
		return fmt.Errorf("写入剪贴板失败: %w", err)
	}
	a.toast("贴图已复制到剪贴板", "success")
	return nil
}

// savePinImage saves a pin's image through the save dialog.
func (a *App) savePinImage(id string) (string, error) {
	img, err := a.pinImage(id)
	if err != nil {
		return "", err
	}
	return a.savePNGBytes(img)
}

// pinImage reads the PNG of one pin.
func (a *App) pinImage(id string) ([]byte, error) {
	st, ok := a.pinState(id)
	if !ok {
		return nil, fmt.Errorf("贴图 %s 不存在", id)
	}
	if a.pinStore == nil {
		return nil, errors.New("贴图存储不可用")
	}
	img, err := a.pinStore.ReadImage(st)
	if err != nil {
		return nil, fmt.Errorf("读取贴图失败: %w", err)
	}
	return img, nil
}

// pinImagePath returns the file of one pin, for the page and for save
// dialogs.
func (a *App) pinImagePath(id string) string {
	st, ok := a.pinState(id)
	if !ok || a.pinStore == nil {
		return ""
	}
	return filepath.Join(a.pinStore.Dir(), st.File)
}
