package app

import (
	"crypto/sha256"
	"errors"
	"fmt"
)

func (a *App) releaseOverlayCaptureLocked() {
	a.overlayPreviewID = ""
	a.overlayPreviewPNG = nil
	a.annotationDigest = [32]byte{}
	a.annotationPayload = capturePayload{}
	a.overlayWindows = nil
	a.overlayCursorX = 0
	a.overlayCursorY = 0
	a.overlayHasCursor = false
	a.overlaySurfaces = nil
	a.overlayInteraction = 0
	a.overlayDesktop = nil
	a.clearOverlayFreezeLocked()
	a.overlaySource = nil
	a.overlayCommitted = false
	a.clearOverlayRevealLocked()
}

func (a *App) finishAnnotation(action, dataBase64 string) (CaptureActionResult, error) {
	a.finishMu.Lock()
	defer a.finishMu.Unlock()
	if action == "cancel" {
		if !a.cancelRegion() {
			return CaptureActionResult{}, errors.New("滚动截图进行中，请使用滚动控制窗口取消")
		}
		return CaptureActionResult{Completed: true}, nil
	}
	if action != "copy" && action != "pin" && action != "save" && action != "hang" {
		return CaptureActionResult{}, fmt.Errorf("未知的标注动作 %q", action)
	}
	a.mu.Lock()
	if !a.capturing || a.scroll != nil {
		a.mu.Unlock()
		return CaptureActionResult{}, errors.New("没有进行中的截图")
	}
	a.captureGeneration++
	generation, overlay := a.captureGeneration, a.overlay
	a.mu.Unlock()
	raw, err := decodeImageBase64(dataBase64)
	if err != nil {
		return CaptureActionResult{}, err
	}
	digest := sha256.Sum256(raw)
	a.mu.Lock()
	payload := a.annotationPayload
	if a.annotationDigest != digest {
		payload = capturePayload{}
	}
	a.mu.Unlock()
	if payload.ID == "" {
		payload, err = a.storeCapture(raw)
		if err != nil {
			return CaptureActionResult{}, err
		}
		if !a.selectionIsCurrent(generation) {
			return CaptureActionResult{}, errors.New("截图请求已取消")
		}
		a.mu.Lock()
		a.annotationDigest, a.annotationPayload = digest, payload
		a.mu.Unlock()
	}
	if !a.selectionIsCurrent(generation) {
		return CaptureActionResult{}, errors.New("截图请求已取消")
	}
	if overlay != nil {
		overlay.SetOpacity(0)
		overlay.Hide()
	}
	actionFn := a.captureAction
	if actionFn == nil {
		actionFn = a.applyCaptureAction
	}
	result, err := actionFn(action, raw, payload)
	a.mu.Lock()
	current := a.capturing && a.captureGeneration == generation
	if current && err == nil && result.Completed {
		a.capturing, a.restoreMainAfterCapture = false, false
		a.releaseOverlayCaptureLocked()
	}
	a.mu.Unlock()
	if current && (err != nil || !result.Completed) && overlay != nil {
		overlay.SetOpacity(1)
		overlay.Show()
		overlay.Focus()
	}
	return result, err
}
