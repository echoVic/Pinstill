package app

import (
	"context"
	"strings"
	"testing"

	"github.com/egoist/mygo"
)

func TestRecognizedWebURL(t *testing.T) {
	for _, payload := range []string{"https://example.test/pinstill?a=1", "http://example.test:8080/文档"} {
		if got := recognizedWebURL(payload); got != payload {
			t.Errorf("web URL %q = %q", payload, got)
		}
	}
	for _, payload := range []string{
		"Pinstill QR text", "file:///example", "mailto:somebody@example.test", "javascript:void(0)",
		"https:example.test", "https://", "https://example.test@other.test/", "https://example.test\\@other.test/",
		"https://example.test/\nhttps://other.test", "https://example.test/\u202e",
	} {
		if got := recognizedWebURL(payload); got != "" {
			t.Errorf("non-web payload %q was made openable: %q", payload, got)
		}
	}
}

func TestRecognizePNGRejectsInvalidImageBeforeVision(t *testing.T) {
	_, err := recognizePNG(context.Background(), "ocr", "English", []byte("invalid png"))
	if err == nil || !strings.Contains(err.Error(), "截图 PNG") {
		t.Fatalf("invalid screenshot error = %v", err)
	}
}

func TestRecognitionRejectsMissingOverlayCaller(t *testing.T) {
	service := &CaptureService{app: &App{capturing: true, overlayCommitted: true, overlayPreviewID: "ready"}}
	if _, err := service.RecognizeSelection(context.Background(), "ocr", "English", ""); err == nil || err.Error() != "没有可识别的截图选区" {
		t.Fatalf("non-overlay call error = %v", err)
	}
}

func TestAuthorizeRecognizedCopyAllowsTranslateWindowOnly(t *testing.T) {
	translateWin := new(mygo.Window)
	otherWin := new(mygo.Window)
	app := &App{translate: translateWin}

	ctx := context.Background()
	if err := app.authorizeRecognizedCopy(ctx, translateWin); err != nil {
		t.Fatalf("authorizeRecognizedCopy(translate) = %v", err)
	}
	if err := app.authorizeRecognizedCopy(ctx, nil); err != errRecognitionSelection {
		t.Fatalf("authorizeRecognizedCopy(nil) = %v, want %v", err, errRecognitionSelection)
	}
	if err := app.authorizeRecognizedCopy(ctx, otherWin); err != errRecognitionSelection {
		t.Fatalf("authorizeRecognizedCopy(other) = %v, want %v", err, errRecognitionSelection)
	}

	app.mu.Lock()
	app.quitting = true
	app.mu.Unlock()
	if err := app.authorizeRecognizedCopy(ctx, translateWin); err != errRecognitionSelection {
		t.Fatalf("authorizeRecognizedCopy(translate while quitting) = %v, want %v", err, errRecognitionSelection)
	}
}

func TestRecognizeTranslateImageAuthorization(t *testing.T) {
	translateWin := new(mygo.Window)
	otherWin := new(mygo.Window)
	service := &CaptureService{app: &App{translate: translateWin}}
	ctx := context.Background()

	if _, err := service.recognizeTranslateImageFor(ctx, nil, "English", ""); err != errRecognitionSelection {
		t.Fatalf("recognizeTranslateImageFor(nil) = %v, want %v", err, errRecognitionSelection)
	}
	if _, err := service.recognizeTranslateImageFor(ctx, otherWin, "English", ""); err != errRecognitionSelection {
		t.Fatalf("recognizeTranslateImageFor(other) = %v, want %v", err, errRecognitionSelection)
	}
	if _, err := service.recognizeTranslateImageFor(ctx, translateWin, "English", "aW52YWxpZA=="); err == nil || !strings.Contains(err.Error(), "截图 PNG") {
		t.Fatalf("recognizeTranslateImageFor(invalid png) = %v", err)
	}
}

func TestTranslateAlwaysOnTopAuthorization(t *testing.T) {
	translateWin := new(mygo.Window)
	otherWin := new(mygo.Window)
	var applied []bool
	app := &App{
		translate:               translateWin,
		setTranslateAlwaysOnTop: func(v bool) { applied = append(applied, v) },
	}

	if _, err := app.setTranslateAlwaysOnTopFor(nil, true); err != errRecognitionSelection {
		t.Fatalf("setTranslateAlwaysOnTopFor(nil) = %v, want %v", err, errRecognitionSelection)
	}
	if _, err := app.setTranslateAlwaysOnTopFor(otherWin, true); err != errRecognitionSelection {
		t.Fatalf("setTranslateAlwaysOnTopFor(other) = %v, want %v", err, errRecognitionSelection)
	}
	got, err := app.setTranslateAlwaysOnTopFor(translateWin, true)
	if err != nil || !got {
		t.Fatalf("setTranslateAlwaysOnTopFor(translate, true) = %v, %v", got, err)
	}
	state, err := app.translateAlwaysOnTopFor(translateWin)
	if err != nil || !state {
		t.Fatalf("translateAlwaysOnTopFor(translate) = %v, %v", state, err)
	}
	if len(applied) != 1 || !applied[0] {
		t.Fatalf("hook applied = %v, want [true]", applied)
	}
}
