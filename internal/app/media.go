package app

import (
	"bytes"
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"github.com/echoVic/Pinstill/internal/imagebudget"
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
	if base64.StdEncoding.DecodedLen(len(data)) > imagebudget.MaxEncodedBytes {
		return nil, errors.New("图片数据超过 64 MiB 限制")
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
	img, err := imagebudget.DecodePNG(pngBytes)
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
	// Sampling quickly finds typical captures, but a visible pixel between
	// samples must not be mistaken for a screen-recording permission failure.
	if stepX > 1 || stepY > 1 {
		for y := b.Min.Y; y < b.Max.Y; y++ {
			for x := b.Min.X; x < b.Max.X; x++ {
				_, _, _, a := img.At(x, y).RGBA()
				if a != 0 {
					return false
				}
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
	startCaptureSound(cmd)
}

func startCaptureSound(cmd *exec.Cmd) {
	if err := cmd.Start(); err != nil {
		return
	}
	// Playback stays asynchronous, but every started process must be reaped.
	go func() { _ = cmd.Wait() }()
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
		return filepath.Join(home, "Pictures", "Pinstill"), nil
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

// writeUniqueFile exclusively creates path, appending a counter before the
// extension on collisions. Choosing the name and creating it must be one
// filesystem operation so simultaneous exports cannot overwrite each other.
func writeUniqueFile(path string, data []byte) (string, error) {
	ext := filepath.Ext(path)
	base := strings.TrimSuffix(path, ext)
	candidate := path
	for i := 2; ; i++ {
		file, err := os.OpenFile(candidate, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o644)
		if os.IsExist(err) {
			candidate = fmt.Sprintf("%s-%d%s", base, i, ext)
			continue
		}
		if err != nil {
			return "", err
		}
		if _, err := file.Write(data); err != nil {
			_ = file.Close()
			_ = os.Remove(candidate)
			return "", err
		}
		if err := file.Close(); err != nil {
			_ = os.Remove(candidate)
			return "", err
		}
		return candidate, nil
	}
}

// trayIconPNG draws the menu bar icon: a black template "P" for Pinstill.
// macOS tints template icons with the menu bar's appearance; Windows shows
// them white on its dark taskbar.
//
// The glyph is rasterised at 8x and box-filtered down, so the rounded stem
// and bowl stay smooth at menu bar sizes (16-22 pt) instead of looking like
// stair-stepped pixel art.
func trayIconPNG() []byte {
	const size = 32
	const ss = 8
	const s = size * ss
	// mask[i] reports whether the glyph covers supersampled pixel i.
	mask := make([]bool, s*s)
	roundRect := func(x0, y0, x1, y1, r int, on bool) {
		for y := max(0, y0); y < min(s, y1); y++ {
			for x := max(0, x0); x < min(s, x1); x++ {
				dx, dy := 0, 0
				if x < x0+r {
					dx = x0 + r - x
				} else if x >= x1-r {
					dx = x - (x1 - r - 1)
				}
				if y < y0+r {
					dy = y0 + r - y
				} else if y >= y1-r {
					dy = y - (y1 - r - 1)
				}
				if dx*dx+dy*dy <= r*r {
					mask[y*s+x] = on
				}
			}
		}
	}
	// Bold P: a rounded stem with a bowl on the upper right.
	roundRect(8*ss, 4*ss, 13*ss, 28*ss, 2*ss, true)   // stem
	roundRect(8*ss, 4*ss, 24*ss, 20*ss, 7*ss, true)   // bowl
	roundRect(13*ss, 8*ss, 19*ss, 16*ss, 3*ss, false) // counter

	img := image.NewNRGBA(image.Rect(0, 0, size, size))
	for y := 0; y < size; y++ {
		for x := 0; x < size; x++ {
			covered := 0
			for sy := 0; sy < ss; sy++ {
				row := (y*ss + sy) * s
				for sx := 0; sx < ss; sx++ {
					if mask[row+x*ss+sx] {
						covered++
					}
				}
			}
			if covered == 0 {
				continue
			}
			alpha := uint8((covered*255 + ss*ss/2) / (ss * ss))
			img.SetNRGBA(x, y, color.NRGBA{R: 0, G: 0, B: 0, A: alpha})
		}
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		return nil
	}
	return buf.Bytes()
}
