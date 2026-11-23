package app

import (
	"context"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"image"
	"image/color"
	"math"
	"strings"
	"sync"

	"github.com/echoVic/Pinstill/internal/capture"
	"github.com/echoVic/Pinstill/internal/imagebudget"
	"github.com/egoist/mygo"
	"github.com/egoist/mygo/transfer"
)

const pixelSampleRadius = 7

var pixelSampleMu sync.Mutex

type PixelSample struct {
	PNGBase64 string `json:"pngBase64"`
	Hex       string `json:"hex"`
}

func (s *CaptureService) SampleOverlayPixels(ctx context.Context, localX, localY float64) (PixelSample, error) {
	app := s.app
	app.mu.Lock()
	generation := app.captureGeneration
	desktop := app.overlayDesktop
	active := app.capturing && !app.quitting && app.scroll == nil
	app.mu.Unlock()
	surface, ok := app.overlaySurfaceFor(mygo.CallerWindow(ctx))
	if !active || !ok {
		return PixelSample{}, errors.New("取色点不在当前选区窗口内")
	}
	if !pixelSampleMu.TryLock() {
		return PixelSample{}, errors.New("正在读取屏幕像素")
	}
	defer pixelSampleMu.Unlock()
	point := capture.Point{X: float64(surface.bounds.X) + localX, Y: float64(surface.bounds.Y) + localY}
	region, scale := pixelSampleRegion(point, mygo.Screen.Displays())
	if region.Empty() || !app.selectionIsCurrent(generation) {
		return PixelSample{}, errors.New("取色点不在可截取的显示器上")
	}
	var frame image.Image
	var err error
	if desktop != nil {
		frame, err = desktop.Crop(ctx, region)
	} else {
		var data []byte
		data, err = app.captureRegion(region, capture.CaptureOptions{BackingScale: scale})
		if err == nil {
			frame, err = imagebudget.DecodePNG(data)
		}
	}
	if err != nil {
		return PixelSample{}, err
	}
	if !app.selectionIsCurrent(generation) {
		return PixelSample{}, errors.New("截图请求已结束")
	}
	return makePixelSample(frame, region, point)
}

func (*CaptureService) CopyPixelColor(value string) error {
	if len(value) != 7 || value[0] != '#' {
		return errors.New("颜色格式无效")
	}
	if _, err := hex.DecodeString(value[1:]); err != nil {
		return errors.New("颜色格式无效")
	}
	return mygo.Clipboard.Write(transfer.TextData(strings.ToLower(value)))
}

func pixelSampleRegion(point capture.Point, displays []mygo.Display) (capture.Rect, float64) {
	for _, display := range displays {
		bounds := display.Bounds
		if point.X >= float64(bounds.X) && point.X < float64(bounds.X+bounds.Width) &&
			point.Y >= float64(bounds.Y) && point.Y < float64(bounds.Y+bounds.Height) {
			return clampToDisplay(capture.Rect{
				X: math.Floor(point.X) - pixelSampleRadius, Y: math.Floor(point.Y) - pixelSampleRadius,
				Width: 2*pixelSampleRadius + 1, Height: 2*pixelSampleRadius + 1,
			}, bounds), display.ScaleFactor
		}
	}
	return capture.Rect{}, 0
}

func makePixelSample(frame image.Image, region capture.Rect, point capture.Point) (PixelSample, error) {
	bounds := frame.Bounds()
	if bounds.Empty() || region.Empty() || point.X < region.X || point.X >= region.X+region.Width ||
		point.Y < region.Y || point.Y >= region.Y+region.Height {
		return PixelSample{}, errors.New("取色像素不可用")
	}
	center := image.Pt(
		bounds.Min.X+int((point.X-region.X)*float64(bounds.Dx())/region.Width),
		bounds.Min.Y+int((point.Y-region.Y)*float64(bounds.Dy())/region.Height),
	)
	center.X = min(center.X, bounds.Max.X-1)
	center.Y = min(center.Y, bounds.Max.Y-1)
	size := 2*pixelSampleRadius + 1
	grid := image.NewNRGBA(image.Rect(0, 0, size, size))
	for gridY := range size {
		for gridX := range size {
			pixel := center.Add(image.Pt(gridX-pixelSampleRadius, gridY-pixelSampleRadius))
			if pixel.In(bounds) {
				grid.Set(gridX, gridY, frame.At(pixel.X, pixel.Y))
			}
		}
	}
	centerColor := color.NRGBAModel.Convert(frame.At(center.X, center.Y)).(color.NRGBA)
	data, err := imagebudget.EncodePNG(grid)
	if err != nil {
		return PixelSample{}, err
	}
	return PixelSample{
		PNGBase64: base64.StdEncoding.EncodeToString(data),
		Hex:       fmt.Sprintf("#%02x%02x%02x", centerColor.R, centerColor.G, centerColor.B),
	}, nil
}
