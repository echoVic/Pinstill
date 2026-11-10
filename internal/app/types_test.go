package app

import (
	"encoding/json"
	"reflect"
	"regexp"
	"slices"
	"testing"
	"time"
)

func TestImageDimensions(t *testing.T) {
	tests := []struct {
		name       string
		raw        []byte
		wantWidth  int
		wantHeight int
	}{
		{name: "valid PNG", raw: gnPNGData(t, 123, 45), wantWidth: 123, wantHeight: 45},
		{name: "valid JPEG", raw: gnJPEGData(t, 7, 9, 80), wantWidth: 7, wantHeight: 9},
		{name: "invalid bytes", raw: []byte("not an image at all"), wantWidth: 0, wantHeight: 0},
		{name: "PNG signature only", raw: gnPNGMagic(), wantWidth: 0, wantHeight: 0},
		{name: "empty", raw: nil, wantWidth: 0, wantHeight: 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			width, height := imageDimensions(tt.raw)
			if width != tt.wantWidth || height != tt.wantHeight {
				t.Fatalf("imageDimensions() = %dx%d, want %dx%d", width, height, tt.wantWidth, tt.wantHeight)
			}
		})
	}
}

func TestCaptureID(t *testing.T) {
	shape := regexp.MustCompile(`^[0-9]{8}-[0-9]{6}-[0-9a-f]{4}$`)
	createdAt := time.Date(2026, 11, 9, 12, 34, 56, 0, time.UTC)

	id := captureID(createdAt)
	if !shape.MatchString(id) {
		t.Fatalf("captureID() = %q, want 20060102-150405-[0-9a-f]{4}", id)
	}
	if len(id) != 20 {
		t.Fatalf("captureID() = %q has length %d, want 20", id, len(id))
	}
	if prefix := "20261109-123456-"; id[:len(prefix)] != prefix {
		t.Fatalf("captureID() = %q, want prefix %q", id, prefix)
	}

	// The wall clock of the passed time is used as-is, not converted to UTC.
	local := time.Date(2026, 11, 9, 12, 34, 56, 0, time.FixedZone("UTC+5", 5*60*60))
	if got := captureID(local); got[:15] != "20261109-123456" {
		t.Fatalf("captureID(local) = %q, want wall clock date/time", got)
	}

	// Two IDs in the same second must differ; retry a few times so the rare
	// random suffix collision does not make the test flaky.
	distinct := false
	for attempt := 0; attempt < 8 && !distinct; attempt++ {
		if captureID(createdAt) != captureID(createdAt) {
			distinct = true
		}
	}
	if !distinct {
		t.Fatal("captureID() produced the same ID eight times for the same second")
	}
}

func TestEventNamesMatchContract(t *testing.T) {
	// Frozen by work/plan.md section 3.5 (plus pin:init, used by pin.html).
	tests := []struct {
		name  string
		event interface{ Name() string }
	}{
		{name: "capture:cancelled", event: EventCaptureCancelled},
		{name: "capture:failed", event: EventCaptureFailed},
		{name: "overlay:shown", event: EventOverlayShown},
		{name: "scroll:init", event: EventScrollInit},
		{name: "scroll:progress", event: EventScrollProgress},
		{name: "scroll:result", event: EventScrollResult},
		{name: "pin:init", event: EventPinInit},
		{name: "history:changed", event: EventHistoryChanged},
		{name: "toast", event: EventToast},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.event.Name(); got != tt.name {
				t.Fatalf("event name = %q, want %q", got, tt.name)
			}
		})
	}
}

// gnJSONKeys returns the sorted top-level JSON keys of v.
func gnJSONKeys(t *testing.T, v any) []string {
	t.Helper()
	data, err := json.Marshal(v)
	if err != nil {
		t.Fatalf("json.Marshal(%T) error = %v", v, err)
	}
	var object map[string]json.RawMessage
	if err := json.Unmarshal(data, &object); err != nil {
		t.Fatalf("json.Unmarshal(%T) error = %v", v, err)
	}
	keys := make([]string, 0, len(object))
	for key := range object {
		keys = append(keys, key)
	}
	slices.Sort(keys)
	return keys
}

func TestSettingsJSONContract(t *testing.T) {
	// The frontend pushes Settings as camelCase JSON (work/plan.md 3.4).
	want := []string{
		"afterCapture",
		"captureKey",
		"filenamePrefix",
		"folder",
		"format",
		"historyLimitMB",
		"includeCursor",
		"keepHistory",
		"launchAtLogin",
		"ocrKey",
		"pinBorder",
		"pinKey",
		"pinOpacity",
		"quality",
		"retentionDays",
		"sound",
		"tooltip",
	}
	if got := gnJSONKeys(t, DefaultSettings()); !slices.Equal(got, want) {
		t.Fatalf("Settings JSON keys = %v, want %v", got, want)
	}

	// Round-trip a frontend-style payload with an unknown field: known fields
	// decode, the unknown one must not break decoding.
	payload := []byte(`{
		"captureKey": "CmdOrCtrl+Shift+4",
		"afterCapture": "pin",
		"format": "JPEG",
		"quality": 75,
		"pinOpacity": 40,
		"unknownField": true
	}`)
	var settings Settings
	if err := json.Unmarshal(payload, &settings); err != nil {
		t.Fatalf("json.Unmarshal(Settings) error = %v", err)
	}
	if settings.CaptureKey != "CmdOrCtrl+Shift+4" || settings.AfterCapture != "pin" ||
		settings.Format != "JPEG" || settings.Quality != 75 || settings.PinOpacity != 40 {
		t.Fatalf("decoded Settings = %+v", settings)
	}
}

func TestPayloadJSONContract(t *testing.T) {
	tests := []struct {
		name  string
		value any
		want  []string
	}{
		{name: "capture cancelled", value: CaptureCancelledPayload{Reason: "user"}, want: []string{"reason"}},
		{name: "capture failed", value: CaptureFailedPayload{Message: "boom", Permission: true}, want: []string{"message", "permission"}},
		{
			name:  "overlay info",
			value: OverlayInfo{DisplayX: 0, DisplayY: 0, Width: 1920, Height: 1080, Scale: 2},
			want:  []string{"displayX", "displayY", "height", "scale", "width"},
		},
		{
			name:  "pin init",
			value: PinInitPayload{ID: "pin-1", URL: "shot://localhost/s/pin-1.png", Width: 100, Height: 50, Opacity: 100, Border: true},
			want:  []string{"border", "height", "id", "opacity", "url", "width"},
		},
		{
			name:  "scroll init",
			value: ScrollInitPayload{Rect: Rect{X: 1, Y: 2, Width: 3, Height: 4}, Auto: true, AutoDenied: false, AccessibilityGranted: true},
			want:  []string{"accessibilityGranted", "auto", "autoDenied", "rect"},
		},
		{
			name:  "scroll result",
			value: ScrollResultPayload{URL: "shot://localhost/s/x.png", Width: 400, Height: 2300, Frames: 6, Capped: false, HistoryID: "x"},
			want:  []string{"capped", "frames", "height", "historyId", "url", "width"},
		},
		{name: "history changed", value: HistoryChangedPayload{Count: 3}, want: []string{"count"}},
		{name: "toast", value: ToastPayload{Text: "hi", Kind: "info"}, want: []string{"kind", "text"}},
		{
			name: "history entry",
			value: HistoryEntry{
				ID: "20261109-120000-abcd", CreatedAt: "2026-11-09T12:00:00Z",
				Width: 400, Height: 300, Size: 1234,
				URL: "shot://localhost/h/x.png", ThumbURL: "shot://localhost/h/x.thumb.jpg",
			},
			want: []string{"createdAt", "height", "id", "size", "thumbUrl", "url", "width"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := gnJSONKeys(t, tt.value); !slices.Equal(got, tt.want) {
				t.Fatalf("%s JSON keys = %v, want %v", tt.name, got, tt.want)
			}
		})
	}
}

func TestDefaultSettingsAreUsable(t *testing.T) {
	settings := DefaultSettings()
	if settings.CaptureKey == "" || settings.PinKey == "" {
		t.Fatalf("default shortcut keys are empty: %+v", settings)
	}
	if settings.AfterCapture != "inplace" || settings.Format != "PNG" {
		t.Fatalf("default afterCapture/format = %q/%q, want inplace/PNG", settings.AfterCapture, settings.Format)
	}
	// normalizeSettings must be a fixed point over the defaults.
	if normalized := normalizeSettings(settings); !reflect.DeepEqual(normalized, settings) {
		t.Fatalf("normalizeSettings(DefaultSettings()) changed defaults: %+v", normalized)
	}
}
