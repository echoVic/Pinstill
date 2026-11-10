package app

import (
	"bytes"
	"encoding/base64"
	"image/png"
	"os"
	"path/filepath"
	"testing"
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
		{name: "undecodable bytes", png: []byte("definitely not a png"), want: false},
		{name: "empty bytes", png: nil, want: false},
		{name: "jpeg bytes", png: gnJPEGData(t, 10, 10, 80), want: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := blankPNG(tt.png); got != tt.want {
				t.Fatalf("blankPNG() = %v, want %v", got, tt.want)
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

func TestUniquePath(t *testing.T) {
	dir := t.TempDir()
	base := filepath.Join(dir, "Pinstill-2026-11-09.png")

	if got := uniquePath(base); got != base {
		t.Fatalf("uniquePath(unused) = %q, want %q", got, base)
	}
	if err := os.WriteFile(base, []byte("x"), 0o644); err != nil {
		t.Fatalf("os.WriteFile() error = %v", err)
	}
	second := filepath.Join(dir, "Pinstill-2026-11-09-2.png")
	if got := uniquePath(base); got != second {
		t.Fatalf("uniquePath(existing) = %q, want %q", got, second)
	}
	if err := os.WriteFile(second, []byte("x"), 0o644); err != nil {
		t.Fatalf("os.WriteFile() error = %v", err)
	}
	third := filepath.Join(dir, "Pinstill-2026-11-09-3.png")
	if got := uniquePath(base); got != third {
		t.Fatalf("uniquePath(existing twice) = %q, want %q", got, third)
	}

	noExtension := filepath.Join(dir, "Pinstill")
	if err := os.WriteFile(noExtension, []byte("x"), 0o644); err != nil {
		t.Fatalf("os.WriteFile() error = %v", err)
	}
	if got, want := uniquePath(noExtension), filepath.Join(dir, "Pinstill-2"); got != want {
		t.Fatalf("uniquePath(no extension) = %q, want %q", got, want)
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

func TestTrayIconPNG(t *testing.T) {
	data := trayIconPNG()
	if len(data) == 0 {
		t.Fatal("trayIconPNG() returned no data")
	}
	if !bytes.HasPrefix(data, gnPNGMagic()) {
		t.Fatalf("trayIconPNG() does not start with the PNG signature: %x", data[:min(8, len(data))])
	}
	config, err := png.DecodeConfig(bytes.NewReader(data))
	if err != nil {
		t.Fatalf("png.DecodeConfig(tray icon) error = %v", err)
	}
	if config.Width != 32 || config.Height != 32 {
		t.Fatalf("tray icon = %dx%d, want 32x32", config.Width, config.Height)
	}

	// The glyph is a "P" now (it used to be the old "T" mark): the stem runs
	// the full height, a bowl sits on the upper right and its counter is a
	// hole. These probes catch a blank or stale glyph.
	img, err := png.Decode(bytes.NewReader(data))
	if err != nil {
		t.Fatalf("png.Decode(tray icon) error = %v", err)
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
		{"stem top", 10, 6, true},
		{"stem bottom", 10, 25, true},
		{"bowl right", 21, 10, true},
		{"bowl counter is a hole", 16, 11, false},
		{"right of lower stem", 20, 25, false},
		{"outside the glyph", 1, 1, false},
	}
	for _, c := range checks {
		got := pixelAlpha(c.x, c.y) > 0x8000
		if got != c.opaque {
			t.Errorf("tray icon pixel (%d,%d) opaque=%v, want %v (%s)", c.x, c.y, got, c.opaque, c.name)
		}
	}
}
