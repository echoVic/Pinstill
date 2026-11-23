package app

import (
	"context"
	"image"
	"image/color"
	"image/draw"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/echoVic/Pinstill/internal/capture"
	"github.com/echoVic/Pinstill/internal/imagebudget"
	"github.com/egoist/mygo"
)

func TestSelectionUsesDesktopFrameTakenBeforePopupCloses(t *testing.T) {
	pink := color.NRGBA{R: 239, G: 24, B: 133, A: 255}
	display := mygo.Display{Bounds: mygo.Rectangle{X: -80, Y: -20, Width: 80, Height: 50}, ScaleFactor: 2}
	frame := image.NewNRGBA(image.Rect(0, 0, 160, 100))
	draw.Draw(frame, frame.Bounds(), image.NewUniform(color.Black), image.Point{}, draw.Src)
	draw.Draw(frame, image.Rect(50, 20, 100, 60), image.NewUniform(pink), image.Point{}, draw.Src)
	pngBytes, err := imagebudget.EncodePNG(frame)
	if err != nil {
		t.Fatal(err)
	}
	frozen, err := capture.FreezeDesktop(context.Background(), []capture.DisplaySurface{{Bounds: asCaptureRect(display.Bounds), Scale: 2}}, capture.CaptureOptions{}, func(context.Context, capture.Rect, capture.CaptureOptions) ([]byte, error) {
		return pngBytes, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	a := &App{
		capturing: true, overlayInteraction: 1, overlayDisplay: display, overlayRect: display.Bounds,
		overlayDesktop: frozen, sessionDir: t.TempDir(),
	}
	for _, selection := range []Rect{
		{X: 25, Y: 10, Width: 25, Height: 20},
		{X: 22, Y: 8, Width: 30, Height: 25},
	} {
		payload, err := a.prepareSelectionWithDelay(selection, 0)
		if err != nil {
			t.Fatal(err)
		}
		data, err := os.ReadFile(filepath.Join(a.sessionDir, payload.ID+".png"))
		if err != nil {
			t.Fatal(err)
		}
		shot, err := imagebudget.DecodePNG(data)
		if err != nil {
			t.Fatal(err)
		}
		if got := color.NRGBAModel.Convert(shot.At(payload.Width/2, payload.Height/2)); got != pink {
			t.Fatalf("crop of %+v lost the popup: got pixel %v", selection, got)
		}
		if payload.Width != int(selection.Width)*2 || payload.Height != int(selection.Height)*2 {
			t.Fatalf("crop of %+v lost Retina dimensions: %+v", selection, payload)
		}
	}

	request := httptest.NewRequest(http.MethodGet, "/d/1/0.png", nil)
	active := httptest.NewRecorder()
	a.serveOverlayBackdrop(active, request)
	if active.Code != http.StatusOK || active.Header().Get("Cache-Control") != "no-store" {
		t.Fatalf("active backdrop response = %d, %v", active.Code, active.Header())
	}
	a.cancelRegion()
	closed := httptest.NewRecorder()
	a.serveOverlayBackdrop(closed, request)
	if closed.Code != http.StatusNotFound {
		t.Fatalf("backdrop survived cancellation: %d", closed.Code)
	}
}
