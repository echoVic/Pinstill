package imagebudget

import (
	"bytes"
	"fmt"
	"image"
	"image/jpeg"
	"image/png"
)

// EncodePNG encodes img while retaining at most MaxEncodedBytes in memory.
// The returned bytes are safe to pass to Decode or DecodePNG.
func EncodePNG(img image.Image) ([]byte, error) {
	return encodePNG(img, MaxEncodedBytes)
}

// EncodeJPEG encodes img at quality while retaining at most MaxEncodedBytes
// in memory. The quality range is the same as image/jpeg: 1 through 100.
func EncodeJPEG(img image.Image, quality int) ([]byte, error) {
	return encodeJPEG(img, quality, MaxEncodedBytes)
}

func encodePNG(img image.Image, limit int) ([]byte, error) {
	if err := validateImage(img); err != nil {
		return nil, err
	}
	w := newBoundedBuffer(limit)
	if err := png.Encode(w, img); err != nil {
		return nil, err
	}
	return w.Bytes(), nil
}

func encodeJPEG(img image.Image, quality, limit int) ([]byte, error) {
	if err := validateImage(img); err != nil {
		return nil, err
	}
	w := newBoundedBuffer(limit)
	if err := jpeg.Encode(w, img, &jpeg.Options{Quality: quality}); err != nil {
		return nil, err
	}
	return w.Bytes(), nil
}

func validateImage(img image.Image) error {
	if img == nil {
		return fmt.Errorf("imagebudget: nil image")
	}
	b := img.Bounds()
	return CheckDimensions(b.Dx(), b.Dy())
}

type boundedBuffer struct {
	bytes.Buffer
	limit int
}

func newBoundedBuffer(limit int) *boundedBuffer {
	return &boundedBuffer{limit: limit}
}

func (b *boundedBuffer) Write(p []byte) (int, error) {
	if len(p) > b.limit-b.Len() {
		return 0, fmt.Errorf("imagebudget: encoded byte limit exceeded")
	}
	return b.Buffer.Write(p)
}
