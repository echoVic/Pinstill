package store

import (
	"bytes"
	"image/jpeg"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestThumbnailDimensions(t *testing.T) {
	tests := []struct {
		name                  string
		width, height         int
		wantWidth, wantHeight int
	}{
		{name: "landscape large", width: 800, height: 600, wantWidth: 320, wantHeight: 240},
		{name: "landscape exactly 4:3", width: 640, height: 480, wantWidth: 320, wantHeight: 240},
		{name: "portrait", width: 600, height: 900, wantWidth: 213, wantHeight: 320},
		{name: "wide", width: 1000, height: 400, wantWidth: 320, wantHeight: 128},
		{name: "small is not upscaled", width: 200, height: 100, wantWidth: 200, wantHeight: 100},
		{name: "exactly max edge", width: 320, height: 320, wantWidth: 320, wantHeight: 320},
		{name: "empty", width: 0, height: 10, wantWidth: 0, wantHeight: 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			width, height := thumbnailDimensions(tt.width, tt.height)
			if width != tt.wantWidth || height != tt.wantHeight {
				t.Fatalf("thumbnailDimensions(%d, %d) = %dx%d, want %dx%d",
					tt.width, tt.height, width, height, tt.wantWidth, tt.wantHeight)
			}
		})
	}
}

func TestSavedThumbnailIs320JPEG(t *testing.T) {
	tests := []struct {
		width, height         int
		wantWidth, wantHeight int
	}{
		{width: 800, height: 600, wantWidth: 320, wantHeight: 240},
		{width: 600, height: 900, wantWidth: 213, wantHeight: 320},
		{width: 200, height: 100, wantWidth: 200, wantHeight: 100},
	}
	for _, tt := range tests {
		s := testStore(t)
		entry := mustSave(t, s, testPNG(t, tt.width, tt.height), time.Now())
		thumbPath := filepath.Join(s.Dir(), entry.Thumb)
		data, err := os.ReadFile(thumbPath)
		if err != nil {
			t.Fatalf("os.ReadFile(thumbnail) error = %v", err)
		}
		if len(data) < 2 || data[0] != 0xFF || data[1] != 0xD8 {
			t.Fatalf("thumbnail for %dx%d does not start with a JPEG SOI marker", tt.width, tt.height)
		}
		config, err := jpeg.DecodeConfig(bytes.NewReader(data))
		if err != nil {
			t.Fatalf("jpeg.DecodeConfig() error = %v", err)
		}
		if config.Width != tt.wantWidth || config.Height != tt.wantHeight {
			t.Fatalf("thumbnail for %dx%d = %dx%d, want %dx%d",
				tt.width, tt.height, config.Width, config.Height, tt.wantWidth, tt.wantHeight)
		}
		if entry.Thumb != entry.ID+thumbExt {
			t.Fatalf("Thumb = %q, want %q", entry.Thumb, entry.ID+thumbExt)
		}
	}
}
