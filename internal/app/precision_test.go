package app

import (
	"encoding/base64"
	"image"
	"image/color"
	"image/png"
	"strings"
	"testing"

	"github.com/echoVic/Pinstill/internal/capture"
	"github.com/egoist/mygo"
)

func TestPixelSampleClipsAtDisplaySeam(t *testing.T) {
	displays := []mygo.Display{
		{Bounds: mygo.Rectangle{X: -1440, Y: -200, Width: 1440, Height: 900}, ScaleFactor: 2},
		{Bounds: mygo.Rectangle{Width: 1920, Height: 1080}, ScaleFactor: 1},
	}
	region, scale := pixelSampleRegion(capture.Point{X: -1, Y: 0}, displays)
	if want := (capture.Rect{X: -8, Y: -7, Width: 8, Height: 15}); region != want {
		t.Fatalf("sample rect = %+v, want %+v", region, want)
	}
	if scale != 2 {
		t.Fatalf("sampled at scale %v", scale)
	}
	if region, _ := pixelSampleRegion(capture.Point{X: -20, Y: 920}, displays); !region.Empty() {
		t.Fatalf("sampled the empty space outside the displays: %+v", region)
	}
}

func TestPixelSampleUsesNativePixelsAndPadsTheGrid(t *testing.T) {
	frame := image.NewNRGBA(image.Rect(0, 0, 16, 30))
	frame.SetNRGBA(14, 14, color.NRGBA{R: 160, G: 80, B: 255, A: 255})
	frame.SetNRGBA(15, 14, color.NRGBA{R: 20, G: 30, B: 40, A: 255})
	result, err := makePixelSample(frame, capture.Rect{X: -8, Y: -7, Width: 8, Height: 15}, capture.Point{X: -1, Y: 0})
	if err != nil {
		t.Fatal(err)
	}
	if result.Hex != "#a050ff" {
		t.Fatalf("sampled %s", result.Hex)
	}
	grid, err := png.Decode(base64.NewDecoder(base64.StdEncoding, strings.NewReader(result.PNGBase64)))
	if err != nil {
		t.Fatal(err)
	}
	if got := grid.Bounds().Size(); got != image.Pt(15, 15) {
		t.Fatalf("grid size = %v", got)
	}
	if got := color.NRGBAModel.Convert(grid.At(8, 7)); got != (color.NRGBA{R: 20, G: 30, B: 40, A: 255}) {
		t.Fatalf("adjacent pixel = %v", got)
	}
	if _, _, _, alpha := grid.At(9, 7).RGBA(); alpha != 0 {
		t.Fatal("pixels outside the display must stay transparent")
	}
}
