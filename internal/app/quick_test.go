package app

import (
	"bytes"
	"encoding/base64"
	"errors"
	"image"
	"image/color"
	"image/png"
	"reflect"
	"strings"
	"testing"
)

func TestQuickThumbnailKeepsAspectRatioAndTransparency(t *testing.T) {
	source := image.NewNRGBA(image.Rect(0, 0, 800, 400))
	source.SetNRGBA(12, 12, color.NRGBA{R: 255, A: 255})
	var encoded bytes.Buffer
	if err := png.Encode(&encoded, source); err != nil {
		t.Fatal(err)
	}
	thumbnail, err := quickThumbnail(encoded.Bytes())
	if err != nil {
		t.Fatal(err)
	}
	data, ok := strings.CutPrefix(thumbnail, "data:image/png;base64,")
	if !ok {
		t.Fatal("thumbnail did not use an inline PNG")
	}
	decoded, err := png.Decode(base64.NewDecoder(base64.StdEncoding, strings.NewReader(data)))
	if err != nil {
		t.Fatal(err)
	}
	if decoded.Bounds().Size() != image.Pt(320, 160) {
		t.Fatalf("thumbnail dimensions = %v", decoded.Bounds())
	}
	if _, _, _, alpha := decoded.At(319, 159).RGBA(); alpha != 0 {
		t.Fatal("thumbnail filled the transparent background")
	}
}

func TestQuickCaptureRejectsStaleAndBusyActions(t *testing.T) {
	app := &App{quick: &quickSession{info: QuickCaptureInfo{ID: "latest"}, png: []byte{10, 20}}}
	var actions []string
	app.captureAction = func(action string, data []byte, payload capturePayload) (CaptureActionResult, error) {
		actions = append(actions, action)
		if !reflect.DeepEqual(data, []byte{10, 20}) {
			t.Fatalf("action used the wrong capture: %v", data)
		}
		if _, err := app.runQuickAction(nil, "latest", "save"); err == nil {
			t.Fatal("accepted a second action while the first was pending")
		}
		return CaptureActionResult{Completed: true}, nil
	}
	if _, err := app.runQuickAction(nil, "previous", "pin"); err == nil {
		t.Fatal("accepted a stale capture")
	}
	result, err := app.runQuickAction(nil, "latest", "hang")
	if err != nil || !result.Completed || !reflect.DeepEqual(actions, []string{"hang"}) {
		t.Fatalf("acting on the current capture returned %+v, %v with actions %v", result, err, actions)
	}
}

func TestQuickCaptureFailureLeavesCurrentCaptureUsable(t *testing.T) {
	app := &App{quick: &quickSession{info: QuickCaptureInfo{ID: "current"}}}
	retry := errors.New("try again")
	app.captureAction = func(string, []byte, capturePayload) (CaptureActionResult, error) {
		return CaptureActionResult{}, retry
	}
	if _, err := app.runQuickAction(nil, "current", "pin"); !errors.Is(err, retry) {
		t.Fatalf("pin failed with %v, want %v", err, retry)
	}
	app.captureAction = func(string, []byte, capturePayload) (CaptureActionResult, error) {
		return CaptureActionResult{Cancelled: true}, nil
	}
	if result, err := app.runQuickAction(nil, "current", "save"); err != nil || !result.Cancelled {
		t.Fatalf("retry returned %+v, %v", result, err)
	}
}

func TestQuickCaptureRejectsActionsAfterNewCaptureStarts(t *testing.T) {
	app := &App{capturing: true, quick: &quickSession{info: QuickCaptureInfo{ID: "previous"}}}
	app.captureAction = func(string, []byte, capturePayload) (CaptureActionResult, error) {
		t.Fatal("ran an action from a previous card during a new capture")
		return CaptureActionResult{}, nil
	}
	if _, err := app.runQuickAction(nil, "previous", "copy"); err == nil {
		t.Fatal("accepted a card after another capture started")
	}
}

func TestQuickCaptureCannotDismissItsReplacement(t *testing.T) {
	previous := &quickSession{info: QuickCaptureInfo{ID: "previous"}}
	latest := &quickSession{info: QuickCaptureInfo{ID: "latest"}}
	app := &App{quick: previous}
	app.captureAction = func(action string, _ []byte, _ capturePayload) (CaptureActionResult, error) {
		if action != "copy" {
			t.Fatal("a replacement acted while the prior action was still running")
		}
		app.mu.Lock()
		app.quick = latest
		app.mu.Unlock()
		if _, err := app.runQuickAction(nil, "latest", "pin"); err == nil {
			t.Fatal("accepted a second shortcut action with the first still active")
		}
		return CaptureActionResult{Completed: true}, nil
	}
	if _, err := app.runQuickAction(nil, "previous", "copy"); err != nil {
		t.Fatal(err)
	}
	if app.takeQuickCapture(previous, true) || app.quick != latest {
		t.Fatal("an old completion cleared the new capture")
	}
}
