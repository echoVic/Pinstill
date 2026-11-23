package capture

import (
	"errors"
	"image"
	"image/color"
	"image/draw"
	"reflect"
	"testing"

	"github.com/echoVic/Pinstill/internal/imagebudget"
)

func TestDisplayCaptureKeepsSingleDisplayPixels(t *testing.T) {
	selection := Rect{X: -94, Y: -19, Width: 5, Height: 7}
	displays := []DisplaySurface{{Bounds: Rect{X: -100, Y: -20, Width: 100, Height: 100}, Scale: 1}, {Bounds: Rect{Width: 100, Height: 100}, Scale: 2}}
	data, err := captureDisplays(selection, CaptureOptions{}, displays, func(region Rect, opts CaptureOptions) ([]byte, error) {
		if region != selection || opts.BackingScale != 1 {
			t.Fatalf("capture got region %+v, scale %v", region, opts.BackingScale)
		}
		return []byte("native PNG data"), nil
	})
	if err != nil || string(data) != "native PNG data" {
		t.Fatalf("single display result = %q, %v", data, err)
	}
}

func TestDisplayCaptureComposesMixedScalesAndDesktopGap(t *testing.T) {
	red := color.NRGBA{R: 255, A: 255}
	blue := color.NRGBA{B: 255, A: 255}
	displays := []DisplaySurface{{Bounds: Rect{X: -100, Y: -20, Width: 100, Height: 100}, Scale: 1}, {Bounds: Rect{Width: 100, Height: 100}, Scale: 2}}
	var regions []Rect
	data, err := captureDisplays(Rect{X: -2, Y: -2, Width: 4, Height: 4}, CaptureOptions{}, displays, func(region Rect, opts CaptureOptions) ([]byte, error) {
		regions = append(regions, region)
		width, height := int(region.Width*opts.BackingScale), int(region.Height*opts.BackingScale)
		piece := image.NewNRGBA(image.Rect(0, 0, width, height))
		fill := blue
		if region.X < 0 {
			fill = red
		}
		draw.Draw(piece, piece.Bounds(), image.NewUniform(fill), image.Point{}, draw.Src)
		return imagebudget.EncodePNG(piece)
	})
	if err != nil {
		t.Fatal(err)
	}
	if want := []Rect{{X: -2, Y: -2, Width: 2, Height: 4}, {Width: 2, Height: 2}}; !reflect.DeepEqual(regions, want) {
		t.Fatalf("captured pieces = %+v, want %+v", regions, want)
	}
	result, err := imagebudget.DecodePNG(data)
	if err != nil {
		t.Fatal(err)
	}
	if result.Bounds() != image.Rect(0, 0, 8, 8) {
		t.Fatalf("composite bounds = %v", result.Bounds())
	}
	for _, sample := range []struct {
		point image.Point
		want  color.NRGBA
	}{{image.Pt(3, 3), red}, {image.Pt(4, 3), color.NRGBA{}}, {image.Pt(4, 4), blue}, {image.Pt(7, 7), blue}} {
		if got := color.NRGBAModel.Convert(result.At(sample.point.X, sample.point.Y)); got != sample.want {
			t.Errorf("pixel %v = %v, want %v", sample.point, got, sample.want)
		}
	}
}

func TestDisplayCaptureChecksBudgetBeforeGrabbing(t *testing.T) {
	displays := []DisplaySurface{{Bounds: Rect{X: -10_000, Width: 10_000, Height: 10_000}, Scale: 2}, {Bounds: Rect{Width: 10_000, Height: 10_000}, Scale: 1}}
	called := false
	_, err := captureDisplays(Rect{X: -5_000, Width: 10_000, Height: 10_000}, CaptureOptions{}, displays, func(Rect, CaptureOptions) ([]byte, error) {
		called = true
		return nil, errors.New("grab should not start")
	})
	if err == nil || called {
		t.Fatalf("capture = %v, grabbed = %v", err, called)
	}
}
