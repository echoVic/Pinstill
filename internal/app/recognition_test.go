package app

import (
	"context"
	"strings"
	"testing"
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
