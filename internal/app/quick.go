package app

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"image"
	"image/draw"
	"log"
	"os"
	"time"

	"github.com/echoVic/Pinstill/internal/imagebudget"
	"github.com/egoist/mygo"
	xdraw "golang.org/x/image/draw"
)

type QuickCaptureInfo struct {
	ID         string `json:"id"`
	Thumbnail  string `json:"thumbnail"`
	Width      int    `json:"width"`
	Height     int    `json:"height"`
	Completion string `json:"completion"`
}

type quickSession struct {
	info    QuickCaptureInfo
	png     []byte
	payload capturePayload
	window  *mygo.Window
	loading *time.Timer
}

func quickThumbnail(data []byte) (string, error) {
	source, err := imagebudget.DecodePNG(data)
	if err != nil {
		return "", err
	}
	bounds := source.Bounds()
	width, height := fitPinSize(bounds.Dx(), bounds.Dy(), 320, 320)
	preview := image.NewNRGBA(image.Rect(0, 0, width, height))
	xdraw.ApproxBiLinear.Scale(preview, preview.Bounds(), source, bounds, draw.Src, nil)
	encoded, err := imagebudget.EncodePNG(preview)
	if err != nil {
		return "", err
	}
	return "data:image/png;base64," + base64.StdEncoding.EncodeToString(encoded), nil
}

func (a *App) postQuickCapture(generation uint64, action string, png []byte, payload capturePayload) {
	mode := os.Getenv("PINSTILL_SELFTEST")
	if (action != "copy" && action != "save") || !mygo.App.IsReady() || (mode != "" && mode != "quick") {
		return
	}
	thumbnail, err := quickThumbnail(png)
	if err != nil {
		log.Printf("pinstill: quick capture thumbnail: %v", err)
		return
	}
	item := &quickSession{
		info: QuickCaptureInfo{
			ID: fmt.Sprintf("quick-%d", generation), Thumbnail: thumbnail,
			Width: payload.Width, Height: payload.Height, Completion: action,
		},
		png: png, payload: payload,
	}
	mygo.RunOnMain(func() {
		a.mu.Lock()
		current := !a.quitting && !a.capturing && a.captureGeneration == generation
		a.mu.Unlock()
		if !current {
			return
		}
		a.hideQuickCapture()
		display := mygo.Screen.DisplayNearestPoint(mygo.Screen.CursorScreenPoint())
		area := display.WorkArea
		if area.Width <= 0 || area.Height <= 0 {
			area = display.Bounds
		}
		if area.Width < 1 || area.Height < 1 {
			return
		}
		const margin = 16
		width, height := min(area.Width, 312), min(area.Height, 116)
		window := mygo.NewWindow(mygo.WindowOptions{
			Page: mygo.PageOptions{Session: a.pageSession}, Title: "Pinstill 截图快捷操作",
			URL: "/quick.html", Panel: true, Frameless: true, AlwaysOnTop: true,
			Hidden: true, SkipTaskbar: true, Transparent: true, BackgroundColor: "#00000000",
			DisableResize: true, DisableMinimize: true, DisableMaximize: true, DisableFullScreen: true,
			Width: width, Height: height,
			X: area.X + max(0, area.Width-width-margin), Y: area.Y + max(0, area.Height-height-margin),
		})
		window.SetContentProtection(true)
		window.SetHasShadow(true)
		item.window = window
		window.OnClosed(func() { a.takeQuickCapture(item, false) })
		a.mu.Lock()
		current = !a.quitting && !a.capturing && a.captureGeneration == generation
		if current {
			a.quick = item
			item.loading = time.AfterFunc(12*time.Second, func() { a.closeLoadingQuickCapture(item) })
		}
		a.mu.Unlock()
		if !current {
			window.Destroy()
		}
	})
}

func (a *App) closeLoadingQuickCapture(item *quickSession) {
	mygo.RunOnMain(func() {
		a.mu.Lock()
		loading := a.quick == item && item.loading != nil
		a.mu.Unlock()
		if loading && a.takeQuickCapture(item, false) {
			item.window.Destroy()
		}
	})
}

func (a *App) takeQuickCapture(item *quickSession, requireIdle bool) bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	if item == nil || a.quick != item || (requireIdle && a.quickAction == item) {
		return false
	}
	a.quick = nil
	if item.loading != nil {
		item.loading.Stop()
		item.loading = nil
	}
	return true
}

func (a *App) hideQuickCapture() {
	a.mu.Lock()
	item := a.quick
	a.mu.Unlock()
	if a.takeQuickCapture(item, false) && item.window != nil {
		item.window.Destroy()
	}
}

func (a *App) quickCaptureLocked(caller *mygo.Window, id string) *quickSession {
	item := a.quick
	if a.quitting || a.capturing || item == nil || caller != item.window || (id != "" && id != item.info.ID) {
		return nil
	}
	return item
}

func (a *App) runQuickAction(caller *mygo.Window, id, action string) (CaptureActionResult, error) {
	if action != "copy" && action != "save" && action != "pin" && action != "hang" {
		return CaptureActionResult{}, errors.New("无效的截图快捷操作")
	}
	a.mu.Lock()
	item := a.quickCaptureLocked(caller, id)
	if id == "" || item == nil || a.quickAction != nil {
		a.mu.Unlock()
		return CaptureActionResult{}, errors.New("这张截图已关闭或正在处理")
	}
	a.quickAction = item
	apply := a.captureAction
	a.mu.Unlock()
	defer func() {
		a.mu.Lock()
		a.quickAction = nil
		a.mu.Unlock()
	}()
	if apply == nil {
		apply = a.applyCaptureAction
	}
	return apply(action, item.png, item.payload)
}

func (s *CaptureService) QuickCapture(ctx context.Context) (QuickCaptureInfo, error) {
	a := s.app
	a.mu.Lock()
	defer a.mu.Unlock()
	if item := a.quickCaptureLocked(mygo.CallerWindow(ctx), ""); item != nil {
		return item.info, nil
	}
	return QuickCaptureInfo{}, errors.New("这张截图已关闭")
}

func (s *CaptureService) ShowQuickCapture(ctx context.Context, id string) {
	if id == "" {
		return
	}
	a, caller := s.app, mygo.CallerWindow(ctx)
	mygo.RunOnMain(func() {
		a.mu.Lock()
		item := a.quickCaptureLocked(caller, id)
		if item == nil || item.loading == nil {
			a.mu.Unlock()
			return
		}
		item.loading.Stop()
		item.loading = nil
		a.mu.Unlock()
		caller.ShowInactive()
	})
}

func (s *CaptureService) QuickAction(ctx context.Context, id, action string) (CaptureActionResult, error) {
	return s.app.runQuickAction(mygo.CallerWindow(ctx), id, action)
}

func (s *CaptureService) DismissQuickCapture(ctx context.Context, id string) {
	if id == "" {
		return
	}
	a := s.app
	a.mu.Lock()
	item := a.quickCaptureLocked(mygo.CallerWindow(ctx), id)
	a.mu.Unlock()
	if a.takeQuickCapture(item, true) && item.window != nil {
		item.window.Destroy()
	}
}
