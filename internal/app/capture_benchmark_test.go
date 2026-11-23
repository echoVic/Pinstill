package app

import (
	"encoding/base64"
	"image"
	"testing"

	"github.com/echoVic/Pinstill/internal/imagebudget"
	"github.com/echoVic/Pinstill/internal/store"
)

func BenchmarkFinishAnnotationWithHistory(b *testing.B) {
	const width, height = 3840, 2160
	frame := image.NewNRGBA(image.Rect(0, 0, width, height))
	for row := range height {
		for column := range width {
			offset := frame.PixOffset(column, row)
			frame.Pix[offset] = uint8(column / 7)
			frame.Pix[offset+1] = uint8(row / 5)
			frame.Pix[offset+2] = uint8((column / 64) ^ (row / 64))
			frame.Pix[offset+3] = 255
		}
	}
	png, err := imagebudget.EncodePNG(frame)
	if err != nil {
		b.Fatal(err)
	}
	encoded := base64.StdEncoding.EncodeToString(png)
	history, err := store.Open(b.TempDir())
	if err != nil {
		b.Fatal(err)
	}
	application := &App{store: history, settings: DefaultSettings(), captureAction: func(string, []byte, capturePayload) (CaptureActionResult, error) {
		return CaptureActionResult{Completed: true}, nil
	}}
	b.ReportAllocs()
	for b.Loop() {
		application.capturing = true
		result, err := application.finishAnnotation("copy", encoded)
		if err != nil || !result.Completed {
			b.Fatalf("capture completion: %+v, %v", result, err)
		}
		b.StopTimer()
		if err := history.Clear(); err != nil {
			b.Fatal(err)
		}
		b.StartTimer()
	}
}
