package app

import (
	"bytes"
	"encoding/base64"
	"errors"
	"image/png"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"syscall"
	"testing"
	"time"

	"github.com/echoVic/Pinstill/internal/imagebudget"
)

func TestDecodeImageBase64(t *testing.T) {
	payload := []byte("hello pinstill")
	encoded := base64.StdEncoding.EncodeToString(payload)
	wrapped := encoded[:4] + "\n" + encoded[4:]

	tests := []struct {
		name    string
		input   string
		want    []byte
		wantErr bool
	}{
		{name: "raw base64", input: encoded, want: payload},
		{name: "data url png", input: "data:image/png;base64," + encoded, want: payload},
		{name: "data url jpeg", input: "data:image/jpeg;base64," + encoded, want: payload},
		{name: "surrounding whitespace is trimmed", input: "  \n" + encoded + "\t ", want: payload},
		{name: "newlines inside base64 are ignored", input: wrapped, want: payload},
		{name: "invalid base64", input: "!!!not base64!!!", wantErr: true},
		{name: "empty input", input: "", wantErr: true},
		{name: "whitespace only", input: "   \n\t", wantErr: true},
		{name: "data url without payload", input: "data:image/png;base64,", wantErr: true},
		{name: "data url without comma", input: "data:image/png;base64" + encoded, wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := decodeImageBase64(tt.input)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("decodeImageBase64(%q) returned nil error, want error", tt.input)
				}
				return
			}
			if err != nil {
				t.Fatalf("decodeImageBase64(%q) error = %v", tt.input, err)
			}
			if !bytes.Equal(got, tt.want) {
				t.Fatalf("decodeImageBase64(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}

func TestBlankPNG(t *testing.T) {
	tests := []struct {
		name string
		png  []byte
		want bool
	}{
		{name: "fully transparent small", png: gnTransparentPNG(t, 10, 10), want: true},
		{name: "fully transparent large", png: gnTransparentPNG(t, 200, 200), want: true},
		{name: "opaque gradient", png: gnPNGData(t, 10, 10), want: false},
		{name: "single visible pixel", png: gnTransparentPNGWithPixel(t, 10, 10, 3, 4), want: false},
		{name: "single visible pixel at origin", png: gnTransparentPNGWithPixel(t, 100, 100, 0, 0), want: false},
		{name: "single visible pixel between samples", png: gnTransparentPNGWithPixel(t, 200, 200, 1, 1), want: false},
		{name: "undecodable bytes", png: []byte("definitely not a png"), want: false},
		{name: "empty bytes", png: nil, want: false},
		{name: "jpeg bytes", png: gnJPEGData(t, 10, 10, 80), want: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			img, err := imagebudget.DecodePNG(tt.png)
			if got := err == nil && blankCapture(img); got != tt.want {
				t.Fatalf("blankCapture() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestExpandHome(t *testing.T) {
	home, err := os.UserHomeDir()
	if err != nil {
		t.Fatalf("os.UserHomeDir() error = %v", err)
	}
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{name: "empty falls back to Pictures/Pinstill", input: "", want: filepath.Join(home, "Pictures", "Pinstill")},
		{name: "tilde", input: "~", want: home},
		{name: "tilde slash", input: "~/", want: home},
		{name: "tilde subdirectory", input: "~/Pictures/Pinstill", want: filepath.Join(home, "Pictures", "Pinstill")},
		{name: "tilded name is not expanded", input: "~foo", want: "~foo"},
		{name: "absolute path", input: "/tmp/pinstill", want: "/tmp/pinstill"},
		{name: "absolute path is trimmed", input: "  /tmp/pinstill  ", want: "/tmp/pinstill"},
		{name: "relative path", input: "Pictures/Pinstill", want: "Pictures/Pinstill"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := expandHome(tt.input)
			if err != nil {
				t.Fatalf("expandHome(%q) error = %v", tt.input, err)
			}
			if got != tt.want {
				t.Fatalf("expandHome(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}

func TestWriteUniqueFile(t *testing.T) {
	dir := t.TempDir()
	base := filepath.Join(dir, "Pinstill-2026-11-09.png")
	second := filepath.Join(dir, "Pinstill-2026-11-09-2.png")
	third := filepath.Join(dir, "Pinstill-2026-11-09-3.png")
	written := make(map[string]string)
	for i, want := range []string{base, second, third} {
		data := []byte{byte('a' + i)}
		got, err := writeUniqueFile(base, data)
		if err != nil || got != want {
			t.Fatalf("writeUniqueFile() = (%q, %v), want %q", got, err, want)
		}
		written[got] = string(data)
	}
	for path, want := range written {
		data, err := os.ReadFile(path)
		if err != nil || string(data) != want {
			t.Fatalf("saved data at %s = %q, %v; want %q", path, data, err, want)
		}
	}

	noExtension := filepath.Join(dir, "Pinstill")
	if err := os.WriteFile(noExtension, []byte("x"), 0o644); err != nil {
		t.Fatalf("os.WriteFile() error = %v", err)
	}
	got, err := writeUniqueFile(noExtension, []byte("new"))
	if want := filepath.Join(dir, "Pinstill-2"); err != nil || got != want {
		t.Fatalf("writeUniqueFile(no extension) = (%q, %v), want %q", got, err, want)
	}
}

func TestRandomSuffix(t *testing.T) {
	const hexChars = "0123456789abcdef"
	for i := 0; i < 50; i++ {
		got := randomSuffix()
		if len(got) != 4 {
			t.Fatalf("randomSuffix() = %q, want 4 characters", got)
		}
		for _, r := range got {
			if !bytes.ContainsRune([]byte(hexChars), r) {
				t.Fatalf("randomSuffix() = %q, want lowercase hex characters", got)
			}
		}
	}
}

func TestStartCaptureSoundReapsChild(t *testing.T) {
	const childEnv = "PINSTILL_TEST_SOUND_EXIT"
	if code := os.Getenv(childEnv); code != "" {
		// This child only waits for stdin to close, then exits. No audio or
		// other native integration is launched by the test.
		_, _ = io.Copy(io.Discard, os.Stdin)
		exitCode, _ := strconv.Atoi(code)
		os.Exit(exitCode)
	}
	if runtime.GOOS == "windows" {
		t.Skip("signal 0 liveness probe is unavailable on Windows; capture sound is macOS-only")
	}
	for _, exitCode := range []string{"0", "7"} {
		t.Run("exit_"+exitCode, func(t *testing.T) {
			cmd := exec.Command(os.Args[0], "-test.run=^TestStartCaptureSoundReapsChild$")
			cmd.Env = append(os.Environ(), childEnv+"="+exitCode)
			input, err := cmd.StdinPipe()
			if err != nil {
				t.Fatal(err)
			}
			returned := make(chan struct{})
			go func() {
				startCaptureSound(cmd)
				close(returned)
			}()
			select {
			case <-returned:
			case <-time.After(5 * time.Second):
				_ = input.Close()
				<-returned
				t.Fatal("sound launch blocked until playback finished")
			}
			if cmd.Process == nil {
				_ = input.Close()
				t.Fatal("sound subprocess was not started")
			}
			t.Cleanup(func() {
				_ = input.Close()
				_ = cmd.Process.Kill()
				_, _ = cmd.Process.Wait()
			})
			if err := cmd.Process.Signal(syscall.Signal(0)); err != nil {
				t.Fatalf("sound subprocess did not remain alive while waiting for stdin: %v", err)
			}
			if err := input.Close(); err != nil {
				t.Fatal(err)
			}
			timeout := time.NewTimer(5 * time.Second)
			defer timeout.Stop()
			tick := time.NewTicker(10 * time.Millisecond)
			defer tick.Stop()
			for {
				// Process.Signal is synchronized internally; reading Cmd.ProcessState
				// while Wait fills it would itself race.
				if errors.Is(cmd.Process.Signal(syscall.Signal(0)), os.ErrProcessDone) {
					return
				}
				select {
				case <-tick.C:
				case <-timeout.C:
					t.Fatal("exited sound subprocess was not reaped")
				}
			}
		})
	}
}

func TestTrayIconPNG(t *testing.T) {
	data := trayIconPNG
	if len(data) == 0 {
		t.Fatal("embedded tray icon is empty")
	}
	if !bytes.HasPrefix(data, gnPNGMagic()) {
		t.Fatalf("tray icon does not start with the PNG signature: %x", data[:min(8, len(data))])
	}
	config, err := png.DecodeConfig(bytes.NewReader(data))
	if err != nil {
		t.Fatalf("png.DecodeConfig(tray icon) error = %v", err)
	}
	if config.Width != 32 || config.Height != 32 {
		t.Fatalf("tray icon = %dx%d, want 32x32", config.Width, config.Height)
	}

	img, err := png.Decode(bytes.NewReader(data))
	if err != nil {
		t.Fatalf("png.Decode(tray icon) error = %v", err)
	}
	for row := 0; row < config.Height; row++ {
		for col := 0; col < config.Width; col++ {
			red, green, blue, _ := img.At(col, row).RGBA()
			if red != 0 || green != 0 || blue != 0 {
				t.Fatalf("tray icon pixel (%d,%d) is not black for template tinting", col, row)
			}
		}
	}
	pixelAlpha := func(x, y int) uint32 {
		_, _, _, a := img.At(x, y).RGBA()
		return a
	}
	checks := []struct {
		name   string
		x, y   int
		opaque bool
	}{
		{"upper crop corner", 5, 9, true},
		{"lower crop corner", 15, 24, true},
		{"pin head", 23, 6, true},
		{"pin needle", 23, 20, true},
		{"open space between the marks", 15, 10, false},
		{"outside the glyph", 1, 1, false},
	}
	for _, c := range checks {
		got := pixelAlpha(c.x, c.y) > 0x8000
		if got != c.opaque {
			t.Errorf("tray icon pixel (%d,%d) opaque=%v, want %v (%s)", c.x, c.y, got, c.opaque, c.name)
		}
	}
}
