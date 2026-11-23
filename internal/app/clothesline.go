package app

import (
	"errors"
	"log"
	"path/filepath"

	"github.com/echoVic/Pinstill/internal/clothesline"
	"github.com/echoVic/Pinstill/internal/diskio"
	"github.com/echoVic/Pinstill/internal/imagebudget"
	"github.com/egoist/mygo"
)

type LineEntry struct {
	ID       string `json:"id"`
	Name     string `json:"name"`
	ThumbURL string `json:"thumbUrl"`
}

type LineView struct {
	Revision uint64      `json:"revision"`
	Entries  []LineEntry `json:"entries"`
}

var EventLineChanged = mygo.NewEvent[LineView]("line:changed")

func (a *App) lineView(state clothesline.State) LineView {
	view := LineView{Revision: state.Revision, Entries: make([]LineEntry, 0, len(state.Items))}
	for _, item := range state.Items {
		view.Entries = append(view.Entries, LineEntry{ID: item.ID, Name: item.Name, ThumbURL: a.shotURL("/l/" + item.ID + ".thumb.png")})
	}
	return view
}

func (a *App) syncClothesline(state clothesline.State, err error) error {
	if err != nil {
		if !diskio.Committed(err) {
			return err
		}
		log.Printf("pinstill: clothesline cleanup or durability: %v", err)
	}
	if !mygo.App.IsReady() {
		return nil
	}
	mygo.RunOnMain(func() {
		a.mu.Lock()
		quitting := a.quitting
		a.mu.Unlock()
		if quitting || state.Revision < a.lineState.Revision {
			return
		}
		a.lineState = state
		window := a.lineWindow
		if state.Visible {
			if window == nil || window.IsDestroyed() {
				a.createClotheslineWindow()
				window = a.lineWindow
			} else if a.lineReady && !window.IsVisible() {
				window.ShowInactive()
			}
		} else if window != nil && a.lineReady {
			window.Hide()
		}
		if window != nil && a.lineReady {
			_ = EventLineChanged.Emit(window, a.lineView(state))
		}
	})
	return nil
}

func (a *App) lineStoreReady() error {
	if a.lineStore == nil {
		return errors.New("无法打开桌面晾绳的存储目录")
	}
	return nil
}

func (a *App) restoreClothesline() {
	if a.lineStore == nil {
		return
	}
	if err := a.syncClothesline(a.lineStore.Snapshot()); err != nil {
		log.Printf("pinstill: restore clothesline: %v", err)
	}
}

func (a *App) toggleClothesline() {
	if err := a.lineStoreReady(); err != nil {
		mygo.Dialog.Error("桌面晾绳", err.Error())
		return
	}
	if err := a.syncClothesline(a.lineStore.ToggleVisible()); err != nil {
		mygo.Dialog.Error("桌面晾绳", err.Error())
	}
}

func (a *App) hangPNG(pngBytes []byte) error {
	return a.addClotheslineImage(pngBytes, "截图")
}

func (a *App) addClotheslineImage(data []byte, name string) error {
	if err := a.lineStoreReady(); err != nil {
		return err
	}
	return a.syncClothesline(a.lineStore.Add(data, name))
}

func lineBounds(display mygo.Display) mygo.Rectangle {
	area := display.WorkArea
	if area.Width <= 0 || area.Height <= 0 {
		area = display.Bounds
	}
	const margin = 16
	width := max(1, min(928, area.Width-2*margin))
	height := max(1, min(230, area.Height-2*margin))
	return mygo.Rectangle{
		X:      area.X + (area.Width-width)/2,
		Y:      area.Y + min(margin, max(0, area.Height-height)),
		Width:  width,
		Height: height,
	}
}

func (a *App) createClotheslineWindow() {
	bounds := lineBounds(mygo.Screen.PrimaryDisplay())
	window := mygo.NewWindow(mygo.WindowOptions{
		Page:              mygo.PageOptions{Session: a.pageSession},
		Title:             "Pinstill 桌面晾绳",
		URL:               "/line.html",
		X:                 bounds.X,
		Y:                 bounds.Y,
		Width:             bounds.Width,
		Height:            bounds.Height,
		Hidden:            true,
		Panel:             true,
		AlwaysOnTop:       true,
		Frameless:         true,
		Transparent:       true,
		SkipTaskbar:       true,
		DisableResize:     true,
		DisableFullScreen: true,
		BackgroundColor:   "rgba(0,0,0,0)",
	})
	window.SetHasShadow(false)
	window.SetContentProtection(true)
	a.lineWindow = window
	a.lineReady = false
	window.OnReadyToShow(func() {
		if a.lineWindow != window {
			return
		}
		a.lineReady = true
		_ = EventLineChanged.Emit(window, a.lineView(a.lineState))
		if a.lineState.Visible {
			window.ShowInactive()
		}
	})
	window.OnClose(func(event *mygo.CloseEvent) {
		a.mu.Lock()
		quitting := a.quitting
		a.mu.Unlock()
		if !quitting {
			event.PreventDefault()
			go func() {
				if err := a.syncClothesline(a.lineStore.SetVisible(false)); err != nil {
					mygo.Dialog.Error("桌面晾绳", err.Error())
				}
			}()
		}
	})
}

func (s *CaptureService) ListClothesline() (LineView, error) {
	if err := s.app.lineStoreReady(); err != nil {
		return LineView{}, err
	}
	state, err := s.app.lineStore.Snapshot()
	return s.app.lineView(state), err
}

func (s *CaptureService) HideClothesline() error {
	if err := s.app.lineStoreReady(); err != nil {
		return err
	}
	return s.app.syncClothesline(s.app.lineStore.SetVisible(false))
}

func (s *CaptureService) HangClipboard() error {
	data := mygo.Clipboard.ReadImage()
	if len(data) == 0 {
		return errors.New("剪贴板中没有图片")
	}
	return s.app.addClotheslineImage(data, "剪贴板图片")
}

func (s *CaptureService) ImportClotheslineImage() (bool, error) {
	paths, err := mygo.Dialog.Open(mygo.OpenDialogOptions{
		Title: "选择要挂到晾绳的图片", ButtonLabel: "晾图",
		Filters: []mygo.FileFilter{{Name: "PNG、JPEG 图片", Extensions: []string{"png", "jpg", "jpeg"}}},
	})
	if err != nil || len(paths) == 0 {
		return false, err
	}
	data, err := diskio.ReadFile(paths[0], imagebudget.MaxEncodedBytes)
	if err != nil {
		return false, err
	}
	err = s.app.addClotheslineImage(data, filepath.Base(paths[0]))
	return err == nil, err
}

func (s *CaptureService) MoveClotheslineImage(id, before string) error {
	if err := s.app.lineStoreReady(); err != nil {
		return err
	}
	return s.app.syncClothesline(s.app.lineStore.Move(id, before))
}

func (s *CaptureService) RemoveClotheslineImage(id string) error {
	if err := s.app.lineStoreReady(); err != nil {
		return err
	}
	return s.app.syncClothesline(s.app.lineStore.Remove(id))
}

func (s *CaptureService) UseClotheslineImage(id, action string) (bool, error) {
	if err := s.app.lineStoreReady(); err != nil {
		return false, err
	}
	pngBytes, err := s.app.lineStore.Read(id)
	if err != nil {
		return false, err
	}
	switch action {
	case "copy":
		err = s.app.copyImagePNG(pngBytes)
		return err == nil, err
	case "pin":
		_, err = s.app.pinPNG(pngBytes, 0, 0)
		return err == nil, err
	case "save":
		path, err := s.app.savePNGBytes(pngBytes)
		return path != "", err
	default:
		return false, errors.New("未知的晾图操作")
	}
}

func (s *HistoryService) Hang(id string) error {
	data, err := s.app.historyPNG(id)
	if err != nil {
		return err
	}
	return s.app.hangPNG(data)
}

func (s *CaptureService) HangScrollResult(id string) error {
	result, err := s.app.scrollResultSnapshot(id)
	if err != nil {
		return err
	}
	if err := s.app.hangPNG(result.png); err != nil {
		return err
	}
	s.app.closeScrollResultSoon(result.window)
	return nil
}
