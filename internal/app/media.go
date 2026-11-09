package app

import (
	"bytes"
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
)

// decodeImageBase64 accepts raw base64 or a data URL and returns the bytes.
func decodeImageBase64(data string) ([]byte, error) {
	data = strings.TrimSpace(data)
	if i := strings.Index(data, ","); strings.HasPrefix(data, "data:") && i >= 0 {
		data = data[i+1:]
	}
	raw, err := base64.StdEncoding.DecodeString(data)
	if err != nil {
		return nil, fmt.Errorf("图片数据无法解码: %w", err)
	}
	if len(raw) == 0 {
		return nil, errors.New("图片数据为空")
	}
	return raw, nil
}

// blankPNG reports whether the PNG has no visible pixels at all, which is
// how macOS delivers a capture taken without the screen recording
// permission.
func blankPNG(pngBytes []byte) bool {
	img, err := png.Decode(bytes.NewReader(pngBytes))
	if err != nil {
		return false
	}
	b := img.Bounds()
	if b.Empty() {
		return true
	}
	stepX := max(1, b.Dx()/64)
	stepY := max(1, b.Dy()/64)
	for y := b.Min.Y; y < b.Max.Y; y += stepY {
		for x := b.Min.X; x < b.Max.X; x += stepX {
			_, _, _, a := img.At(x, y).RGBA()
			if a != 0 {
				return false
			}
		}
	}
	return true
}

// playCaptureSound plays the system screenshot sound (macOS only).
func playCaptureSound() {
	if runtime.GOOS != "darwin" {
		return
	}
	cmd := exec.Command("/usr/bin/afplay", "/System/Library/Sounds/Glass.aiff")
	_ = cmd.Start()
}

// randomSuffix returns four random lowercase hex characters.
func randomSuffix() string {
	var b [2]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "0000"
	}
	return hex.EncodeToString(b[:])
}

// expandHome resolves a leading ~ to the user's home directory.
func expandHome(path string) (string, error) {
	path = strings.TrimSpace(path)
	if path == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", err
		}
		return filepath.Join(home, "Pictures", "Tupen"), nil
	}
	if path == "~" || strings.HasPrefix(path, "~/") {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", err
		}
		return filepath.Join(home, strings.TrimPrefix(strings.TrimPrefix(path, "~"), "/")), nil
	}
	return path, nil
}

// uniquePath makes path unused by appending a counter before the extension.
func uniquePath(path string) string {
	ext := filepath.Ext(path)
	base := strings.TrimSuffix(path, ext)
	candidate := path
	for i := 2; ; i++ {
		if _, err := os.Stat(candidate); os.IsNotExist(err) {
			return candidate
		}
		candidate = fmt.Sprintf("%s-%d%s", base, i, ext)
	}
}

// trayIconPNG draws the menu bar icon: a black template "T". macOS tints
// template icons with the menu bar's appearance; Windows shows them white
// on its dark taskbar.
func trayIconPNG() []byte {
	const size = 32
	img := image.NewNRGBA(image.Rect(0, 0, size, size))
	black := color.NRGBA{R: 0, G: 0, B: 0, A: 255}
	fill := func(x0, y0, x1, y1 int) {
		for y := y0; y < y1; y++ {
			for x := x0; x < x1; x++ {
				if image.Pt(x, y).In(img.Bounds()) {
					img.SetNRGBA(x, y, black)
				}
			}
		}
	}
	// Bold T: a crop frame at the top, a stem, and rounded feet.
	fill(6, 6, 26, 10)
	fill(13, 10, 19, 25)
	fill(9, 25, 23, 29)
	// Tick marks on the T's bar evoke a screenshot selection.
	fill(4, 4, 8, 8)
	fill(24, 4, 28, 8)
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		return nil
	}
	return buf.Bytes()
}
