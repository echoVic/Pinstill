// Package imagebudget bounds image input before decoding or allocation.
package imagebudget

import (
	"bytes"
	"fmt"
	"image"
	_ "image/jpeg"
	"image/png"
)

const (
	// MaxEncodedBytes is the maximum accepted compressed image size.
	MaxEncodedBytes = 64 << 20
	// MaxPixels limits an RGBA canvas to 128 MiB, excluding decoder overhead.
	MaxPixels = 32 << 20
)

// CheckDimensions rejects empty images and pixel counts above MaxPixels
// without multiplying untrusted dimensions.
func CheckDimensions(width, height int) error {
	if width <= 0 || height <= 0 {
		return fmt.Errorf("imagebudget: invalid dimensions %dx%d", width, height)
	}
	if width > MaxPixels || height > MaxPixels/width {
		return fmt.Errorf("imagebudget: pixel limit exceeded by %dx%d (maximum %d)", width, height, MaxPixels)
	}
	return nil
}

// Decode checks the encoded size and header dimensions before full decoding.
// JPEG and PNG are registered here; other registered image formats also pass
// through the same checks. A successful return includes full-body validation.
func Decode(data []byte) (image.Image, string, error) {
	if err := checkBytes(data); err != nil {
		return nil, "", err
	}
	config, format, err := image.DecodeConfig(bytes.NewReader(data))
	if err != nil {
		return nil, format, err
	}
	if err := CheckDimensions(config.Width, config.Height); err != nil {
		return nil, format, err
	}
	img, actualFormat, err := image.Decode(bytes.NewReader(data))
	if err != nil {
		return nil, actualFormat, err
	}
	if actualFormat != format || img.Bounds().Dx() != config.Width || img.Bounds().Dy() != config.Height {
		return nil, actualFormat, fmt.Errorf("imagebudget: decoded image does not match its header")
	}
	return img, actualFormat, nil
}

// DecodePNG applies the same budget while accepting only a complete PNG.
func DecodePNG(data []byte) (image.Image, error) {
	if err := checkBytes(data); err != nil {
		return nil, err
	}
	config, err := png.DecodeConfig(bytes.NewReader(data))
	if err != nil {
		return nil, err
	}
	if err := CheckDimensions(config.Width, config.Height); err != nil {
		return nil, err
	}
	return png.Decode(bytes.NewReader(data))
}

func checkBytes(data []byte) error {
	if len(data) > MaxEncodedBytes {
		return fmt.Errorf("imagebudget: encoded byte limit exceeded (%d > %d)", len(data), MaxEncodedBytes)
	}
	return nil
}
