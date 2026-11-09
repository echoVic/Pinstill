package app

import (
	"bytes"
	"errors"
	"fmt"
	"image"
	"image/jpeg"
	"image/png"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/echoVic/Tupen/internal/capture"
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

// FinishRegion completes the region capture with the selection the user
// dragged on the overlay, in overlay-local CSS pixels. It is kept for
// compatibility and opens the editor; the overlay's action bar uses
// CompleteSelection instead.
func (s *CaptureService) FinishRegion(rect Rect) (CaptureResult, error) {
	return CaptureResult{}, s.app.completeSelection(rect, "annotate")
}

// CompleteSelection runs one action on the region the user selected in the
// overlay: annotate, copy, pin or save. The work happens on the desktop;
// only annotate brings the main window back.
func (s *CaptureService) CompleteSelection(rect Rect, action string) error {
	return s.app.completeSelection(rect, action)
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

// CaptureFullScreen captures the whole display under the pointer. Like
// FinishRegion it delivers the capture through capture:completed.
func (s *CaptureService) CaptureFullScreen() CaptureResult {
	s.app.captureFullScreen()
	return CaptureResult{}
}

// CopyImage puts a PNG on the system clipboard.
func (s *CaptureService) CopyImage(dataBase64 string) error {
	pngBytes, err := toPNG(dataBase64)
	if err != nil {
		return err
	}
	return s.app.copyImagePNG(pngBytes)
}

// ExportImage asks where to save the annotated image, converts it to the
// configured format, and writes it. It returns the path, or "" when the
// user canceled.
func (s *CaptureService) ExportImage(dataBase64 string, suggestedName string) (string, error) {
	settings := s.app.settingsSnapshot()
	raw, err := decodeImageBase64(dataBase64)
	if err != nil {
		return "", err
	}
	data, ext, err := normalizeExport(raw, settings)
	if err != nil {
		return "", err
	}
	name := replaceExtension(sanitizeExportName(suggestedName, settings), ext)
	defaultPath := name
	if folder, folderErr := expandHome(settings.Folder); folderErr == nil {
		if info, statErr := os.Stat(folder); statErr == nil && info.IsDir() {
			defaultPath = filepath.Join(folder, name)
		}
	}
	path, err := saveDialog(s.app, defaultPath, ext)
	if err != nil {
		return "", err
	}
	if path == "" {
		return "", nil
	}
	path = replaceExtension(path, ext)
	if err := os.WriteFile(path, data, 0o644); err != nil {
		return "", fmt.Errorf("写入文件失败: %w", err)
	}
	return path, nil
}

// SaveImageToFolder writes the annotated image into the configured folder
// without asking. It returns the written path.
func (s *CaptureService) SaveImageToFolder(dataBase64 string) (string, error) {
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
	path := uniquePath(filepath.Join(folder, name))
	if err := os.WriteFile(path, data, 0o644); err != nil {
		return "", fmt.Errorf("写入文件失败: %w", err)
	}
	s.app.toast("已保存到 "+path, "success")
	return path, nil
}

// PinImage opens an always-on-top pin window showing the image.
func (s *CaptureService) PinImage(dataBase64 string) (string, error) {
	pngBytes, err := toPNG(dataBase64)
	if err != nil {
		return "", err
	}
	return s.app.pinPNG(pngBytes, 0, 0)
}

// PinClipboard opens a pin window showing the image on the clipboard.
func (s *CaptureService) PinClipboard() (string, error) { return s.app.pinClipboard() }

// ClosePin closes a pin window.
func (s *CaptureService) ClosePin(id string) error { return s.app.closePin(id) }

// ApplySettings applies the user's settings and returns the ones that could
// not be applied, keyed by setting name.
func (s *CaptureService) ApplySettings(settings Settings) map[string]string {
	return s.app.applySettings(normalizeSettings(settings))
}

// PermissionGranted reports whether the OS lets the app record the screen.
func (s *CaptureService) PermissionGranted() bool { return capture.PermissionGranted() }

// OpenPermissionSettings opens the OS privacy pane for screen recording.
func (s *CaptureService) OpenPermissionSettings() error { return capture.OpenPermissionSettings() }

// OverlayInfo describes the display the overlay covers.
func (s *CaptureService) OverlayInfo() OverlayInfo { return s.app.overlayInfo() }

// ShowMainWindow reveals and focuses the main window.
func (s *CaptureService) ShowMainWindow() { s.app.showMain() }

// HistoryService reads and edits the persisted captures.
type HistoryService struct{ app *App }

// List returns persisted captures, newest first.
func (s *HistoryService) List() []HistoryEntry { return s.app.historyEntries() }

// Get loads one persisted capture for the editor.
func (s *HistoryService) Get(id string) (HistoryCapture, error) { return s.app.historyCapture(id) }

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
	case "toolbar", "clipboard", "pin", "editor":
	default:
		s.AfterCapture = "toolbar"
	}
	if s.Format != "JPEG" {
		s.Format = "PNG"
	}
	s.Quality = min(max(s.Quality, 10), 100)
	if strings.TrimSpace(s.FilenamePrefix) == "" {
		s.FilenamePrefix = "Tupen"
	}
	if strings.TrimSpace(s.Folder) == "" {
		s.Folder = "~/Pictures/Tupen"
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
	if actual == want {
		return raw, want, nil
	}
	img, _, err := image.Decode(bytes.NewReader(raw))
	if err != nil {
		return nil, "", fmt.Errorf("图片解码失败: %w", err)
	}
	var out bytes.Buffer
	if want == "jpg" {
		quality := min(max(settings.Quality, 10), 100)
		if err := jpeg.Encode(&out, img, &jpeg.Options{Quality: quality}); err != nil {
			return nil, "", fmt.Errorf("JPEG 编码失败: %w", err)
		}
		return out.Bytes(), "jpg", nil
	}
	if err := png.Encode(&out, img); err != nil {
		return nil, "", fmt.Errorf("PNG 编码失败: %w", err)
	}
	return out.Bytes(), "png", nil
}

// toPNG decodes an image and re-encodes it as PNG, for the clipboard and
// pin windows.
func toPNG(data string) ([]byte, error) {
	raw, err := decodeImageBase64(data)
	if err != nil {
		return nil, err
	}
	actual, err := detectImageFormat(raw)
	if err != nil {
		return nil, err
	}
	if actual == "png" {
		return raw, nil
	}
	img, _, err := image.Decode(bytes.NewReader(raw))
	if err != nil {
		return nil, fmt.Errorf("图片解码失败: %w", err)
	}
	var out bytes.Buffer
	if err := png.Encode(&out, img); err != nil {
		return nil, fmt.Errorf("PNG 编码失败: %w", err)
	}
	return out.Bytes(), nil
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
		prefix = "Tupen"
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
	path = replaceExtension(path, ext)
	if err := os.WriteFile(path, data, 0o644); err != nil {
		return "", fmt.Errorf("写入文件失败: %w", err)
	}
	return path, nil
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
