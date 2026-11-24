package app

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"image"
	"log"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/echoVic/Pinstill/internal/capture"
	"github.com/echoVic/Pinstill/internal/imagebudget"
	"github.com/egoist/mygo"
)

func (a *App) freezeOverlayDesktop(ctx context.Context, displays []mygo.Display, hidMain bool) *capture.DesktopSnapshot {
	if os.Getenv("PINSTILL_SELFTEST_FAKE_CAPTURE") != "" || os.Getenv("PINSTILL_SELFTEST_SCROLL_SIM") != "" {
		return nil
	}
	if hidMain {
		timer := time.NewTimer(32 * time.Millisecond)
		defer timer.Stop()
		select {
		case <-ctx.Done():
			return nil
		case <-timer.C:
		}
	}
	surfaces := make([]capture.DisplaySurface, 0, len(displays))
	for _, display := range displays {
		surfaces = append(surfaces, capture.DisplaySurface{Bounds: asCaptureRect(captureOverlayBounds(display)), Scale: display.ScaleFactor})
	}
	options := capture.CaptureOptions{IncludeCursor: a.settingsSnapshot().IncludeCursor}
	desktop, err := capture.FreezeScreen(ctx, surfaces, options)
	if err != nil && ctx.Err() == nil {
		log.Printf("pinstill: desktop snapshot: %v", err)
	}
	return desktop
}

func (a *App) clearOverlayFreezeLocked() {
	if a.overlayFreeze != nil {
		a.overlayFreeze()
		a.overlayFreeze = nil
	}
}

func (a *App) serveOverlayBackdrop(w http.ResponseWriter, r *http.Request) {
	sessionText, frameName, _ := strings.Cut(strings.TrimPrefix(r.URL.Path, "/d/"), "/")
	session, sessionErr := strconv.ParseUint(sessionText, 10, 64)
	index, frameErr := strconv.Atoi(strings.TrimSuffix(frameName, ".png"))
	if sessionErr != nil || frameErr != nil || !strings.HasSuffix(frameName, ".png") {
		http.NotFound(w, r)
		return
	}
	a.mu.Lock()
	var data []byte
	if session != 0 && session == a.overlayInteraction && !a.quitting {
		data = a.overlayDesktop.PNG(index)
	}
	a.mu.Unlock()
	if len(data) == 0 {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Content-Type", "image/png")
	http.ServeContent(w, r, frameName, time.Time{}, bytes.NewReader(data))
}

func (a *App) selectionImage(ctx context.Context, desktop *capture.DesktopSnapshot, region capture.Rect, displays []capture.DisplaySurface, generation uint64, chromeDelay time.Duration) (image.Image, []byte, error) {
	if desktop != nil {
		frame, err := desktop.Crop(ctx, region)
		if err != nil {
			return nil, nil, err
		}
		if err := ctx.Err(); err != nil {
			return nil, nil, err
		}
		data, err := imagebudget.EncodePNG(frame)
		return frame, data, err
	}
	timer := time.NewTimer(chromeDelay)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return nil, nil, ctx.Err()
	case <-timer.C:
	}
	if !a.selectionIsCurrent(generation) {
		return nil, nil, errors.New("截图请求已取消")
	}
	options := capture.CaptureOptions{IncludeCursor: a.settingsSnapshot().IncludeCursor}
	data, err := a.captureRegionOnDisplays(ctx, region, options, displays)
	if err != nil {
		return nil, nil, err
	}
	if err := ctx.Err(); err != nil {
		return nil, nil, err
	}
	frame, err := imagebudget.DecodePNG(data)
	if err != nil {
		return nil, nil, fmt.Errorf("截图数据无效: %w", err)
	}
	return frame, data, nil
}
