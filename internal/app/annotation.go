package app

import (
	"crypto/sha256"
	"errors"
	"fmt"
	"log"
	"os"
	"path/filepath"

	"github.com/echoVic/Pinstill/internal/imagebudget"
)

// The caller holds a.mu. Only files owned by the overlay are released; a
// separate result preview keeps its own file until its window closes.
func (a *App) releaseOverlayCacheLocked() error {
	a.annotationDigest = [32]byte{}
	a.annotationPayload = capturePayload{}
	a.overlayWindows = nil
	a.overlayCursorX = 0
	a.overlayCursorY = 0
	a.overlayHasCursor = false
	a.clearOverlayRevealLocked()
	return a.releaseCacheFileLocked(&a.overlaySessionFile)
}

func (a *App) releaseCacheFileLocked(file *string) error {
	if *file == "" {
		return nil
	}
	if err := os.Remove(filepath.Join(a.sessionDir, *file)); err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("无法清理截图预览缓存，请检查磁盘权限后重试: %w", err)
	}
	*file = ""
	return nil
}

func (a *App) writePreviewLocked(file *string, pngBytes []byte) (string, error) {
	return a.writePreviewLockedWith(file, pngBytes, func(f *os.File, data []byte) error { _, err := f.Write(data); return err })
}

func (a *App) writePreviewLockedWith(file *string, pngBytes []byte, write func(*os.File, []byte) error) (string, error) {
	if err := a.releasePendingPreviewsLocked(); err != nil {
		return "", err
	}
	if *file != "" {
		info, err := os.Lstat(filepath.Join(a.sessionDir, *file))
		if err != nil && !errors.Is(err, os.ErrNotExist) {
			return "", err
		}
		if err == nil && !info.Mode().IsRegular() {
			return "", errors.New("预览缓存不是普通文件，请检查缓存目录")
		}
	}
	f, err := os.CreateTemp(a.sessionDir, "preview-*.png")
	if err != nil {
		return "", err
	}
	staged := filepath.Base(f.Name())
	cleanup := func() error {
		if err := a.releaseCacheFileLocked(&staged); err != nil {
			a.pendingPreviewFiles = append(a.pendingPreviewFiles, staged)
			return err
		}
		return nil
	}
	err = write(f, pngBytes)
	closeErr := f.Close()
	if err = errors.Join(err, closeErr); err != nil {
		return "", errors.Join(err, cleanup())
	}
	if err := a.releaseCacheFileLocked(file); err != nil {
		return "", errors.Join(err, cleanup())
	}
	*file = staged
	return *file, nil
}

func (a *App) releasePendingPreviewsLocked() error {
	for len(a.pendingPreviewFiles) > 0 {
		pending := a.pendingPreviewFiles[0]
		if err := a.releaseCacheFileLocked(&pending); err != nil {
			return err
		}
		a.pendingPreviewFiles = a.pendingPreviewFiles[1:]
	}
	return nil
}

func reportCacheCleanup(err error) {
	if err != nil {
		log.Printf("pinstill: preview cache cleanup: %v", err)
	}
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
	if action != "copy" && action != "pin" && action != "save" {
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
	if _, err := imagebudget.DecodePNG(raw); err != nil {
		return CaptureActionResult{}, fmt.Errorf("标注后的图片不是有效 PNG: %w", err)
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
		reportCacheCleanup(a.releaseOverlayCacheLocked())
	}
	a.mu.Unlock()
	if current && (err != nil || !result.Completed) && overlay != nil {
		overlay.SetOpacity(1)
		overlay.Show()
		overlay.Focus()
	}
	return result, err
}
