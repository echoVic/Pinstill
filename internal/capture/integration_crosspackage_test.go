//go:build darwin && integration

package capture

import (
	"bytes"
	"image/png"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/echoVic/Pinstill/internal/store"
)

// TestIntegrationCaptureThenStore captures a real region and persists it in a
// throwaway history store to prove the two packages compose.
func TestIntegrationCaptureThenStore(t *testing.T) {
	if !PermissionGranted() {
		t.Skip("screen recording permission is not granted")
	}
	pngBytes, err := CaptureRegion(Rect{X: 100, Y: 100, Width: 120, Height: 80}, CaptureOptions{})
	if err != nil {
		t.Fatalf("CaptureRegion() error = %v", err)
	}
	dir := filepath.Join(t.TempDir(), "captures")
	st, err := store.Open(dir)
	if err != nil {
		t.Fatalf("store.Open() error = %v", err)
	}
	entry, err := st.Save(pngBytes, time.Now())
	if err != nil {
		t.Fatalf("store.Save() error = %v", err)
	}
	if entry.Width < 120 || entry.Height < 80 {
		t.Fatalf("entry size = %dx%d, want at least 120x80", entry.Width, entry.Height)
	}
	if _, err := os.Stat(filepath.Join(dir, entry.Thumb)); err != nil {
		t.Fatalf("thumbnail missing: %v", err)
	}
	reopened, err := store.Open(dir)
	if err != nil {
		t.Fatalf("store.Open() reopen error = %v", err)
	}
	if entries := reopened.List(); len(entries) != 1 || entries[0].ID != entry.ID {
		t.Fatalf("reopened List() = %+v, want entry %q", entries, entry.ID)
	}
	data, err := reopened.ReadPNG(entry.ID)
	if err != nil {
		t.Fatalf("ReadPNG() error = %v", err)
	}
	config, err := png.DecodeConfig(bytes.NewReader(data))
	if err != nil || config.Width != entry.Width || config.Height != entry.Height {
		t.Fatalf("stored PNG config = %+v, %v; entry = %dx%d", config, err, entry.Width, entry.Height)
	}
	t.Logf("saved real capture: id=%s size=%dx%d bytes=%d", entry.ID, entry.Width, entry.Height, entry.Size)
}
