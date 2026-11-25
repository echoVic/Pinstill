package app

import (
	"bytes"
	"image"
	"time"

	"github.com/echoVic/Pinstill/internal/imagebudget"
	"github.com/egoist/mygo"
)

// Rect is a rectangle sent from a page: overlay-local CSS pixels for
// PrepareSelection, screen coordinates elsewhere.
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

// CaptureActionResult distinguishes a completed action from a canceled save.
type CaptureActionResult struct {
	Completed bool `json:"completed"`
	Cancelled bool `json:"cancelled"`
}

// OverlayWindow describes one visible desktop window projected into
// overlay-local CSS pixels, ordered front-to-back for hover snapping.
type OverlayWindow struct {
	X      float64 `json:"x"`
	Y      float64 `json:"y"`
	Width  float64 `json:"width"`
	Height float64 `json:"height"`
	Owner  string  `json:"owner,omitempty"`
	Title  string  `json:"title,omitempty"`
}

type OverlayDisplay struct {
	Rect
	Scale float64 `json:"scale"`
}

// OverlayInfo tells the overlay page where the selection surface is.
type OverlayInfo struct {
	Session     uint64           `json:"session,omitempty"`
	DisplayX    int              `json:"displayX"`
	DisplayY    int              `json:"displayY"`
	Width       int              `json:"width"`
	Height      int              `json:"height"`
	Scale       float64          `json:"scale"`
	CursorX     float64          `json:"cursorX"`
	CursorY     float64          `json:"cursorY"`
	HasCursor   bool             `json:"hasCursor,omitempty"`
	Windows     []OverlayWindow  `json:"windows,omitempty"`
	Desktop     *Rect            `json:"desktop,omitempty"`
	Displays    []OverlayDisplay `json:"displays,omitempty"`
	BackdropURL string           `json:"backdropUrl,omitempty"`
}

type OverlayPreviewPayload struct {
	Session uint64 `json:"session"`
	Rect    *Rect  `json:"rect,omitempty"`
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
	ID           string           `json:"id"`
	URL          string           `json:"url"`
	Width        float64          `json:"width"`
	Height       float64          `json:"height"`
	Opacity      float64          `json:"opacity"`
	Border       bool             `json:"border"`
	Presentation *PinPresentation `json:"presentation,omitempty"`
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

// capturePayload holds the capture identity and pixel size used by actions.
type capturePayload struct {
	ID        string
	HistoryID string
	Width     int
	Height    int
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

// OverlayCapturePayload hands a freshly captured selection back to the
// overlay page, which annotates it in place on the desktop.
type OverlayCapturePayload struct {
	ID     string `json:"id"`
	URL    string `json:"url"`
	Width  int    `json:"width"`
	Height int    `json:"height"`
	Rect   Rect   `json:"rect"`
}

// PinWindowState is the size and opacity the page reports for a pin.
type PinWindowState struct {
	Width   int     `json:"width"`
	Height  int     `json:"height"`
	Opacity float64 `json:"opacity"`
}

// PinArchiveEntry is a saved pin intentionally kept closed at startup.
type PinArchiveEntry struct {
	ID        string `json:"id"`
	URL       string `json:"url"`
	Width     int    `json:"width"`
	Height    int    `json:"height"`
	CreatedAt string `json:"createdAt"`
}

// ScrollInitPayload configures the scroll control window for one session.
type ScrollInitPayload struct {
	SessionID            string `json:"sessionId"`
	Rect                 Rect   `json:"rect"`
	Auto                 bool   `json:"auto"`
	AutoDenied           bool   `json:"autoDenied"`
	AccessibilityGranted bool   `json:"accessibilityGranted"`
}

// ScrollResultPayload tells the preview window which long image to show.
type ScrollResultPayload struct {
	ID        string `json:"id"`
	URL       string `json:"url"`
	Width     int    `json:"width"`
	Height    int    `json:"height"`
	Frames    int    `json:"frames"`
	Capped    bool   `json:"capped"`
	HistoryID string `json:"historyId,omitempty"`
}

// ScrollProgressPayload reports how a scrolling capture is going.
type ScrollProgressPayload struct {
	SessionID string `json:"sessionId"`
	Frames    int    `json:"frames"`
	Height    int    `json:"height"`
	Status    string `json:"status"`
	Auto      bool   `json:"auto"`
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
	TranslateKey    string   `json:"translateKey"`
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
		TranslateKey:   "CmdOrCtrl+Shift+3",
		OcrKey:         "CmdOrCtrl+Shift+O",
		AfterCapture:   "inplace",
		Format:         "PNG",
		Quality:        90,
		FilenamePrefix: "Pinstill",
		Folder:         "~/Pictures/Pinstill",
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
	// EventCaptureCancelled is emitted when the user cancels a capture.
	EventCaptureCancelled = mygo.NewEvent[CaptureCancelledPayload]("capture:cancelled")
	// EventCaptureFailed is emitted when capturing failed.
	EventCaptureFailed = mygo.NewEvent[CaptureFailedPayload]("capture:failed")
	// EventOverlayShown is emitted to the overlay window before it appears.
	EventOverlayShown   = mygo.NewEvent[OverlayInfo]("overlay:shown")
	EventOverlayPreview = mygo.NewEvent[OverlayPreviewPayload]("overlay:preview")
	EventOverlayHidden  = mygo.NewEvent[uint64]("overlay:hidden")
	// EventScrollInit is emitted to the scroll control window when a session
	// starts.
	EventScrollInit = mygo.NewEvent[ScrollInitPayload]("scroll:init")
	// EventScrollProgress reports the state of a scrolling capture.
	EventScrollProgress = mygo.NewEvent[ScrollProgressPayload]("scroll:progress")
	// EventScrollResult hands the stitched long image to the preview window.
	EventScrollResult = mygo.NewEvent[ScrollResultPayload]("scroll:result")
	// EventPinInit is emitted to a pin window to show its image.
	EventPinInit = mygo.NewEvent[PinInitPayload]("pin:init")
	// EventHistoryChanged is emitted when the persisted history changed.
	EventHistoryChanged = mygo.NewEvent[HistoryChangedPayload]("history:changed")
	// EventToast asks a page to show a toast.
	EventToast = mygo.NewEvent[ToastPayload]("toast")
)

// imageDimensions returns a PNG's pixel size without keeping it decoded.
func imageDimensions(data []byte) (int, int) {
	if len(data) > imagebudget.MaxEncodedBytes {
		return 0, 0
	}
	cfg, _, err := image.DecodeConfig(bytes.NewReader(data))
	if err != nil || imagebudget.CheckDimensions(cfg.Width, cfg.Height) != nil {
		return 0, 0
	}
	return cfg.Width, cfg.Height
}

// captureID makes a filename-safe, sortable capture identifier.
func captureID(t time.Time) string {
	return t.Format("20060102-150405") + "-" + randomSuffix()
}
