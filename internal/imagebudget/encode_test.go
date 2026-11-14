package imagebudget

import (
	"image"
	"image/color"
	"strings"
	"testing"
)

func noisyTestImage() *image.RGBA {
	img := image.NewRGBA(image.Rect(0, 0, 128, 128))
	for y := 0; y < img.Rect.Dy(); y++ {
		for x := 0; x < img.Rect.Dx(); x++ {
			img.SetRGBA(x, y, color.RGBA{
				R: uint8(x*37 + y*17), G: uint8(x*11 + y*53), B: uint8(x*73 + y*29), A: 255,
			})
		}
	}
	return img
}

func TestEncodePNGRejectsSyntheticEncodedLimit(t *testing.T) {
	_, err := encodePNG(noisyTestImage(), 256)
	if err == nil || !strings.Contains(err.Error(), "encoded byte limit") {
		t.Fatalf("encodePNG error = %v, want encoded byte limit", err)
	}
}

func TestEncodeJPEGRejectsSyntheticEncodedLimit(t *testing.T) {
	_, err := encodeJPEG(noisyTestImage(), 90, 256)
	if err == nil || !strings.Contains(err.Error(), "encoded byte limit") {
		t.Fatalf("encodeJPEG error = %v, want encoded byte limit", err)
	}
}

func TestEncodePNGAndJPEGUseBoundedProductionAPI(t *testing.T) {
	img := noisyTestImage()
	pngData, err := EncodePNG(img)
	if err != nil || len(pngData) == 0 {
		t.Fatalf("EncodePNG() = %d bytes, %v", len(pngData), err)
	}
	jpegData, err := EncodeJPEG(img, 85)
	if err != nil || len(jpegData) == 0 {
		t.Fatalf("EncodeJPEG() = %d bytes, %v", len(jpegData), err)
	}
}
