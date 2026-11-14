package app

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/echoVic/Pinstill/internal/capture"
	"github.com/echoVic/Pinstill/internal/imagebudget"
	"github.com/egoist/mygo"
)

// CaptureService is the page-facing capture workflow: screenshots, the
// clipboard, exports, pin windows and settings.
type CaptureService struct{ app *App }

// BeginRegion starts a region capture: the main window hides and the
// selection overlay appears.
func (s *CaptureService) BeginRegion() error { return s.app.beginRegion() }

// CancelRegion cancels the running region capture.
func (s *CaptureService) CancelRegion() { s.app.cancelRegion() }

// PrepareSelection captures the region the user selected and hands it back
// to the overlay, so it can annotate the capture in place on the desktop.
func (s *CaptureService) PrepareSelection(rect Rect) (OverlayCapturePayload, error) {
	return s.app.prepareSelection(rect)
}

// PrepareSelectionFast re-captures a region the page has already prepared
// (chrome hidden, one frame awaited): used when adjusting the selection.
func (s *CaptureService) PrepareSelectionFast(rect Rect) (OverlayCapturePayload, error) {
	return s.app.prepareSelectionFast(rect)
}

// FinishAnnotated completes an in-place annotation: it stores the composited
// image and runs the chosen action (copy, pin, save or cancel).
func (s *CaptureService) FinishAnnotated(action string, dataBase64 string) (CaptureActionResult, error) {
	return s.app.finishAnnotated(action, dataBase64)
}

// StartScroll begins a scrolling capture for the current selection.
func (s *CaptureService) StartScroll(rect Rect, autoScroll bool) error {
	return s.app.startScroll(rect, autoScroll)
}

// FinishScroll stops the scrolling capture and runs the chosen action.
func (s *CaptureService) FinishScroll(sessionID string, action string) error {
	return s.app.finishScrollFor(sessionID, action)
}

// SetScrollAuto switches a running scroll capture between auto and manual
// scrolling.
func (s *CaptureService) SetScrollAuto(sessionID string, enabled bool) error {
	return s.app.setScrollAutoFor(sessionID, enabled)
}

// SetScrollPaused pauses or resumes frame capturing for the running session.
func (s *CaptureService) SetScrollPaused(sessionID string, paused bool) error {
	return s.app.setScrollPausedFor(sessionID, paused)
}

// CopyScrollResult puts the stitched long image on the clipboard.
func (s *CaptureService) CopyScrollResult(id string) error { return s.app.copyScrollResultFor(id) }

// SaveScrollResult saves the stitched long image through the save dialog.
func (s *CaptureService) SaveScrollResult(id string) (string, error) {
	return s.app.saveScrollResultFor(id)
}

// PinScrollResult opens the stitched long image as a desktop pin.
func (s *CaptureService) PinScrollResult(id string) (string, error) {
	return s.app.pinScrollResultFor(id)
}

// CloseScrollResult closes the scroll result preview window.
func (s *CaptureService) CloseScrollResult(id string) error { return s.app.closeScrollResultFor(id) }

// ScrollAccessibilityGranted reports whether automatic scrolling is allowed.
func (s *CaptureService) ScrollAccessibilityGranted() bool { return capture.AccessibilityGranted() }

// OpenScrollAccessibilitySettings opens the Accessibility privacy pane.
func (s *CaptureService) OpenScrollAccessibilitySettings() error {
	return capture.OpenAccessibilitySettings()
}

// Settings returns the settings currently applied, so pages (the overlay)
// can honor the user's default action.
func (s *CaptureService) Settings() Settings { return s.app.settingsSnapshot() }

// ResizePin sets a desktop pin's size in DIPs.
func (s *CaptureService) ResizePin(id string, width int, height int) error {
	return s.app.resizePin(id, width, height)
}

// SetPinOpacity sets a desktop pin's opacity (10..100).
func (s *CaptureService) SetPinOpacity(id string, opacity float64) error {
	return s.app.setPinOpacity(id, opacity)
}

// CopyPin puts a pin's image on the clipboard.
func (s *CaptureService) CopyPin(id string) error { return s.app.copyPinImage(id) }

// SavePin saves a pin's image through the save dialog.
func (s *CaptureService) SavePin(id string) (string, error) { return s.app.savePinImage(id) }

// UpdatePinState persists the size and opacity the page reports.
func (s *CaptureService) UpdatePinState(id string, state PinWindowState) error {
	return s.app.updatePinState(id, state)
}

// CaptureFullScreen captures the whole display under the pointer, stores it
// in history and runs the configured after-capture action.
func (s *CaptureService) CaptureFullScreen() CaptureResult {
	s.app.captureFullScreen()
	return CaptureResult{}
}

// saveImageToFolder writes the annotated image into the configured folder
// without asking. It returns the written path.
func (s *CaptureService) saveImageToFolder(dataBase64 string) (string, error) {
	settings := s.app.settingsSnapshot()
	raw, err := decodeImageBase64(dataBase64)
	if err != nil {
		return "", err
	}
	data, ext, err := normalizeExport(raw, settings)
	if err != nil {
		return "", err
	}
	folder, err := expandHome(settings.Folder)
	if err != nil {
		return "", err
	}
	if err := os.MkdirAll(folder, 0o755); err != nil {
		return "", fmt.Errorf("保存文件夹不可用: %w", err)
	}
	name := replaceExtension(sanitizeExportName("", settings), ext)
	path, err := writeUniqueFile(filepath.Join(folder, name), data)
	if err != nil {
		return "", fmt.Errorf("写入文件失败: %w", err)
	}
	s.app.toast("已保存到 "+path, "success")
	return path, nil
}

// PinClipboard opens a pin window showing the image on the clipboard.
func (s *CaptureService) PinClipboard() (string, error) { return s.app.pinClipboard() }

// ClosePin closes a pin window.
func (s *CaptureService) ClosePin(id string) error { return s.app.closePin(id) }

// ListArchivedPins lists images deliberately kept closed at launch.
func (s *CaptureService) ListArchivedPins() []PinArchiveEntry {
	states := s.app.archivedPinStates()
	entries := make([]PinArchiveEntry, 0, len(states))
	for _, st := range states {
		entries = append(entries, PinArchiveEntry{ID: st.ID, URL: s.app.pinURL(st.File), Width: st.Width, Height: st.Height, CreatedAt: st.CreatedAt.Format(time.RFC3339)})
	}
	return entries
}

// RestoreArchivedPin reopens a saved pin only at the user's request.
func (s *CaptureService) RestoreArchivedPin(id string) error { return s.app.restoreArchivedPin(id) }

// ApplySettings applies the user's settings and returns the ones that could
// not be applied, keyed by setting name.
func (s *CaptureService) ApplySettings(settings Settings) map[string]string {
	return s.app.applySettings(normalizeSettings(settings))
}

// PermissionGranted reports whether the OS lets the app record the screen.
func (s *CaptureService) PermissionGranted() bool { return capture.PermissionGranted() }

// RequestScreenPermission asks the OS for screen recording access. macOS shows
// its system prompt when the grant is missing; the return value is the state
// after the request, which is still false until the user approves and the
// process restarts. The page falls back to opening System Settings when the
// request cannot be shown.
func (s *CaptureService) RequestScreenPermission() bool { return capture.RequestPermission() }

// OpenPermissionSettings opens the OS privacy pane for screen recording.
func (s *CaptureService) OpenPermissionSettings() error { return capture.OpenPermissionSettings() }

// LaunchAtLogin reports whether the OS currently launches Pinstill at login.
// It reads the real login item state, which can differ from the preference
// when the app runs from a path macOS refuses to register (e.g. not copied to
// /Applications).
func (s *CaptureService) LaunchAtLogin() bool { return mygo.App.OpenAtLogin() }

// OpenLoginItemsSettings opens the OS login items settings pane.
func (s *CaptureService) OpenLoginItemsSettings() error { return openLoginItemsSettings() }

// OverlayInfo describes the display the overlay covers.
func (s *CaptureService) OverlayInfo() OverlayInfo { return s.app.overlayInfo() }

// ShowMainWindow reveals and focuses the main window.
func (s *CaptureService) ShowMainWindow() { s.app.showMain() }

// HistoryService reads and edits the persisted captures.
type HistoryService struct{ app *App }

// List returns persisted captures, newest first.
func (s *HistoryService) List() []HistoryEntry { return s.app.historyEntries() }

// Get loads one persisted capture's preview metadata (the pages show the
// original through its URL; the type kept for compatibility).
func (s *HistoryService) Get(id string) (HistoryCapture, error) { return s.app.historyCapture(id) }

// Copy puts a persisted capture on the clipboard.
func (s *HistoryService) Copy(id string) error {
	pngBytes, err := s.app.historyPNG(id)
	if err != nil {
		return err
	}
	return s.app.copyImagePNG(pngBytes)
}

// Save saves a persisted capture through the system save dialog and returns
// the chosen path (empty when the user cancelled).
func (s *HistoryService) Save(id string) (string, error) {
	pngBytes, err := s.app.historyPNG(id)
	if err != nil {
		return "", err
	}
	return s.app.savePNGBytes(pngBytes)
}

// Pin opens a desktop pin showing a persisted capture.
func (s *HistoryService) Pin(id string) (string, error) {
	pngBytes, err := s.app.historyPNG(id)
	if err != nil {
		return "", err
	}
	width, height := imageDimensions(pngBytes)
	return s.app.pinPNG(pngBytes, width, height)
}

// Remove deletes one persisted capture.
func (s *HistoryService) Remove(id string) error { return s.app.removeHistory(id) }

// Clear deletes every persisted capture.
func (s *HistoryService) Clear() error { return s.app.clearHistory() }

// normalizeSettings repairs values that arrive from the page.
func normalizeSettings(s Settings) Settings {
	s.CaptureKey = strings.TrimSpace(s.CaptureKey)
	s.PinKey = strings.TrimSpace(s.PinKey)
	s.OcrKey = strings.TrimSpace(s.OcrKey)
	switch s.AfterCapture {
	case "inplace", "toolbar", "clipboard", "pin", "editor":
	default:
		s.AfterCapture = "inplace"
	}
	if s.AfterCapture == "toolbar" {
		s.AfterCapture = "inplace"
	}
	if s.Format != "JPEG" {
		s.Format = "PNG"
	}
	s.Quality = min(max(s.Quality, 10), 100)
	if strings.TrimSpace(s.FilenamePrefix) == "" {
		s.FilenamePrefix = "Pinstill"
	}
	if strings.TrimSpace(s.Folder) == "" {
		s.Folder = "~/Pictures/Pinstill"
	}
	s.RetentionDays = max(s.RetentionDays, 0)
	s.HistoryLimitMB = max(s.HistoryLimitMB, 0)
	if s.PinOpacity <= 0 {
		s.PinOpacity = 100
	}
	s.PinOpacity = min(max(s.PinOpacity, 30), 100)
	return s
}

// normalizeExport converts raw image bytes to the configured format and
// returns the bytes with the matching extension.
func normalizeExport(raw []byte, settings Settings) ([]byte, string, error) {
	actual, err := detectImageFormat(raw)
	if err != nil {
		return nil, "", err
	}
	want := "png"
	if settings.Format == "JPEG" {
		want = "jpg"
	}
	img, _, err := imagebudget.Decode(raw)
	if err != nil {
		return nil, "", fmt.Errorf("图片解码失败: %w", err)
	}
	if actual == want {
		return raw, want, nil
	}
	if want == "jpg" {
		quality := min(max(settings.Quality, 10), 100)
		out, err := imagebudget.EncodeJPEG(img, quality)
		if err != nil {
			return nil, "", fmt.Errorf("JPEG 编码失败: %w", err)
		}
		return out, "jpg", nil
	}
	out, err := imagebudget.EncodePNG(img)
	if err != nil {
		return nil, "", fmt.Errorf("PNG 编码失败: %w", err)
	}
	return out, "png", nil
}

// toPNG decodes an image and re-encodes it as PNG, for the clipboard and
// pin windows.
func toPNG(data string) ([]byte, error) {
	raw, err := decodeImageBase64(data)
	if err != nil {
		return nil, err
	}
	pngBytes, _, err := normalizeExport(raw, Settings{Format: "PNG"})
	return pngBytes, err
}

func detectImageFormat(raw []byte) (string, error) {
	if len(raw) >= 8 && bytes.Equal(raw[:8], []byte("\x89PNG\r\n\x1a\n")) {
		return "png", nil
	}
	if len(raw) >= 3 && raw[0] == 0xFF && raw[1] == 0xD8 && raw[2] == 0xFF {
		return "jpg", nil
	}
	return "", errors.New("不支持的图片格式")
}

// sanitizeExportName builds the file name from the prefix and today's date.
func sanitizeExportName(suggested string, settings Settings) string {
	suggested = strings.TrimSpace(suggested)
	if suggested != "" {
		base := strings.TrimSuffix(suggested, filepath.Ext(suggested))
		if base != "" {
			return base
		}
	}
	prefix := strings.TrimSpace(settings.FilenamePrefix)
	if prefix == "" {
		prefix = "Pinstill"
	}
	prefix = strings.Map(func(r rune) rune {
		if strings.ContainsRune(`/\:*?"<>|`, r) {
			return '-'
		}
		return r
	}, prefix)
	return fmt.Sprintf("%s-%s", prefix, time.Now().Format("2006-01-02"))
}

func replaceExtension(path, ext string) string {
	base := strings.TrimSuffix(path, filepath.Ext(path))
	if base == "" {
		base = path
	}
	return base + "." + ext
}

// writeDialogExport only overwrites the exact path confirmed by the dialog.
// An adjusted extension names a different destination, which must be new.
func writeDialogExport(selectedPath, ext string, data []byte) (string, error) {
	path := replaceExtension(selectedPath, ext)
	if path == selectedPath {
		if err := os.WriteFile(path, data, 0o644); err != nil {
			return "", fmt.Errorf("写入文件失败: %w", err)
		}
		return path, nil
	}
	file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o644)
	if os.IsExist(err) {
		return "", fmt.Errorf("调整扩展名后的文件 %q 已存在，未覆盖；请重新选择保存路径: %w", path, err)
	}
	if err != nil {
		return "", fmt.Errorf("写入文件失败: %w", err)
	}
	if _, err := file.Write(data); err != nil {
		_ = file.Close()
		_ = os.Remove(path)
		return "", fmt.Errorf("写入文件失败: %w", err)
	}
	if err := file.Close(); err != nil {
		_ = os.Remove(path)
		return "", fmt.Errorf("写入文件失败: %w", err)
	}
	return path, nil
}

// savePNGBytes converts and writes one PNG through the native save dialog,
// starting in the user's default save folder. It returns the path, or ""
// when the user canceled.
func (a *App) savePNGBytes(pngBytes []byte) (string, error) {
	settings := a.settingsSnapshot()
	data, ext, err := normalizeExport(pngBytes, settings)
	if err != nil {
		return "", err
	}
	name := replaceExtension(sanitizeExportName("", settings), ext)
	defaultPath := name
	if folder, folderErr := expandHome(settings.Folder); folderErr == nil {
		if info, statErr := os.Stat(folder); statErr == nil && info.IsDir() {
			defaultPath = filepath.Join(folder, name)
		}
	}
	path, err := saveDialog(a, defaultPath, ext)
	if err != nil || path == "" {
		return "", err
	}
	return writeDialogExport(path, ext, data)
}

// saveDialog opens the native save dialog and returns the chosen path, or
// "" when the user canceled.
func saveDialog(a *App, suggestedName, ext string) (string, error) {
	filter := mygo.FileFilter{Name: "PNG 图片", Extensions: []string{"png"}, MIMETypes: []string{"image/png"}}
	if ext == "jpg" {
		filter = mygo.FileFilter{Name: "JPEG 图片", Extensions: []string{"jpg", "jpeg"}, MIMETypes: []string{"image/jpeg"}}
	}
	a.mu.Lock()
	parent := a.main
	a.mu.Unlock()
	return mygo.Dialog.Save(mygo.SaveDialogOptions{
		Parent:      parent,
		Title:       "保存截图",
		DefaultPath: suggestedName,
		ButtonLabel: "保存",
		Filters:     []mygo.FileFilter{filter},
	})
}
