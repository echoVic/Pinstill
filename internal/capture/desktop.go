package capture

import (
	"context"
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
	return CaptureDisplaysContext(context.Background(), region, opts, displays)
}

func CaptureDisplaysContext(ctx context.Context, region Rect, opts CaptureOptions, displays []DisplaySurface) ([]byte, error) {
	return captureDisplays(ctx, region, opts, displays, CaptureRegionContext)
}

func captureDisplays(ctx context.Context, region Rect, opts CaptureOptions, displays []DisplaySurface, grab func(context.Context, Rect, CaptureOptions) ([]byte, error)) ([]byte, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	region, pieces, err := displayCapturePieces(region, displays)
	if err != nil {
		return nil, err
	}
	if len(pieces) == 1 && pieces[0].Bounds == region {
		opts.BackingScale = pieces[0].Scale
		return grab(ctx, region, opts)
	}
	frame, err := composeDisplayPieces(ctx, region, pieces, func(piece DisplaySurface) (image.Image, error) {
		opts.BackingScale = piece.Scale
		data, err := grab(ctx, piece.Bounds, opts)
		if err != nil {
			return nil, err
		}
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		opts.DelayMillis = 0
		return imagebudget.DecodePNG(data)
	})
	if err != nil {
		return nil, err
	}
	data, err := imagebudget.EncodePNG(frame)
	if err != nil {
		return nil, err
	}
	return data, ctx.Err()
}

func displayCapturePieces(region Rect, displays []DisplaySurface) (Rect, []DisplaySurface, error) {
	left, top, width, height := pointRect(region)
	if width == 0 || height == 0 {
		return Rect{}, nil, errors.New("capture: invalid selection")
	}
	region = Rect{X: float64(left), Y: float64(top), Width: float64(width), Height: float64(height)}
	var pieces []DisplaySurface
	for _, display := range displays {
		part := region.Intersect(display.Bounds)
		if part.Empty() {
			continue
		}
		if part == region {
			return region, []DisplaySurface{{Bounds: part, Scale: display.Scale}}, nil
		}
		pieces = append(pieces, DisplaySurface{Bounds: part, Scale: display.Scale})
	}
	if len(pieces) == 0 {
		return Rect{}, nil, errors.New("capture: selection does not cover a display")
	}
	return region, pieces, nil
}

func composeDisplayPieces(ctx context.Context, region Rect, pieces []DisplaySurface, read func(DisplaySurface) (image.Image, error)) (image.Image, error) {
	scale := 1.0
	for _, piece := range pieces {
		scale = max(scale, piece.Scale)
	}
	width, height := int(region.Width), int(region.Height)
	if err := checkScaledCapturePixels(width, height, scale); err != nil {
		return nil, err
	}
	canvas := image.NewNRGBA(image.Rect(0, 0, int(math.Round(float64(width)*scale)), int(math.Round(float64(height)*scale))))
	for _, piece := range pieces {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		frame, err := read(piece)
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
	return canvas, ctx.Err()
}

func checkScaledCapturePixels(width, height int, scale float64) error {
	scaledWidth, scaledHeight := math.Ceil(float64(width)*scale), math.Ceil(float64(height)*scale)
	if !finite(scaledWidth) || !finite(scaledHeight) || scaledWidth > imagebudget.MaxPixels || scaledHeight > imagebudget.MaxPixels {
		return errors.New("capture: display pixel limit exceeded")
	}
	return imagebudget.CheckDimensions(int(scaledWidth), int(scaledHeight))
}
