package pin

import (
	"bytes"
	"encoding/json"
	"errors"
	"image"
	"image/color"
	"image/jpeg"
	"image/png"
	"math"
	"os"
	"path/filepath"
	"slices"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// testPNG builds a deterministic, fully opaque PNG of the requested size.
func testPNG(t *testing.T, width, height int) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, width, height))
	for y := 0; y < height; y++ {
		for x := 0; x < width; x++ {
			img.SetRGBA(x, y, color.RGBA{
				R: uint8(x % 256),
				G: uint8(y % 256),
				B: uint8((x + y) % 256),
				A: 255,
			})
		}
	}
	var buffer bytes.Buffer
	if err := png.Encode(&buffer, img); err != nil {
		t.Fatalf("png.Encode(%dx%d) error = %v", width, height, err)
	}
	return buffer.Bytes()
}

// openTestStore opens a store in a fresh temporary directory.
func openTestStore(t *testing.T) *Store {
	t.Helper()
	store, err := Open(filepath.Join(t.TempDir(), "pins"))
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	return store
}

// mustSaveImage saves a test PNG and returns the relative file name.
func mustSaveImage(t *testing.T, s *Store, id string, width, height int) string {
	t.Helper()
	file, err := s.SaveImage(id, testPNG(t, width, height))
	if err != nil {
		t.Fatalf("SaveImage(%q) error = %v", id, err)
	}
	return file
}

// mustUpsert stores a state and returns the normalized result.
func mustUpsert(t *testing.T, s *Store, st State) State {
	t.Helper()
	stored, err := s.Upsert(st)
	if err != nil {
		t.Fatalf("Upsert(%+v) error = %v", st, err)
	}
	return stored
}

// listedIDs returns the IDs from List in order.
func listedIDs(s *Store) []string {
	states := s.List()
	ids := make([]string, len(states))
	for i, state := range states {
		ids[i] = state.ID
	}
	return ids
}

// sameState compares two states, treating equal instants as equal CreatedAt
// values (a JSON round trip drops the monotonic clock reading).
func sameState(a, b State) bool {
	return a.ID == b.ID && a.File == b.File && a.X == b.X && a.Y == b.Y &&
		a.Width == b.Width && a.Height == b.Height && a.Opacity == b.Opacity &&
		a.CreatedAt.Equal(b.CreatedAt)
}

// sameStates reports whether both slices contain the same states in order.
func sameStates(a, b []State) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if !sameState(a[i], b[i]) {
			return false
		}
	}
	return true
}

// readPinsIndex decodes pins.json from dir.
func readPinsIndex(t *testing.T, dir string) indexFile {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(dir, indexFileName))
	if err != nil {
		t.Fatalf("os.ReadFile(pins.json) error = %v", err)
	}
	var index indexFile
	if err := json.Unmarshal(data, &index); err != nil {
		t.Fatalf("pins.json is not valid JSON: %v", err)
	}
	return index
}

// writePinsIndex writes an arbitrary index file, bypassing validation.
func writePinsIndex(t *testing.T, dir string, index indexFile) {
	t.Helper()
	data, err := json.Marshal(index)
	if err != nil {
		t.Fatalf("json.Marshal(index) error = %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, indexFileName), data, 0o644); err != nil {
		t.Fatalf("os.WriteFile(pins.json) error = %v", err)
	}
}

// writePinsJSON writes raw pins.json content.
func writePinsJSON(t *testing.T, dir, content string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, indexFileName), []byte(content), 0o644); err != nil {
		t.Fatalf("os.WriteFile(pins.json) error = %v", err)
	}
}

// fileExists reports whether path exists.
func fileExists(t *testing.T, path string) bool {
	t.Helper()
	_, err := os.Stat(path)
	return err == nil
}

func TestOpenCreatesDirectoryAndEmptyIndex(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "nested", "pins")
	s, err := Open(dir)
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	if s.Dir() != dir {
		t.Fatalf("Dir() = %q, want %q", s.Dir(), dir)
	}
	info, err := os.Stat(dir)
	if err != nil || !info.IsDir() {
		t.Fatalf("store directory was not created: %v", err)
	}
	if states := s.List(); len(states) != 0 {
		t.Fatalf("List() = %+v, want empty", states)
	}
	index := readPinsIndex(t, dir)
	if index.Version != indexVersion || len(index.Pins) != 0 {
		t.Fatalf("index = %+v, want version %d with no pins", index, indexVersion)
	}

	reopened, err := Open(dir)
	if err != nil {
		t.Fatalf("re-Open() error = %v", err)
	}
	if len(reopened.List()) != 0 {
		t.Fatalf("List() after reopen = %+v, want empty", reopened.List())
	}
}

func TestOpenRejectsEmptyDirectory(t *testing.T) {
	for _, dir := range []string{"", "   "} {
		if _, err := Open(dir); err == nil {
			t.Fatalf("Open(%q) returned nil error", dir)
		}
	}
}

func TestUpsertNormalizesState(t *testing.T) {
	s := openTestStore(t)
	createdAt := time.Date(2026, 11, 9, 21, 30, 0, 123456789, time.UTC)
	mustSaveImage(t, s, "pin-1", 8, 8)

	stored := mustUpsert(t, s, State{
		ID:        "pin-1",
		X:         -40,
		Y:         20,
		Width:     10,
		Height:    -5,
		Opacity:   5,
		CreatedAt: createdAt,
	})
	if stored.File != "pin-1.png" {
		t.Fatalf("File = %q, want pin-1.png", stored.File)
	}
	if stored.X != -40 || stored.Y != 20 {
		t.Fatalf("position = (%d,%d), want (-40,20)", stored.X, stored.Y)
	}
	if stored.Width != minPinSize || stored.Height != minPinSize {
		t.Fatalf("size = %dx%d, want %dx%d", stored.Width, stored.Height, minPinSize, minPinSize)
	}
	if stored.Opacity != minOpacity {
		t.Fatalf("Opacity = %v, want %v", stored.Opacity, minOpacity)
	}
	if !stored.CreatedAt.Equal(createdAt.Round(0)) {
		t.Fatalf("CreatedAt = %v, want %v", stored.CreatedAt, createdAt.Round(0))
	}

	// The normalized value must be what List and a reopened store expose.
	if states := s.List(); len(states) != 1 || !sameState(states[0], stored) {
		t.Fatalf("List() = %+v, want [%+v]", states, stored)
	}
	reopened, err := Open(s.Dir())
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	if states := reopened.List(); len(states) != 1 || !sameState(states[0], stored) {
		t.Fatalf("reopened List() = %+v, want [%+v]", states, stored)
	}
}

func TestUpsertOpacityNormalization(t *testing.T) {
	tests := []struct {
		name    string
		opacity float64
		want    float64
	}{
		{name: "unset defaults to opaque", opacity: 0, want: 100},
		{name: "negative defaults to opaque", opacity: -10, want: 100},
		{name: "NaN defaults to opaque", opacity: math.NaN(), want: 100},
		{name: "below minimum clamps", opacity: 1, want: 10},
		{name: "just below minimum clamps", opacity: 9.9, want: 10},
		{name: "minimum kept", opacity: 10, want: 10},
		{name: "middle kept", opacity: 55.5, want: 55.5},
		{name: "maximum kept", opacity: 100, want: 100},
		{name: "above maximum clamps", opacity: 150, want: 100},
		{name: "infinity clamps", opacity: math.Inf(1), want: 100},
	}
	for i, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := openTestStore(t)
			stored := mustUpsert(t, s, State{ID: "pin-" + string(rune('a'+i)), Opacity: tt.opacity})
			if stored.Opacity != tt.want {
				t.Fatalf("Opacity = %v, want %v", stored.Opacity, tt.want)
			}
		})
	}
}

func TestUpsertSizeNormalization(t *testing.T) {
	tests := []struct {
		name       string
		width      int
		height     int
		wantWidth  int
		wantHeight int
	}{
		{name: "zero becomes minimum", width: 0, height: 0, wantWidth: 32, wantHeight: 32},
		{name: "negative becomes minimum", width: -100, height: -1, wantWidth: 32, wantHeight: 32},
		{name: "below minimum clamps", width: 31, height: 20, wantWidth: 32, wantHeight: 32},
		{name: "minimum kept", width: 32, height: 32, wantWidth: 32, wantHeight: 32},
		{name: "large kept", width: 640, height: 480, wantWidth: 640, wantHeight: 480},
	}
	for i, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := openTestStore(t)
			stored := mustUpsert(t, s, State{ID: "pin-s" + string(rune('a'+i)), Width: tt.width, Height: tt.height})
			if stored.Width != tt.wantWidth || stored.Height != tt.wantHeight {
				t.Fatalf("size = %dx%d, want %dx%d", stored.Width, stored.Height, tt.wantWidth, tt.wantHeight)
			}
		})
	}
}

func TestUpsertUpdatePreservesIdentity(t *testing.T) {
	s := openTestStore(t)
	createdAt := time.Date(2026, 11, 9, 21, 30, 0, 0, time.UTC)
	first := mustUpsert(t, s, State{
		ID:   "pin-1",
		File: "pin-1.png",
		X:    1, Y: 2,
		Width: 100, Height: 80,
		Opacity:   60,
		CreatedAt: createdAt,
	})

	updated := mustUpsert(t, s, State{
		ID: "pin-1",
		X:  10, Y: 20,
		Width: 200, Height: 150,
		Opacity: 70,
	})
	if updated.File != first.File {
		t.Fatalf("File = %q, want preserved %q", updated.File, first.File)
	}
	if !updated.CreatedAt.Equal(createdAt) {
		t.Fatalf("CreatedAt = %v, want preserved %v", updated.CreatedAt, createdAt)
	}
	if updated.X != 10 || updated.Y != 20 || updated.Width != 200 || updated.Height != 150 || updated.Opacity != 70 {
		t.Fatalf("updated state = %+v", updated)
	}
	if states := s.List(); len(states) != 1 || !sameState(states[0], updated) {
		t.Fatalf("List() = %+v, want [%+v]", states, updated)
	}
}

func TestListOrderingAndCopy(t *testing.T) {
	s := openTestStore(t)
	base := time.Date(2026, 11, 9, 21, 0, 0, 0, time.UTC)
	mustUpsert(t, s, State{ID: "pin-c", CreatedAt: base.Add(2 * time.Minute)})
	mustUpsert(t, s, State{ID: "pin-a", CreatedAt: base})
	mustUpsert(t, s, State{ID: "pin-b", CreatedAt: base}) // same time as pin-a: ID tie-break
	mustUpsert(t, s, State{ID: "pin-d", CreatedAt: base.Add(10 * time.Minute)})

	want := []string{"pin-a", "pin-b", "pin-c", "pin-d"}
	if got := listedIDs(s); !slices.Equal(got, want) {
		t.Fatalf("List() order = %v, want %v", got, want)
	}

	// List must hand out a copy; mutating it cannot corrupt the store.
	states := s.List()
	states[0].X = 9999
	states[0].ID = "mutated"
	if got := listedIDs(s); !slices.Equal(got, want) {
		t.Fatalf("List() after mutating the copy = %v, want %v", got, want)
	}
}

func TestReopenPersistsStates(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "pins")
	s, err := Open(dir)
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	createdAt := time.Date(2026, 11, 9, 21, 30, 0, 0, time.UTC)
	fileOne := mustSaveImage(t, s, "pin-1", 120, 80)
	mustUpsert(t, s, State{
		ID: "pin-1", File: fileOne, X: -100, Y: 50,
		Width: 120, Height: 80, Opacity: 80, CreatedAt: createdAt,
	})
	fileTwo := mustSaveImage(t, s, "pin-2", 64, 64)
	mustUpsert(t, s, State{
		ID: "pin-2", File: fileTwo, X: 300, Y: 200,
		Width: 64, Height: 64, Opacity: 100, CreatedAt: createdAt.Add(time.Minute),
	})

	before := s.List()
	reopened, err := Open(dir)
	if err != nil {
		t.Fatalf("re-Open() error = %v", err)
	}
	if after := reopened.List(); !sameStates(after, before) {
		t.Fatalf("reopened List() = %+v, want %+v", after, before)
	}

	// Update one state, reopen again and verify the update survived.
	mustUpsert(t, reopened, State{ID: "pin-1", X: 42, Y: 43, Width: 200, Height: 100, Opacity: 50})
	final, err := Open(dir)
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	states := final.List()
	if len(states) != 2 {
		t.Fatalf("List() = %+v, want 2 states", states)
	}
	if states[0].ID != "pin-1" || states[0].X != 42 || states[0].Opacity != 50 {
		t.Fatalf("updated state = %+v", states[0])
	}

	// pins.json must keep the documented shape.
	index := readPinsIndex(t, dir)
	if index.Version != indexVersion || len(index.Pins) != 2 {
		t.Fatalf("index = %+v, want version %d with 2 pins", index, indexVersion)
	}
	var raw struct {
		Pins []map[string]json.RawMessage `json:"pins"`
	}
	data, err := os.ReadFile(filepath.Join(dir, indexFileName))
	if err != nil {
		t.Fatalf("os.ReadFile(pins.json) error = %v", err)
	}
	if err := json.Unmarshal(data, &raw); err != nil {
		t.Fatalf("json.Unmarshal(pins.json) error = %v", err)
	}
	for _, key := range []string{"id", "file", "x", "y", "width", "height", "opacity", "createdAt"} {
		if _, ok := raw.Pins[0][key]; !ok {
			t.Fatalf("pins.json entry missing %q: %s", key, raw.Pins[0])
		}
	}
}

func TestCorruptIndexIsTolerated(t *testing.T) {
	corruptions := []struct {
		name    string
		content string
	}{
		{name: "garbage", content: "{ this is not json"},
		{name: "wrong top-level shape", content: "[]"},
		{name: "pins wrong type", content: `{"version":1,"pins":"nope"}`},
	}
	for _, tt := range corruptions {
		t.Run(tt.name, func(t *testing.T) {
			dir := filepath.Join(t.TempDir(), "pins")
			s, err := Open(dir)
			if err != nil {
				t.Fatalf("Open() error = %v", err)
			}
			mustSaveImage(t, s, "pin-1", 64, 64)
			mustUpsert(t, s, State{ID: "pin-1", Width: 64, Height: 64, Opacity: 100})
			writePinsJSON(t, dir, tt.content)

			reopened, err := Open(dir)
			if err != nil {
				t.Fatalf("Open() with corrupt index error = %v", err)
			}
			if states := reopened.List(); len(states) != 0 {
				t.Fatalf("List() = %+v, want empty after corrupt index", states)
			}
			// Corrupt state must never delete the user's images.
			if !fileExists(t, filepath.Join(dir, "pin-1.png")) {
				t.Fatal("image file was deleted while tolerating a corrupt index")
			}
			// The index is replaced with a valid empty one.
			index := readPinsIndex(t, dir)
			if index.Version != indexVersion || len(index.Pins) != 0 {
				t.Fatalf("rebuilt index = %+v, want empty version %d", index, indexVersion)
			}
			// The store stays usable.
			mustUpsert(t, reopened, State{ID: "pin-1", Width: 64, Height: 64, Opacity: 100})
			if ids := listedIDs(reopened); !slices.Equal(ids, []string{"pin-1"}) {
				t.Fatalf("List() after corruption recovery = %v, want [pin-1]", ids)
			}
		})
	}
}

func TestNewerIndexVersionIsRejected(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "pins")
	if _, err := Open(dir); err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	writePinsJSON(t, dir, `{"version":99,"pins":[]}`)
	if _, err := Open(dir); err == nil {
		t.Fatal("Open() with a newer index version returned nil error")
	}
}

func TestMissingImageDropsStateOnOpen(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "pins")
	s, err := Open(dir)
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	mustSaveImage(t, s, "pin-1", 64, 64)
	mustUpsert(t, s, State{ID: "pin-1", Width: 64, Height: 64})
	mustSaveImage(t, s, "pin-2", 32, 32)
	mustUpsert(t, s, State{ID: "pin-2", Width: 32, Height: 32})

	// Simulate an image lost outside the app.
	if err := os.Remove(filepath.Join(dir, "pin-2.png")); err != nil {
		t.Fatalf("os.Remove(pin-2.png) error = %v", err)
	}

	reopened, err := Open(dir)
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	if ids := listedIDs(reopened); !slices.Equal(ids, []string{"pin-1"}) {
		t.Fatalf("List() = %v, want only [pin-1]", ids)
	}
	if !fileExists(t, filepath.Join(dir, "pin-1.png")) {
		t.Fatal("valid pin image was removed during cleanup")
	}
	if index := readPinsIndex(t, dir); len(index.Pins) != 1 {
		t.Fatalf("index = %+v, want one pin after dropping the missing image", index.Pins)
	}
}

func TestInvalidIndexEntriesDropped(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "pins")
	s, err := Open(dir)
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	// Real image files referenced by the crafted index.
	if _, err := s.SaveImage("pin-1", testPNG(t, 64, 64)); err != nil {
		t.Fatalf("SaveImage(pin-1) error = %v", err)
	}
	if _, err := s.SaveImage("pin-4", testPNG(t, 48, 48)); err != nil {
		t.Fatalf("SaveImage(pin-4) error = %v", err)
	}
	// A canary outside the store directory that unsafe entries must not touch.
	canary := filepath.Join(filepath.Dir(dir), "canary.png")
	if err := os.WriteFile(canary, []byte("canary"), 0o644); err != nil {
		t.Fatalf("os.WriteFile(canary) error = %v", err)
	}
	base := time.Date(2026, 11, 9, 21, 0, 0, 0, time.UTC)
	writePinsIndex(t, dir, indexFile{Version: indexVersion, Pins: []State{
		{ID: "pin-1", File: "pin-1.png", X: 5, Y: 6, Width: 64, Height: 64, Opacity: 80, CreatedAt: base},
		{ID: "../../canary", File: "pin-1.png", Width: 64, Height: 64, Opacity: 80, CreatedAt: base},
		{ID: "pin-2", File: "../../canary.png", Width: 64, Height: 64, Opacity: 80, CreatedAt: base},
		{ID: "pin-1", File: "pin-1.png", X: 999, Width: 64, Height: 64, Opacity: 80, CreatedAt: base},
		{ID: "pin-3", File: "pin-3.png", Width: 64, Height: 64, Opacity: 80, CreatedAt: base},
		{ID: "pin-4", File: "pin-4.png", Width: 10, Height: 10, Opacity: 5, CreatedAt: base.Add(time.Minute)},
	}})

	reopened, err := Open(dir)
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	states := reopened.List()
	if ids := listedIDs(reopened); !slices.Equal(ids, []string{"pin-1", "pin-4"}) {
		t.Fatalf("List() = %v, want [pin-1 pin-4]", ids)
	}
	// The first valid entry wins over its duplicate.
	if states[0].X != 5 {
		t.Fatalf("pin-1 X = %d, want 5 (first valid entry)", states[0].X)
	}
	// Loaded values are normalized.
	if states[1].Width != minPinSize || states[1].Height != minPinSize || states[1].Opacity != minOpacity {
		t.Fatalf("pin-4 was not normalized: %+v", states[1])
	}
	if _, err := os.Stat(canary); err != nil {
		t.Fatalf("canary file outside the store was touched: %v", err)
	}
	if index := readPinsIndex(t, dir); len(index.Pins) != 2 {
		t.Fatalf("index = %+v, want the cleaned 2 pins", index.Pins)
	}
}

func TestRemove(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "pins")
	s, err := Open(dir)
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	fileOne := mustSaveImage(t, s, "pin-1", 64, 64)
	mustUpsert(t, s, State{ID: "pin-1", File: fileOne, Width: 64, Height: 64})
	fileTwo := mustSaveImage(t, s, "pin-2", 32, 32)
	mustUpsert(t, s, State{ID: "pin-2", File: fileTwo, Width: 32, Height: 32})

	if err := s.Remove("pin-1"); err != nil {
		t.Fatalf("Remove(pin-1) error = %v", err)
	}
	if ids := listedIDs(s); !slices.Equal(ids, []string{"pin-2"}) {
		t.Fatalf("List() = %v, want [pin-2]", ids)
	}
	if fileExists(t, filepath.Join(dir, "pin-1.png")) {
		t.Fatal("pin-1.png still exists after Remove")
	}
	// Idempotent for the removed and unknown IDs, and rejects unsafe IDs
	// without touching anything outside the store.
	for _, id := range []string{"pin-1", "unknown", "../../canary", ""} {
		if err := s.Remove(id); err != nil {
			t.Fatalf("Remove(%q) error = %v, want nil", id, err)
		}
	}
	reopened, err := Open(dir)
	if err != nil {
		t.Fatalf("Open() after Remove error = %v", err)
	}
	if ids := listedIDs(reopened); !slices.Equal(ids, []string{"pin-2"}) {
		t.Fatalf("reopened List() = %v, want [pin-2]", ids)
	}
}

func TestClear(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "pins")
	s, err := Open(dir)
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	if err := s.Clear(); err != nil {
		t.Fatalf("Clear() on empty store error = %v", err)
	}
	fileOne := mustSaveImage(t, s, "pin-1", 64, 64)
	mustUpsert(t, s, State{ID: "pin-1", File: fileOne, Width: 64, Height: 64})
	fileTwo := mustSaveImage(t, s, "pin-2", 32, 32)
	mustUpsert(t, s, State{ID: "pin-2", File: fileTwo, Width: 32, Height: 32})
	// Orphan image (e.g. left behind by a crash) and a stale temp file.
	if err := os.WriteFile(filepath.Join(dir, "orphan.png"), testPNG(t, 8, 8), 0o644); err != nil {
		t.Fatalf("os.WriteFile(orphan.png) error = %v", err)
	}
	for _, staleName := range []string{"." + indexFileName + ".tmp-stale", ".pin-1.png.tmp-stale"} {
		if err := os.WriteFile(filepath.Join(dir, staleName), []byte("x"), 0o644); err != nil {
			t.Fatalf("os.WriteFile(stale temp %q) error = %v", staleName, err)
		}
	}

	if err := s.Clear(); err != nil {
		t.Fatalf("Clear() error = %v", err)
	}
	if states := s.List(); len(states) != 0 {
		t.Fatalf("List() after Clear = %+v, want empty", states)
	}
	dirEntries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("os.ReadDir() error = %v", err)
	}
	for _, entry := range dirEntries {
		if entry.Name() != indexFileName {
			t.Fatalf("unexpected file %q left after Clear", entry.Name())
		}
	}
	if info, err := os.Stat(dir); err != nil || !info.IsDir() {
		t.Fatalf("store directory was removed: %v", err)
	}
	if index := readPinsIndex(t, dir); len(index.Pins) != 0 {
		t.Fatalf("index after Clear = %+v, want empty", index.Pins)
	}
	reopened, err := Open(dir)
	if err != nil {
		t.Fatalf("Open() after Clear error = %v", err)
	}
	if len(reopened.List()) != 0 {
		t.Fatalf("reopened List() = %+v, want empty", reopened.List())
	}
}

func TestConcurrentAccess(t *testing.T) {
	s := openTestStore(t)
	const workers = 8
	errs := make(chan error, workers*16)
	var wg sync.WaitGroup

	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			id := "pin-" + string(rune('a'+i))
			pngBytes := testPNG(t, 32+i, 32)
			file, err := s.SaveImage(id, pngBytes)
			if err != nil {
				errs <- err
				return
			}
			if _, err := s.Upsert(State{ID: id, File: file, X: i, Y: i, Width: 32 + i, Height: 32, Opacity: 100}); err != nil {
				errs <- err
				return
			}
			if _, err := s.ReadImage(State{ID: id, File: file}); err != nil {
				errs <- err
			}
			_ = s.List()
		}(i)
	}
	wg.Wait()

	// Concurrent updates of the same ID serialize into exactly one state.
	var updated atomic.Int64
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			if _, err := s.Upsert(State{ID: "pin-a", X: i, Width: 100, Height: 80, Opacity: 100}); err != nil {
				errs <- err
				return
			}
			updated.Add(1)
			_ = s.List()
		}(i)
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		t.Fatalf("concurrent access error = %v", err)
	}
	if got := updated.Load(); got != workers {
		t.Fatalf("concurrent upserts = %d, want %d", got, workers)
	}
	if states := s.List(); len(states) != workers {
		t.Fatalf("List() has %d states, want %d", len(states), workers)
	}

	reopened, err := Open(s.Dir())
	if err != nil {
		t.Fatalf("Open() after concurrent access error = %v", err)
	}
	if len(reopened.List()) != workers {
		t.Fatalf("reopened List() has %d states, want %d", len(reopened.List()), workers)
	}
}

func TestNoTemporaryFilesLeftBehind(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "pins")
	s, err := Open(dir)
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	file := mustSaveImage(t, s, "pin-1", 64, 64)
	mustUpsert(t, s, State{ID: "pin-1", File: file, Width: 64, Height: 64})
	if _, err := s.Upsert(State{ID: "pin-1", Width: 100, Height: 80}); err != nil {
		t.Fatalf("Upsert() error = %v", err)
	}
	if err := s.Remove("pin-1"); err != nil {
		t.Fatalf("Remove() error = %v", err)
	}

	dirEntries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("os.ReadDir() error = %v", err)
	}
	for _, entry := range dirEntries {
		if entry.Name() != indexFileName {
			t.Fatalf("unexpected file %q in store directory", entry.Name())
		}
	}
}

func TestJPEGIsNotAcceptedAsPinImage(t *testing.T) {
	s := openTestStore(t)
	var buffer bytes.Buffer
	img := image.NewRGBA(image.Rect(0, 0, 8, 8))
	for y := 0; y < 8; y++ {
		for x := 0; x < 8; x++ {
			img.SetRGBA(x, y, color.RGBA{R: uint8(x), G: uint8(y), B: 0, A: 255})
		}
	}
	if err := jpeg.Encode(&buffer, img, nil); err != nil {
		t.Fatalf("jpeg.Encode() error = %v", err)
	}
	if _, err := s.SaveImage("pin-1", buffer.Bytes()); !errors.Is(err, ErrInvalidImage) {
		t.Fatalf("SaveImage(JPEG) error = %v, want ErrInvalidImage", err)
	}
	if _, err := s.SaveImage("pin-1", []byte("not a png")); !errors.Is(err, ErrInvalidImage) {
		t.Fatalf("SaveImage(junk) error = %v, want ErrInvalidImage", err)
	}
	if _, err := s.SaveImage("pin-1", []byte("\x89PNG\r\n\x1a\n")); !errors.Is(err, ErrInvalidImage) {
		t.Fatalf("SaveImage(PNG signature only) error = %v, want ErrInvalidImage", err)
	}
	if _, err := s.SaveImage("pin-1", nil); !errors.Is(err, ErrInvalidImage) {
		t.Fatalf("SaveImage(nil) error = %v, want ErrInvalidImage", err)
	}
}
