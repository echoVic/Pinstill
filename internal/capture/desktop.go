package capture

import (
	"errors"
	"image"
	"image/draw"
	"math"

	"github.com/echoVic/Pinstill/internal/imagebudget"
	xdraw "golang.org/x/image/draw"
)

type DisplaySurface struct {
	Bounds Rect
	Scale  float64
}

func CaptureDisplays(region Rect, opts CaptureOptions, displays []DisplaySurface) ([]byte, error) {
	return captureDisplays(region, opts, displays, CaptureRegion)
}

func captureDisplays(region Rect, opts CaptureOptions, displays []DisplaySurface, grab func(Rect, CaptureOptions) ([]byte, error)) ([]byte, error) {
	left, top, width, height := pointRect(region)
	if width == 0 || height == 0 {
		return nil, errors.New("capture: invalid selection")
	}
	region = Rect{X: float64(left), Y: float64(top), Width: float64(width), Height: float64(height)}
	var pieces []DisplaySurface
	scale := 1.0
	for _, display := range displays {
		part := region.Intersect(display.Bounds)
		if part.Empty() {
			continue
		}
		if part == region {
			opts.BackingScale = display.Scale
			return grab(region, opts)
		}
		pieces = append(pieces, DisplaySurface{Bounds: part, Scale: display.Scale})
		scale = max(scale, display.Scale)
	}
	if len(pieces) == 0 {
		return nil, errors.New("capture: selection does not cover a display")
	}
	if err := checkScaledCapturePixels(width, height, scale); err != nil {
		return nil, err
	}
	canvas := image.NewNRGBA(image.Rect(0, 0, int(math.Round(float64(width)*scale)), int(math.Round(float64(height)*scale))))
	for _, piece := range pieces {
		opts.BackingScale = piece.Scale
		data, err := grab(piece.Bounds, opts)
		if err != nil {
			return nil, err
		}
		opts.DelayMillis = 0
		frame, err := imagebudget.DecodePNG(data)
		if err != nil {
			return nil, err
		}
		part := piece.Bounds
		x, y := (part.X-region.X)*scale, (part.Y-region.Y)*scale
		bounds := image.Rect(int(math.Round(x)), int(math.Round(y)), int(math.Round(x+part.Width*scale)), int(math.Round(y+part.Height*scale)))
		if frame.Bounds().Size() == bounds.Size() {
			draw.Draw(canvas, bounds, frame, frame.Bounds().Min, draw.Src)
		} else {
			xdraw.NearestNeighbor.Scale(canvas, bounds, frame, frame.Bounds(), draw.Src, nil)
		}
	}
	return imagebudget.EncodePNG(canvas)
}

func checkScaledCapturePixels(width, height int, scale float64) error {
	scaledWidth, scaledHeight := math.Ceil(float64(width)*scale), math.Ceil(float64(height)*scale)
	if !finite(scaledWidth) || !finite(scaledHeight) || scaledWidth > imagebudget.MaxPixels || scaledHeight > imagebudget.MaxPixels {
		return errors.New("capture: display pixel limit exceeded")
	}
	return imagebudget.CheckDimensions(int(scaledWidth), int(scaledHeight))
}
