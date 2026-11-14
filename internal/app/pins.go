package app

import (
	"errors"
	"fmt"
	"log"
	"math"
	"os"
	"path/filepath"
	"time"

	"github.com/echoVic/Pinstill/internal/diskio"
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
	states, err := a.pinStore.PrepareRestore(maxRestoredPins)
	if err != nil {
		a.toast("恢复贴图失败: "+err.Error(), "error")
		return
	}
	if count := a.archivedPinCount(); count > 0 {
		a.toast(fmt.Sprintf("%d 张较早的贴图已归档，原图已保留，不再自动打开", count), "info")
	}
	if len(states) == 0 {
		return
	}
	displays := mygo.Screen.Displays()
	for _, st := range states {
		st = clampPinToDisplays(st, displays)
		if err := a.createPinWindow(st); err != nil {
			log.Printf("pinstill: restore pin %s: %v", st.ID, err)
		}
	}
}

// clampPinToDisplays keeps a restored pin fully inside the work area of the
// display nearest its center, so it stays reachable after display changes.
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
	st.Width, st.Height = fitPinSize(st.Width, st.Height, max(area.Width, 1), max(area.Height, 1))
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
		Page:              mygo.PageOptions{Session: a.pageSession},
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
	win.OnClose(func(e *mygo.CloseEvent) {
		a.handlePinClose(id, e, func(err error) { a.reportPinError(win, err) })
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
	a.pinTimers[id] = time.AfterFunc(260*time.Millisecond, func() {
		if err := a.snapshotPin(id); err != nil {
			a.mu.Lock()
			win := a.pins[id]
			a.mu.Unlock()
			a.reportPinError(win, err)
		}
	})
	a.mu.Unlock()
}

// snapshotPin stores the current window geometry of one pin.
func (a *App) snapshotPin(id string) error {
	a.mu.Lock()
	win := a.pins[id]
	st, ok := a.pinStates[id]
	a.mu.Unlock()
	if !ok {
		return nil
	}
	if win == nil || win.IsDestroyed() {
		return nil
	}
	b := win.Bounds()
	st.X, st.Y, st.Width, st.Height = b.X, b.Y, b.Width, b.Height
	st.Opacity = win.Opacity() * 100
	return a.savePinSnapshot(st, nil)
}

// storePinState updates a live pin. A snapshot copied before removal must not
// create the pin again. The lock serializes this check and write with removal.
func (a *App) storePinState(st pin.State) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	if _, ok := a.pinStates[st.ID]; !ok {
		return fmt.Errorf("贴图 %s 不存在", st.ID)
	}
	return a.storePinStateLocked(st)
}

// storePinStateLocked publishes state only after persistence succeeds. The
// caller holds a.mu, including for a new pin's initial registration.
func (a *App) storePinStateLocked(st pin.State) error {
	if a.pinStore == nil {
		return errors.New("贴图存储不可用")
	}
	stored, err := a.pinStore.Upsert(st)
	if err != nil {
		return fmt.Errorf("贴图状态写入失败: %w", err)
	}
	a.pinStates[st.ID] = stored
	return nil
}

// snapshotAllPins records every pin's geometry, e.g. before the app quits.
func (a *App) snapshotAllPins() error {
	return a.snapshotAllPinsWith(a.snapshotPin)
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
	return a.openSavedPin(id, file, width, height, disp, a.createPinWindow)
}

// openSavedPin persists a saved image's initial state before showing its window.
// Keeping the native display lookup and window creation at the boundary lets
// persistence failures be handled without opening a window.
func (a *App) openSavedPin(id, file string, width, height int, disp mygo.Display, show func(pin.State) error) (string, error) {
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
		Width:     max(int(math.Round(widthDip)), 1),
		Height:    max(int(math.Round(heightDip)), 1),
		Opacity:   opacity,
		CreatedAt: time.Now(),
	}
	if disp.Bounds.Width > 0 && disp.Bounds.Height > 0 {
		st = clampPinToDisplays(st, []mygo.Display{disp})
	}
	a.mu.Lock()
	err := a.storePinStateLocked(st)
	a.mu.Unlock()
	if err != nil {
		// A failed directory sync can leave the index committed. Preserve its image.
		if diskio.Committed(err) {
			return "", err
		}
		// SaveImage already succeeded, but no state references this new image.
		if cleanupErr := os.Remove(filepath.Join(a.pinStore.Dir(), file)); cleanupErr != nil && !errors.Is(cleanupErr, os.ErrNotExist) {
			return "", errors.Join(err, fmt.Errorf("清理贴图文件失败: %w", cleanupErr))
		}
		return "", err
	}
	if err := show(st); err != nil {
		return "", err
	}
	return id, nil
}

// removePinState forgets a pin and deletes its image.
func (a *App) removePinState(id string) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	if _, ok := a.pinStates[id]; !ok {
		return nil
	}
	if a.pinStore == nil {
		return errors.New("贴图存储不可用")
	}
	if err := a.pinStore.Remove(id); err != nil {
		return fmt.Errorf("删除贴图失败: %w", err)
	}
	if timer := a.pinTimers[id]; timer != nil {
		timer.Stop()
	}
	delete(a.pinTimers, id)
	delete(a.pinStates, id)
	return nil
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
	return a.closePinWith(id, win.Close)
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
	return a.changePin(id, func(st *pin.State) { st.Width, st.Height = max(width, 1), max(height, 1) },
		func(st pin.State) { win.SetContentSize(st.Width, st.Height) })
}

// updatePinState stores the size and opacity the page reports.
func (a *App) updatePinState(id string, state PinWindowState) error {
	st, ok := a.pinState(id)
	if !ok {
		return fmt.Errorf("贴图 %s 不存在", id)
	}
	if state.Width > 0 {
		st.Width = max(state.Width, 1)
	}
	if state.Height > 0 {
		st.Height = max(state.Height, 1)
	}
	if state.Opacity > 0 {
		st.Opacity = min(max(state.Opacity, 10), 100)
	}
	return a.storePinState(st)
}

// setPinOpacity changes a pin's opacity (10..100) and persists it.
func (a *App) setPinOpacity(id string, opacity float64) error {
	a.mu.Lock()
	win := a.pins[id]
	a.mu.Unlock()
	if win == nil {
		return fmt.Errorf("贴图 %s 不存在", id)
	}
	if math.IsNaN(opacity) || math.IsInf(opacity, 0) {
		return errors.New("贴图透明度无效")
	}
	return a.changePin(id, func(st *pin.State) { st.Opacity = min(max(opacity, 10), 100) },
		func(st pin.State) { win.SetOpacity(st.Opacity / 100) })
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

func (a *App) handlePinClose(id string, e *mygo.CloseEvent, report func(error)) {
	a.mu.Lock()
	quitting := a.quitting
	a.mu.Unlock()
	if !quitting {
		if err := a.removePinState(id); err != nil {
			e.PreventDefault()
			report(err)
		}
	}
}

func (a *App) closePinWith(id string, closeWindow func()) error {
	if err := a.removePinState(id); err != nil {
		return err
	}
	closeWindow()
	return nil
}

func (a *App) changePin(id string, change func(*pin.State), apply func(pin.State)) error {
	a.mu.Lock()
	st, ok := a.pinStates[id]
	if !ok {
		a.mu.Unlock()
		return fmt.Errorf("贴图 %s 不存在", id)
	}
	change(&st)
	if err := a.storePinStateLocked(st); err != nil {
		a.mu.Unlock()
		return err
	}
	a.mu.Unlock()
	a.applyPinChange(id, apply)
	return nil
}

// applyPinChange keeps native window calls on the UI thread without holding
// App.mu while the dispatcher waits.
func (a *App) applyPinChange(id string, apply func(pin.State)) {
	a.mu.Lock()
	native := a.pins[id] != nil
	dispatch := a.scrollUI
	a.mu.Unlock()
	run := func() {
		latest, ok := a.pinState(id)
		if !ok {
			return
		}
		apply(latest)
	}
	if dispatch != nil {
		dispatch(run)
		return
	}
	if native {
		mygo.RunOnMain(run)
		return
	}
	run()
}

func (a *App) savePinSnapshot(st pin.State, report func(error)) error {
	if err := a.storePinState(st); err != nil {
		log.Printf("pinstill: save pin state: %v", err)
		if report != nil {
			report(err)
		}
		return err
	}
	return nil
}

// fitPinSize uses one scale for both axes, with one DIP as the rounding floor.
func fitPinSize(width, height, maxWidth, maxHeight int) (int, int) {
	w, h := float64(max(width, 1)), float64(max(height, 1))
	scale := min(1.0, float64(maxWidth)/w, float64(maxHeight)/h)
	return max(1, int(math.Round(w*scale))), max(1, int(math.Round(h*scale)))
}

// archivedPinCount can be exposed by the parent service for persistent UI info.
func (a *App) archivedPinCount() int {
	if a.pinStore == nil {
		return 0
	}
	count := 0
	for _, st := range a.pinStore.List() {
		if st.Archived {
			count++
		}
	}
	return count
}

func (a *App) archivedPinStates() []pin.State {
	states := []pin.State{}
	if a.pinStore == nil {
		return states
	}
	for _, st := range a.pinStore.List() {
		if st.Archived {
			states = append(states, st)
		}
	}
	return states
}
func (a *App) restoreArchivedPin(id string) error {
	return a.restoreArchivedPinWithDisplays(id, mygo.Screen.Displays(), a.createPinWindow)
}

func (a *App) restoreArchivedPinWithDisplays(id string, displays []mygo.Display, show func(pin.State) error) error {
	return a.restoreArchivedPinWith(id, func(st pin.State) error {
		st = clampPinToDisplays(st, displays)
		if err := a.storePinState(st); err != nil {
			return err
		}
		return show(st)
	})
}
func (a *App) restoreArchivedPinWith(id string, show func(pin.State) error) error {
	a.mu.Lock()
	if len(a.pinStates) >= maxRestoredPins {
		a.mu.Unlock()
		return fmt.Errorf("最多同时打开 %d 张贴图，请先关闭一张再恢复归档", maxRestoredPins)
	}
	if _, ok := a.pinStates[id]; ok {
		a.mu.Unlock()
		return errors.New("这张贴图已经打开")
	}
	var archived pin.State
	found := false
	for _, st := range a.archivedPinStates() {
		if st.ID == id {
			archived = st
			found = true
			break
		}
	}
	if !found {
		a.mu.Unlock()
		return fmt.Errorf("归档贴图 %s 不存在", id)
	}
	if _, err := a.pinStore.ReadImage(archived); err != nil {
		a.mu.Unlock()
		return fmt.Errorf("归档原图无法读取: %w", err)
	}
	st := archived
	st.Archived = false
	if err := a.storePinStateLocked(st); err != nil {
		a.mu.Unlock()
		return err
	}
	a.mu.Unlock()
	if err := show(st); err != nil {
		a.mu.Lock()
		defer a.mu.Unlock()
		delete(a.pinStates, id)
		if timer := a.pinTimers[id]; timer != nil {
			timer.Stop()
			delete(a.pinTimers, id)
		}
		_, rollbackErr := a.pinStore.Upsert(archived)
		if rollbackErr != nil {
			return errors.Join(err, fmt.Errorf("恢复失败且无法重新归档，原图仍保留: %w", rollbackErr))
		}
		return err
	}
	return nil
}
func (a *App) snapshotAllPinsWith(snapshot func(string) error) error {
	a.mu.Lock()
	ids := make([]string, 0, len(a.pinStates))
	for id := range a.pinStates {
		ids = append(ids, id)
	}
	a.mu.Unlock()
	var failures []error
	for _, id := range ids {
		if err := snapshot(id); err != nil {
			failures = append(failures, err)
		}
	}
	return errors.Join(failures...)
}

// Use a native message for asynchronous persistence failures: the main history
// window may be hidden and the pin page has no general toast subscription.
func (a *App) reportPinError(win *mygo.Window, err error) {
	if err == nil {
		return
	}
	go func() {
		_, dialogErr := mygo.Dialog.Message(mygo.MessageOptions{
			Parent: win, Type: mygo.MessageError, Title: "贴图未能保存", Message: err.Error(),
			Detail: "请检查存储位置后重试。当前贴图已保留。",
		})
		if dialogErr != nil {
			log.Printf("pinstill: show pin error: %v", dialogErr)
		}
	}()
}
