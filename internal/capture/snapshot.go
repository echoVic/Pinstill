package capture

import (
	"context"
	"errors"
	"image"
	"image/draw"
	"math"
	"sync"

	"github.com/echoVic/Pinstill/internal/imagebudget"
)

type DesktopSnapshot struct {
	displays []DisplaySurface
	frames   []desktopFrame
}

type desktopFrame struct {
	png   []byte
	image func() (image.Image, error)
}

func FreezeDesktop(ctx context.Context, displays []DisplaySurface, opts CaptureOptions, grab func(context.Context, Rect, CaptureOptions) ([]byte, error)) (*DesktopSnapshot, error) {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	snapshot := &DesktopSnapshot{displays: displays, frames: make([]desktopFrame, len(displays))}
	failures := make(chan error, len(displays))
	var captures sync.WaitGroup
	for index, display := range displays {
		captures.Go(func() {
			options := opts
			options.BackingScale = display.Scale
			data, err := grab(ctx, display.Bounds, options)
			if err != nil {
				failures <- err
				cancel()
				return
			}
			snapshot.frames[index] = desktopFrame{png: data, image: sync.OnceValues(func() (image.Image, error) {
				return imagebudget.DecodePNG(data)
			})}
		})
	}
	captures.Wait()
	select {
	case err := <-failures:
		return nil, err
	default:
		return snapshot, ctx.Err()
	}
}

func (snapshot *DesktopSnapshot) PNG(index int) []byte {
	if snapshot == nil || index < 0 || index >= len(snapshot.frames) {
		return nil
	}
	return snapshot.frames[index].png
}

func (snapshot *DesktopSnapshot) Crop(ctx context.Context, region Rect) (image.Image, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	region, pieces, err := displayCapturePieces(region, snapshot.displays)
	if err != nil {
		return nil, err
	}
	if len(pieces) == 1 && pieces[0].Bounds == region {
		return snapshot.cropDisplayPiece(region)
	}
	return composeDisplayPieces(ctx, region, pieces, func(piece DisplaySurface) (image.Image, error) {
		return snapshot.cropDisplayPiece(piece.Bounds)
	})
}

func (snapshot *DesktopSnapshot) cropDisplayPiece(part Rect) (image.Image, error) {
	for index, display := range snapshot.displays {
		if part.Intersect(display.Bounds) != part {
			continue
		}
		frame, err := snapshot.frames[index].image()
		if err != nil {
			return nil, err
		}
		bounds := frame.Bounds()
		scaleX := float64(bounds.Dx()) / display.Bounds.Width
		scaleY := float64(bounds.Dy()) / display.Bounds.Height
		x, y := part.X-display.Bounds.X, part.Y-display.Bounds.Y
		crop := image.Rect(
			int(math.Round(x*scaleX)), int(math.Round(y*scaleY)),
			int(math.Round((x+part.Width)*scaleX)), int(math.Round((y+part.Height)*scaleY)),
		).Add(bounds.Min).Intersect(bounds)
		if sub, ok := frame.(interface {
			SubImage(image.Rectangle) image.Image
		}); ok {
			return sub.SubImage(crop), nil
		}
		result := image.NewNRGBA(image.Rectangle{Max: crop.Size()})
		draw.Draw(result, result.Bounds(), frame, crop.Min, draw.Src)
		return result, nil
	}
	return nil, errors.New("capture: selection does not cover a display")
}
