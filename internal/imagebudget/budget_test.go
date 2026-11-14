package imagebudget

import (
	"bytes"
	"encoding/binary"
	"hash/crc32"
	"image"
	"image/jpeg"
	"image/png"
	"io"
	"strings"
	"testing"
)

func TestCheckDimensions(t *testing.T) {
	for _, size := range [][2]int{{1, 1}, {8192, 4096}, {1, 32 << 20}} {
		if err := CheckDimensions(size[0], size[1]); err != nil {
			t.Errorf("valid dimensions %v: %v", size, err)
		}
	}
	for _, size := range [][2]int{{0, 1}, {-1, 1}, {1, 0}, {8192, 4097}, {int(^uint(0) >> 1), 2}} {
		if err := CheckDimensions(size[0], size[1]); err == nil {
			t.Errorf("accepted dimensions %v", size)
		}
	}
}

func TestDecodeChecksConfigBeforePixels(t *testing.T) {
	decoded := false
	image.RegisterFormat("budget-test", "BUDGET", func(_ io.Reader) (image.Image, error) {
		decoded = true
		return image.NewRGBA(image.Rect(0, 0, 1, 1)), nil
	}, func(_ io.Reader) (image.Config, error) {
		return image.Config{Width: 8192, Height: 4097}, nil
	})
	if _, _, err := Decode([]byte("BUDGET")); err == nil || decoded {
		t.Fatalf("oversized decoder invoked = %v, error = %v", decoded, err)
	}
}

// pngHeader describes a large image without storing or allocating its pixels.
func pngHeader(width, height uint32) []byte {
	data := make([]byte, 33)
	copy(data, "\x89PNG\r\n\x1a\n")
	binary.BigEndian.PutUint32(data[8:], 13)
	copy(data[12:], "IHDR")
	binary.BigEndian.PutUint32(data[16:], width)
	binary.BigEndian.PutUint32(data[20:], height)
	data[24], data[25] = 8, 6
	binary.BigEndian.PutUint32(data[29:], crc32.ChecksumIEEE(data[12:29]))
	return data
}

func TestDecodePNGRejectsPixelBudgetBeforeBody(t *testing.T) {
	data := pngHeader(8192, 4097)
	if _, err := DecodePNG(data); err == nil || !strings.Contains(err.Error(), "pixel") {
		t.Fatalf("oversize header: want pixel budget error before missing body, got %v", err)
	}
}

func TestDecodeFormatsAndLimits(t *testing.T) {
	img := image.NewRGBA(image.Rect(0, 0, 3, 2))
	var p, j bytes.Buffer
	if err := png.Encode(&p, img); err != nil {
		t.Fatal(err)
	}
	if err := jpeg.Encode(&j, img, nil); err != nil {
		t.Fatal(err)
	}
	for format, data := range map[string][]byte{"png": p.Bytes(), "jpeg": j.Bytes()} {
		got, name, err := Decode(data)
		if err != nil || name != format || got.Bounds() != img.Bounds() {
			t.Fatalf("Decode(%s) = %v, %q, %v", format, got, name, err)
		}
	}
	if _, err := DecodePNG(j.Bytes()); err == nil {
		t.Fatal("accepted JPEG as PNG")
	}
	if _, err := DecodePNG(p.Bytes()[:33]); err == nil {
		t.Fatal("accepted truncated PNG")
	}
	oversized := make([]byte, (64<<20)+1)
	copy(oversized, p.Bytes())
	if _, _, err := Decode(oversized); err == nil {
		t.Error("Decode accepted encoded bytes over limit")
	}
	if _, err := DecodePNG(oversized); err == nil {
		t.Error("DecodePNG accepted encoded bytes over limit")
	}
}
