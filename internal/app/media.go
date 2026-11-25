package app

import (
	"crypto/rand"
	_ "embed"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"image"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/echoVic/Pinstill/internal/imagebudget"
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

// blankCapture reports whether the image has no visible pixels at all, which is
// how macOS delivers a capture taken without the screen recording
// permission.
func blankCapture(img image.Image) bool {
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

//go:embed tray-icon.png
var trayIconPNG []byte
