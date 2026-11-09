package pin

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSaveAndReadImage(t *testing.T) {
	s := openTestStore(t)
	pngBytes := testPNG(t, 120, 80)
	file, err := s.SaveImage("pin-1", pngBytes)
	if err != nil {
		t.Fatalf("SaveImage() error = %v", err)
	}
	if file != "pin-1.png" {
		t.Fatalf("SaveImage() file = %q, want pin-1.png", file)
	}
	info, err := os.Stat(filepath.Join(s.Dir(), file))
	if err != nil {
		t.Fatalf("os.Stat(%q) error = %v", file, err)
	}
	if info.IsDir() || info.Size() != int64(len(pngBytes)) {
		t.Fatalf("stored image info = %v, want a regular %d byte file", info, len(pngBytes))
	}
	if perm := info.Mode().Perm(); perm != 0o644 {
		t.Fatalf("image mode = %o, want 0644", perm)
	}

	// Both an explicit File and a derived file name must be readable.
	for _, st := range []State{
		{ID: "pin-1", File: file},
		{ID: "pin-1"},
	} {
		data, err := s.ReadImage(st)
		if err != nil {
			t.Fatalf("ReadImage(%+v) error = %v", st, err)
		}
		if !bytes.Equal(data, pngBytes) {
			t.Fatalf("ReadImage(%+v) returned %d bytes, want the saved image", st, len(data))
		}
	}

	// Saving the same ID again replaces the image.
	replacement := testPNG(t, 32, 24)
	if _, err := s.SaveImage("pin-1", replacement); err != nil {
		t.Fatalf("SaveImage(replacement) error = %v", err)
	}
	data, err := s.ReadImage(State{ID: "pin-1"})
	if err != nil {
		t.Fatalf("ReadImage() after replacement error = %v", err)
	}
	if !bytes.Equal(data, replacement) {
		t.Fatal("ReadImage() returned the old image after a replacement save")
	}
}

func TestSaveImageRejectsInvalidPNG(t *testing.T) {
	s := openTestStore(t)
	if _, err := s.SaveImage("pin-1", testPNG(t, 8, 8)); err != nil {
		t.Fatalf("SaveImage(valid) error = %v", err)
	}
	stored, err := os.ReadFile(filepath.Join(s.Dir(), "pin-1.png"))
	if err != nil {
		t.Fatalf("os.ReadFile() error = %v", err)
	}

	invalid := []struct {
		name string
		data []byte
	}{
		{name: "nil", data: nil},
		{name: "empty", data: []byte{}},
		{name: "text", data: []byte("not an image")},
		{name: "png signature only", data: []byte("\x89PNG\r\n\x1a\n")},
		{name: "gif", data: []byte("GIF89a")},
	}
	for _, tt := range invalid {
		t.Run(tt.name, func(t *testing.T) {
			if _, err := s.SaveImage("pin-1", tt.data); !errors.Is(err, ErrInvalidImage) {
				t.Fatalf("SaveImage() error = %v, want ErrInvalidImage", err)
			}
		})
	}
	// A failed save must neither create nor clobber the image.
	after, err := os.ReadFile(filepath.Join(s.Dir(), "pin-1.png"))
	if err != nil {
		t.Fatalf("os.ReadFile() after failed saves error = %v", err)
	}
	if !bytes.Equal(after, stored) {
		t.Fatal("a failed SaveImage clobbered the existing image")
	}
	dirEntries, err := os.ReadDir(s.Dir())
	if err != nil {
		t.Fatalf("os.ReadDir() error = %v", err)
	}
	for _, entry := range dirEntries {
		if entry.Name() != indexFileName && entry.Name() != "pin-1.png" {
			t.Fatalf("unexpected file %q after failed saves", entry.Name())
		}
	}
}

func TestSaveImageUnsafeIDs(t *testing.T) {
	s := openTestStore(t)
	invalid := []string{
		"",
		"../evil",
		"..",
		".",
		"a/b",
		"a\\b",
		"a b",
		"a\tb",
		"a\x00b",
		"CON",
		"con",
		"LPT1",
		"pin:1",
		"pin*1",
		strings.Repeat("a", maxIDLength+1),
	}
	for _, id := range invalid {
		if _, err := s.SaveImage(id, testPNG(t, 8, 8)); !errors.Is(err, ErrInvalidID) {
			t.Fatalf("SaveImage(%q) error = %v, want ErrInvalidID", id, err)
		}
	}

	// Ugly but path-safe IDs are accepted and become files inside Dir().
	valid := []string{"pin-1", "PIN_2", "a..b", "x.y-z_9", strings.Repeat("a", maxIDLength)}
	for _, id := range valid {
		file, err := s.SaveImage(id, testPNG(t, 8, 8))
		if err != nil {
			t.Fatalf("SaveImage(%q) error = %v", id, err)
		}
		if filepath.Dir(filepath.Join(s.Dir(), file)) != s.Dir() {
			t.Fatalf("SaveImage(%q) escaped the store directory: %q", id, file)
		}
	}
}

func TestReadImageErrors(t *testing.T) {
	s := openTestStore(t)
	if _, err := s.SaveImage("pin-1", testPNG(t, 8, 8)); err != nil {
		t.Fatalf("SaveImage() error = %v", err)
	}
	tests := []struct {
		name    string
		state   State
		wantErr error
	}{
		{name: "traversal file", state: State{ID: "pin-1", File: "../evil.png"}, wantErr: ErrInvalidFile},
		{name: "absolute file", state: State{ID: "pin-1", File: "/tmp/evil.png"}, wantErr: ErrInvalidFile},
		{name: "nested file", state: State{ID: "pin-1", File: "sub/pin-1.png"}, wantErr: ErrInvalidFile},
		{name: "wrong extension", state: State{ID: "pin-1", File: "pin-1.jpg"}, wantErr: ErrInvalidFile},
		{name: "reserved file", state: State{ID: "pin-1", File: "CON.png"}, wantErr: ErrInvalidFile},
		{name: "unsafe derived id", state: State{ID: "../pin-1"}},
		{name: "missing image", state: State{ID: "pin-2", File: "pin-2.png"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := s.ReadImage(tt.state)
			if err == nil {
				t.Fatalf("ReadImage(%+v) returned nil error", tt.state)
			}
			if tt.wantErr != nil && !errors.Is(err, tt.wantErr) {
				t.Fatalf("ReadImage() error = %v, want %v", err, tt.wantErr)
			}
		})
	}
}

func TestOpenDropsImageThatIsADirectory(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "pins")
	if _, err := Open(dir); err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	writePinsIndex(t, dir, indexFile{Version: indexVersion, Pins: []State{
		{ID: "pin-9", File: "pin-9.png", Width: 64, Height: 64, Opacity: 100},
	}})
	// A directory named like an image must not be accepted as an image file.
	if err := os.MkdirAll(filepath.Join(dir, "pin-9.png"), 0o755); err != nil {
		t.Fatalf("os.MkdirAll(pin-9.png) error = %v", err)
	}
	reopened, err := Open(dir)
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	if states := reopened.List(); len(states) != 0 {
		t.Fatalf("List() = %+v, want empty (image path is a directory)", states)
	}
}

func TestOpenRemovesStaleTempFiles(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "pins")
	s, err := Open(dir)
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	mustSaveImage(t, s, "pin-1", 48, 48)
	mustUpsert(t, s, State{ID: "pin-1", Width: 48, Height: 48})
	stale := []string{".pins.json.tmp-123456", ".pin-1.png.tmp-abcdef"}
	for _, name := range stale {
		if err := os.WriteFile(filepath.Join(dir, name), []byte("partial"), 0o644); err != nil {
			t.Fatalf("os.WriteFile(%q) error = %v", name, err)
		}
	}

	if _, err := Open(dir); err != nil {
		t.Fatalf("Open() with stale temp files error = %v", err)
	}
	for _, name := range stale {
		if fileExists(t, filepath.Join(dir, name)) {
			t.Fatalf("stale temp file %q survived Open", name)
		}
	}
	if !fileExists(t, filepath.Join(dir, "pin-1.png")) {
		t.Fatal("real image was removed together with the stale temp files")
	}
	if index := readPinsIndex(t, dir); len(index.Pins) != 1 {
		t.Fatalf("index = %+v, want the one pin to survive cleanup", index.Pins)
	}
}

func TestValidateIDAndFile(t *testing.T) {
	validIDs := []string{"pin-1", "a", "A1", "pin-20261109-213000-ab12", "x..y", "x_y.z-1"}
	for _, id := range validIDs {
		if _, err := validateID(id); err != nil {
			t.Fatalf("validateID(%q) error = %v", id, err)
		}
	}
	invalidIDs := []string{"", " ", ".", "..", "-pin", "_pin", ".pin", "a/b", "a\\b", "CON", "nul", "LPT9", "CON.png", "nul.txt", "pin 1", "pin:1"}
	for _, id := range invalidIDs {
		if _, err := validateID(id); !errors.Is(err, ErrInvalidID) {
			t.Fatalf("validateID(%q) error = %v, want ErrInvalidID", id, err)
		}
	}

	validFiles := []string{"pin-1.png", "a.png", "x.y-z_9.png"}
	for _, file := range validFiles {
		if _, err := validateFile(file); err != nil {
			t.Fatalf("validateFile(%q) error = %v", file, err)
		}
	}
	invalidFiles := []string{"", "pin-1.jpg", "pin-1", "../pin-1.png", "sub/pin-1.png", "/pin-1.png", ".png", "-pin.png", "CON.png", "CON.png.png", "NUL.foo.png", "pin 1.png"}
	for _, file := range invalidFiles {
		if _, err := validateFile(file); !errors.Is(err, ErrInvalidFile) {
			t.Fatalf("validateFile(%q) error = %v, want ErrInvalidFile", file, err)
		}
	}
}

func TestNormalizeOpacity(t *testing.T) {
	tests := []struct {
		in   float64
		want float64
	}{
		{in: 0, want: 100},
		{in: -1, want: 100},
		{in: 1, want: 10},
		{in: 9.99, want: 10},
		{in: 10, want: 10},
		{in: 33.3, want: 33.3},
		{in: 100, want: 100},
		{in: 100.1, want: 100},
	}
	for _, tt := range tests {
		if got := normalizeOpacity(tt.in); got != tt.want {
			t.Fatalf("normalizeOpacity(%v) = %v, want %v", tt.in, got, tt.want)
		}
	}
}
