//go:build darwin

package app

import (
	"bytes"
	"context"
	"encoding/base64"
	"image"
	"image/color"
	"image/draw"
	"image/png"
	"testing"

	"golang.org/x/image/font"
	"golang.org/x/image/font/gofont/goregular"
	"golang.org/x/image/font/opentype"
	"golang.org/x/image/math/fixed"
)

func TestVisionExtractsSeparateTextLines(t *testing.T) {
	faceFont, err := opentype.Parse(goregular.TTF)
	if err != nil {
		t.Fatal(err)
	}
	face, err := opentype.NewFace(faceFont, &opentype.FaceOptions{Size: 42, DPI: 72})
	if err != nil {
		t.Fatal(err)
	}
	defer face.Close()
	imageData := image.NewRGBA(image.Rect(0, 0, 640, 200))
	draw.Draw(imageData, imageData.Bounds(), image.White, image.Point{}, draw.Src)
	drawer := font.Drawer{Dst: imageData, Src: image.NewUniform(color.Black), Face: face}
	drawer.Dot = fixed.P(24, 70)
	drawer.DrawString("Pinstill OCR test")
	drawer.Dot = fixed.P(24, 144)
	drawer.DrawString("Copy these words")
	var encoded bytes.Buffer
	if err := png.Encode(&encoded, imageData); err != nil {
		t.Fatal(err)
	}
	result, err := recognizePNG(context.Background(), "ocr", "中英文混合", encoded.Bytes())
	if err != nil {
		t.Fatal(err)
	}
	if result.Text != "Pinstill OCR test\nCopy these words" {
		t.Fatalf("OCR = %q", result.Text)
	}
}

func TestVisionDecodesMultipleQRs(t *testing.T) {
	const fixture = "iVBORw0KGgoAAAANSUhEUgAAAooAAAETAQAAAACZjho3AAADLklEQVR42u2cXeqjShDFz4mB9k130NlJZ2Pjx39lZiftDvStBfXMgwnM3Gty4TLM0EMVIhLo30MV9dVVhMIvlscFv1wMaUhDGtKQhjSkIQ1pSEMa0pCGNOSfRI4kWY4sR/KmEiJJdqbLvxfZSju85qAEJsBJiqbLvxgpssPYluPFxa+FHUaWp2dmkrzOxEzWIEneTZe5Iv0Ffl/YVp+8O0hCNbAOK1BImkyXOSPHDmjgP2Vw8o75LvXFBMy8mi6zRFLqMHZAX8UvjB280oeTla5zu9Wmy3yRPXkBGhcboAGwkLx9OPpYAcx302VeSOhfEikpodU7mYKkYsKRxIvXL08ZzDy59ONtCfHWl8DCzkUsN5XjaTZ/kASCmwLm+0ayNl1miPTFzEKPbgm7i2JgCc638wwuaa36terXZ5AYTJcZIr+V+MYbFfsSwNi5B897tPl+BVhjq7H5Yatxna0fzzCPR7jI5JX8DjQutgl8l8dRDcfHCqzVUFgez9LirSKAVhEutsnv8FI8q98mACgmoHoGc6vc8szjZFDyLAH49qjZlvPuLKzVsAIa20KDNGze8niO3Vkv7S62CW3yfToc/NTHNWgKAKShkFBprWQ+nmU/HpkAgJKS1wcBsAJHiS7rx3NFqvRbpf0VzMXbccd+MjtDNVwRWIe1Gn4enJku84rqSr6DV/J9QgO/493N2xRWhB/Cu0X1LLsz+A5onDr4Dn4H+LZWr1RMOGI7cFjfLJ5fHk/+eHYXGxffmPsHNy+moCloglVuefo4FeHUK8LpmJPyvH579ePFhGIKx9ssnuvsLFLq4PvjQsadR/UgqdCwIgBBk+VxZLzLigZo01G6Pz9wvgMDOPXAgzU223NDvrusHcaLi81yYwKW0H04+1pyC9fXdavpMr9dVuiYjwM9b1/n/fhrgrbVYZ2Jf27CmC4zQy5hB76clKB3G63SgGpw02OrA6re5uMZI8cLxssrtrfnGxHHDsx833gvpsfVfDzfaal2p17an8Wb7/U/xGr1jO7cABfhtDvtLlL/eQljFs8QSft/IkMa0pCGNKQhDWlIQxrSkIY0pCEN+buQ3wEQdt5X3L9PFAAAAABJRU5ErkJggg=="
	encoded, err := base64.StdEncoding.DecodeString(fixture)
	if err != nil {
		t.Fatal(err)
	}
	result, err := recognizePNG(context.Background(), "qr", "", encoded)
	if err != nil {
		t.Fatal(err)
	}
	const web = "https://example.test/pinstill?a=1"
	if len(result.Codes) != 2 {
		t.Fatalf("QR codes = %+v", result.Codes)
	}
	got := make(map[string]string)
	for _, code := range result.Codes {
		got[code.Text] = code.URL
	}
	if got[web] != web {
		t.Fatalf("web QR missing: %+v", got)
	}
	if target, ok := got["Pinstill QR text"]; !ok || target != "" {
		t.Fatalf("plain text QR missing or incorrectly linkable: %+v", got)
	}
}
