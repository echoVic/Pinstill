package app

import (
	"bytes"
	"image"
	"image/color"
	"image/jpeg"
	"image/png"
	"testing"
)

// gnPNGData produces a deterministic, fully opaque PNG of the requested size.
// It is used by the app package tests as a stand-in for a captured screenshot.
func gnPNGData(t *testing.T, width, height int) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, width, height))
	for y := 0; y < height; y++ {
		for x := 0; x < width; x++ {
			img.SetRGBA(x, y, color.RGBA{
				R: uint8(x % 256),
				G: uint8(y % 256),
				B: uint8((x + y) % 256),
				A: 255,
			})
		}
	}
	return gnEncodePNG(t, img)
}

// gnTransparentPNG produces a fully transparent PNG, the shape of the blank
// image macOS returns without screen recording permission.
func gnTransparentPNG(t *testing.T, width, height int) []byte {
	t.Helper()
	return gnEncodePNG(t, image.NewNRGBA(image.Rect(0, 0, width, height)))
}

// gnTransparentPNGWithPixel produces a transparent PNG with one opaque pixel.
func gnTransparentPNGWithPixel(t *testing.T, width, height, px, py int) []byte {
	t.Helper()
	img := image.NewNRGBA(image.Rect(0, 0, width, height))
	img.SetNRGBA(px, py, color.NRGBA{R: 255, G: 255, B: 255, A: 255})
	return gnEncodePNG(t, img)
}

func gnEncodePNG(t *testing.T, img image.Image) []byte {
	t.Helper()
	var buffer bytes.Buffer
	if err := png.Encode(&buffer, img); err != nil {
		t.Fatalf("png.Encode() error = %v", err)
	}
	return buffer.Bytes()
}

// gnJPEGData produces a deterministic opaque JPEG of the requested size.
func gnJPEGData(t *testing.T, width, height, quality int) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, width, height))
	for y := 0; y < height; y++ {
		for x := 0; x < width; x++ {
			img.SetRGBA(x, y, color.RGBA{R: uint8(x * 7), G: uint8(y * 5), B: 128, A: 255})
		}
	}
	var buffer bytes.Buffer
	if err := jpeg.Encode(&buffer, img, &jpeg.Options{Quality: quality}); err != nil {
		t.Fatalf("jpeg.Encode() error = %v", err)
	}
	return buffer.Bytes()
}

// gnImageConfig decodes an image header and returns its pixel dimensions.
func gnImageConfig(t *testing.T, data []byte) (int, int) {
	t.Helper()
	config, _, err := image.DecodeConfig(bytes.NewReader(data))
	if err != nil {
		t.Fatalf("image.DecodeConfig() error = %v", err)
	}
	return config.Width, config.Height
}

// gnPNGMagic is the 8 byte PNG file signature.
func gnPNGMagic() []byte {
	return []byte("\x89PNG\r\n\x1a\n")
}

// gnHasJPEGMagic reports whether data starts with the JPEG SOI marker.
func gnHasJPEGMagic(data []byte) bool {
	return len(data) >= 3 && data[0] == 0xFF && data[1] == 0xD8 && data[2] == 0xFF
}
