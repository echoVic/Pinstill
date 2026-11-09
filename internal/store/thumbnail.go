package store

import (
	"bytes"
	"errors"
	"image"
	"image/jpeg"
	"math"

	"golang.org/x/image/draw"
)

const (
	// thumbnailMaxEdge is the longest thumbnail side in pixels.
	thumbnailMaxEdge = 320
	// thumbnailQuality is the JPEG quality used for thumbnails.
	thumbnailQuality = 80
)

// thumbnailDimensions returns the target thumbnail size for an image of the
// given size: the longest side is capped at thumbnailMaxEdge and smaller
// images are never upscaled.
func thumbnailDimensions(width, height int) (int, int) {
	if width <= 0 || height <= 0 {
		return 0, 0
	}
	longest := max(width, height)
	if longest <= thumbnailMaxEdge {
		return width, height
	}
	scale := float64(thumbnailMaxEdge) / float64(longest)
	scaledWidth := max(1, int(math.Round(float64(width)*scale)))
	scaledHeight := max(1, int(math.Round(float64(height)*scale)))
	return scaledWidth, scaledHeight
}

// encodeThumbnail scales src down to thumbnail size and encodes it as a JPEG.
func encodeThumbnail(src image.Image) ([]byte, error) {
	bounds := src.Bounds()
	width, height := thumbnailDimensions(bounds.Dx(), bounds.Dy())
	if width <= 0 || height <= 0 {
		return nil, errors.New("store: cannot thumbnail an empty image")
	}
	dst := image.NewRGBA(image.Rect(0, 0, width, height))
	draw.CatmullRom.Scale(dst, dst.Bounds(), src, bounds, draw.Src, nil)

	var buffer bytes.Buffer
	if err := jpeg.Encode(&buffer, dst, &jpeg.Options{Quality: thumbnailQuality}); err != nil {
		return nil, errors.New("store: encode thumbnail: " + err.Error())
	}
	return buffer.Bytes(), nil
}
