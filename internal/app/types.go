package app

import (
	"bytes"
	"image"
	"time"

	"github.com/egoist/mygo"
)

// Rect is a rectangle sent from a page: overlay-local CSS pixels for
// FinishRegion, screen coordinates elsewhere.
type Rect struct {
	X      float64 `json:"x"`
	Y      float64 `json:"y"`
	Width  float64 `json:"width"`
	Height float64 `json:"height"`
}

// CaptureResult identifies a capture handled by the app.
type CaptureResult struct {
	ID string `json:"id"`
}

// OverlayInfo tells the overlay page where the selection surface is.
type OverlayInfo struct {
	DisplayX int     `json:"displayX"`
	DisplayY int     `json:"displayY"`
	Width    int     `json:"width"`
	Height   int     `json:"height"`
	Scale    float64 `json:"scale"`
}

// CaptureCompletedPayload is sent to the main window when a capture was
// written and is ready to show in the editor.
type CaptureCompletedPayload struct {
	ID        string `json:"id"`
	URL       string `json:"url"`
	ThumbURL  string `json:"thumbUrl,omitempty"`
	HistoryID string `json:"historyId,omitempty"`
	Width     int    `json:"width"`
	Height    int    `json:"height"`
	Source    string `json:"source"`
}

// CaptureCancelledPayload reports a capture the user backed out of.
type CaptureCancelledPayload struct {
	Reason string `json:"reason"`
}

// CaptureFailedPayload reports a failed capture. Permission is true when the
// failure is most likely the missing screen recording permission.
type CaptureFailedPayload struct {
	Message    string `json:"message"`
	Permission bool   `json:"permission"`
}

// PinInitPayload configures a pin window that has just been created.
type PinInitPayload struct {
	ID      string  `json:"id"`
	URL     string  `json:"url"`
	Width   float64 `json:"width"`
	Height  float64 `json:"height"`
	Opacity float64 `json:"opacity"`
	Border  bool    `json:"border"`
}

// HistoryChangedPayload announces that the capture history changed.
type HistoryChangedPayload struct {
	Count int `json:"count"`
}

// ToastPayload asks a page to show a transient status message.
type ToastPayload struct {
	Text string `json:"text"`
	Kind string `json:"kind"`
}

// HistoryEntry is one persisted capture shown in the history view.
type HistoryEntry struct {
	ID        string `json:"id"`
	CreatedAt string `json:"createdAt"`
	Width     int    `json:"width"`
	Height    int    `json:"height"`
	Size      int64  `json:"size"`
	URL       string `json:"url"`
	ThumbURL  string `json:"thumbUrl"`
}

// HistoryCapture is a persisted capture loaded into the editor.
type HistoryCapture struct {
	ID     string `json:"id"`
	Width  int    `json:"width"`
	Height int    `json:"height"`
	URL    string `json:"url"`
}

// Settings configures the capture workflow. The frontend owns the persisted
// values and pushes them to Go whenever they change.
type Settings struct {
	CaptureKey      string   `json:"captureKey"`
	PinKey          string   `json:"pinKey"`
	OcrKey          string   `json:"ocrKey"`
	AfterCapture    string   `json:"afterCapture"`
	Format          string   `json:"format"`
	Quality         int      `json:"quality"`
	FilenamePrefix  string   `json:"filenamePrefix"`
	Folder          string   `json:"folder"`
	IncludeCursor   bool     `json:"includeCursor"`
	Sound           bool     `json:"sound"`
	LaunchAtLogin   bool     `json:"launchAtLogin"`
	KeepHistory     bool     `json:"keepHistory"`
	RetentionDays   int      `json:"retentionDays"`
	HistoryLimitMB  int      `json:"historyLimitMB"`
	Tooltip         bool     `json:"tooltip"`
	PinOpacity      float64  `json:"pinOpacity"`
	PinBorder       bool     `json:"pinBorder"`
	IgnoredSettings []string `json:"-"`
}

// DefaultSettings returns the settings the app runs with until the page
// pushes the user's own.
func DefaultSettings() Settings {
	return Settings{
		CaptureKey:     "CmdOrCtrl+Shift+1",
		PinKey:         "CmdOrCtrl+Shift+2",
		OcrKey:         "CmdOrCtrl+Shift+O",
		AfterCapture:   "editor",
		Format:         "PNG",
		Quality:        90,
		FilenamePrefix: "Tupen",
		Folder:         "~/Pictures/Tupen",
		IncludeCursor:  false,
		Sound:          true,
		LaunchAtLogin:  false,
		KeepHistory:    true,
		RetentionDays:  30,
		HistoryLimitMB: 2048,
		Tooltip:        true,
		PinOpacity:     100,
		PinBorder:      true,
	}
}

// Events the pages listen to with the untyped mygo-runtime API.
var (
	// EventCaptureCompleted is emitted to the main window with the new
	// capture, ready to be drawn in the editor.
	EventCaptureCompleted = mygo.NewEvent[CaptureCompletedPayload]("capture:completed")
	// EventCaptureCancelled is emitted when the user cancels a capture.
	EventCaptureCancelled = mygo.NewEvent[CaptureCancelledPayload]("capture:cancelled")
	// EventCaptureFailed is emitted when capturing failed.
	EventCaptureFailed = mygo.NewEvent[CaptureFailedPayload]("capture:failed")
	// EventOverlayShown is emitted to the overlay window before it appears.
	EventOverlayShown = mygo.NewEvent[OverlayInfo]("overlay:shown")
	// EventPinInit is emitted to a pin window to show its image.
	EventPinInit = mygo.NewEvent[PinInitPayload]("pin:init")
	// EventHistoryChanged is emitted when the persisted history changed.
	EventHistoryChanged = mygo.NewEvent[HistoryChangedPayload]("history:changed")
	// EventToast asks a page to show a toast.
	EventToast = mygo.NewEvent[ToastPayload]("toast")
)

// imageDimensions returns a PNG's pixel size without keeping it decoded.
func imageDimensions(png []byte) (int, int) {
	cfg, _, err := image.DecodeConfig(bytes.NewReader(png))
	if err != nil {
		return 0, 0
	}
	return cfg.Width, cfg.Height
}

// captureID makes a filename-safe, sortable capture identifier.
func captureID(t time.Time) string {
	return t.Format("20060102-150405") + "-" + randomSuffix()
}
