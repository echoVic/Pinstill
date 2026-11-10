package pin

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"testing"
	"time"
)

// restoreCap mirrors app.maxRestoredPins: at launch the app restores at most
// the newest 60 persisted pins. Package pin cannot import internal/app (that
// would be an import cycle), so this test locks the List ordering contract
// that makes "the newest 60" possible.
const restoreCap = 60

// TestOpenToleratesDamagedIndexKeepsImages covers every damaged pins.json
// shape the store claims to tolerate: the state is dropped (index rebuilt
// empty) but the user's images are never deleted, and the store stays usable.
func TestOpenToleratesDamagedIndexKeepsImages(t *testing.T) {
	damages := []struct {
		name    string
		content string
	}{
		{name: "empty file", content: ""},
		{name: "whitespace only", content: " \n\t "},
		{name: "truncated json", content: `{"version":1,"pins":[{"id":"pin-1"`},
		{name: "garbage", content: "{ this is not json"},
		{name: "wrong top-level shape", content: "[]"},
		{name: "pins wrong type", content: `{"version":1,"pins":"nope"}`},
	}
	for _, damage := range damages {
		t.Run(damage.name, func(t *testing.T) {
			dir := filepath.Join(t.TempDir(), "pins")
			s, err := Open(dir)
			if err != nil {
				t.Fatalf("Open() error = %v", err)
			}
			pngBytes := testPNG(t, 48, 32)
			if _, err := s.SaveImage("pin-1", pngBytes); err != nil {
				t.Fatalf("SaveImage(pin-1) error = %v", err)
			}
			mustUpsert(t, s, State{ID: "pin-1", Width: 48, Height: 32, Opacity: 80})
			// An orphan image (not referenced by the index) must survive too.
			orphan := testPNG(t, 8, 8)
			if err := os.WriteFile(filepath.Join(dir, "orphan.png"), orphan, 0o644); err != nil {
				t.Fatalf("os.WriteFile(orphan.png) error = %v", err)
			}
			writePinsJSON(t, dir, damage.content)

			reopened, err := Open(dir)
			if err != nil {
				t.Fatalf("Open() with damaged index error = %v", err)
			}
			if states := reopened.List(); len(states) != 0 {
				t.Fatalf("List() = %+v, want empty after a damaged index", states)
			}
			// Damage tolerance must never delete user images.
			if data, err := os.ReadFile(filepath.Join(dir, "pin-1.png")); err != nil {
				t.Fatalf("image was deleted while tolerating a damaged index: %v", err)
			} else if !bytes.Equal(data, pngBytes) {
				t.Fatal("image bytes changed while tolerating a damaged index")
			}
			if !fileExists(t, filepath.Join(dir, "orphan.png")) {
				t.Fatal("orphan image was deleted while tolerating a damaged index")
			}
			// pins.json is replaced by a valid empty index, so the next start
			// takes the clean path instead of failing again.
			index := readPinsIndex(t, dir)
			if index.Version != indexVersion || len(index.Pins) != 0 {
				t.Fatalf("rebuilt index = %+v, want empty version %d", index, indexVersion)
			}
			again, err := Open(dir)
			if err != nil {
				t.Fatalf("second Open() error = %v", err)
			}
			if states := again.List(); len(states) != 0 {
				t.Fatalf("second List() = %+v, want empty", states)
			}
			if !fileExists(t, filepath.Join(dir, "pin-1.png")) {
				t.Fatal("image was deleted by the second Open()")
			}
			// The store is usable again and re-registers the preserved image.
			mustUpsert(t, again, State{ID: "pin-1", Width: 48, Height: 32, Opacity: 100})
			if ids := listedIDs(again); !slices.Equal(ids, []string{"pin-1"}) {
				t.Fatalf("List() after re-registering = %v, want [pin-1]", ids)
			}
			read, err := again.ReadImage(State{ID: "pin-1"})
			if err != nil {
				t.Fatalf("ReadImage() after re-registering error = %v", err)
			}
			if !bytes.Equal(read, pngBytes) {
				t.Fatal("re-registered pin does not use the preserved image")
			}
		})
	}
}

// TestOpenWithoutIndexStartsEmptyAndKeepsImages covers a manually deleted
// pins.json: the index is rebuilt empty, images stay on disk and a re-added
// pin reuses its original image.
func TestOpenWithoutIndexStartsEmptyAndKeepsImages(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "pins")
	s, err := Open(dir)
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	pngBytes := testPNG(t, 64, 64)
	if _, err := s.SaveImage("pin-1", pngBytes); err != nil {
		t.Fatalf("SaveImage(pin-1) error = %v", err)
	}
	mustUpsert(t, s, State{ID: "pin-1", Width: 64, Height: 64, Opacity: 100})
	if err := os.Remove(filepath.Join(dir, indexFileName)); err != nil {
		t.Fatalf("os.Remove(pins.json) error = %v", err)
	}

	reopened, err := Open(dir)
	if err != nil {
		t.Fatalf("Open() without an index error = %v", err)
	}
	if states := reopened.List(); len(states) != 0 {
		t.Fatalf("List() = %+v, want empty without an index", states)
	}
	if index := readPinsIndex(t, dir); index.Version != indexVersion || len(index.Pins) != 0 {
		t.Fatalf("rebuilt index = %+v, want empty version %d", index, indexVersion)
	}
	data, err := os.ReadFile(filepath.Join(dir, "pin-1.png"))
	if err != nil {
		t.Fatalf("image was deleted together with the index: %v", err)
	}
	if !bytes.Equal(data, pngBytes) {
		t.Fatal("image bytes changed when the index was rebuilt")
	}

	mustUpsert(t, reopened, State{ID: "pin-1", Width: 64, Height: 64, Opacity: 100})
	read, err := reopened.ReadImage(State{ID: "pin-1"})
	if err != nil {
		t.Fatalf("ReadImage() after re-registering error = %v", err)
	}
	if !bytes.Equal(read, pngBytes) {
		t.Fatal("re-registered pin does not use the preserved image")
	}
}

// TestOpenTreatsJSONNullIndexAsEmpty documents that a syntactically valid but
// empty index (the literal null) is tolerated as "no pins" rather than as a
// hard failure.
func TestOpenTreatsJSONNullIndexAsEmpty(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "pins")
	s, err := Open(dir)
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	if _, err := s.SaveImage("pin-1", testPNG(t, 16, 16)); err != nil {
		t.Fatalf("SaveImage(pin-1) error = %v", err)
	}
	mustUpsert(t, s, State{ID: "pin-1", Width: 16, Height: 16, Opacity: 100})
	writePinsJSON(t, dir, "null")

	reopened, err := Open(dir)
	if err != nil {
		t.Fatalf("Open() with a null index error = %v", err)
	}
	if states := reopened.List(); len(states) != 0 {
		t.Fatalf("List() = %+v, want empty", states)
	}
	if !fileExists(t, filepath.Join(dir, "pin-1.png")) {
		t.Fatal("image was deleted while tolerating a null index")
	}
	mustUpsert(t, reopened, State{ID: "pin-1", Width: 16, Height: 16, Opacity: 100})
	if ids := listedIDs(reopened); !slices.Equal(ids, []string{"pin-1"}) {
		t.Fatalf("List() after re-registering = %v, want [pin-1]", ids)
	}
}

// TestListOrderSupportsNewestRestoreCap verifies the ordering guarantee behind
// the 60-pin restore cap: List returns every persisted pin oldest first, so
// the app can keep the tail (the newest 60) when there are more.
func TestListOrderSupportsNewestRestoreCap(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "pins")
	s, err := Open(dir)
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	base := time.Date(2026, 11, 9, 12, 0, 0, 0, time.UTC)
	total := restoreCap + 5 // 65 pins: five must fall outside the restore window
	ids := make([]string, total)
	for i := 0; i < total; i++ {
		id := fmt.Sprintf("pin-%02d", i)
		ids[i] = id
		mustSaveImage(t, s, id, 8, 8)
		mustUpsert(t, s, State{
			ID:        id,
			Width:     8,
			Height:    8,
			Opacity:   100,
			CreatedAt: base.Add(time.Duration(i) * time.Minute),
		})
	}

	reopened, err := Open(dir)
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	states := reopened.List()
	if len(states) != total {
		t.Fatalf("List() has %d pins, want %d", len(states), total)
	}
	for i := 1; i < len(states); i++ {
		if !states[i-1].CreatedAt.Before(states[i].CreatedAt) {
			t.Fatalf("List() is not oldest-first at %d: %v then %v",
				i, states[i-1].CreatedAt, states[i].CreatedAt)
		}
	}

	// Replicate the restore window the app computes: keep the newest 60.
	start := 0
	if len(states) > restoreCap {
		start = len(states) - restoreCap
	}
	restored := states[start:]
	if len(restored) != restoreCap {
		t.Fatalf("restore window has %d pins, want %d", len(restored), restoreCap)
	}
	if restored[0].ID != ids[total-restoreCap] {
		t.Fatalf("oldest restored pin = %q, want %q", restored[0].ID, ids[total-restoreCap])
	}
	if restored[len(restored)-1].ID != ids[total-1] {
		t.Fatalf("newest restored pin = %q, want %q", restored[len(restored)-1].ID, ids[total-1])
	}
	for i := 0; i < total-restoreCap; i++ {
		if states[i].ID != ids[i] {
			t.Fatalf("dropped pin %d = %q, want the oldest %q", i, states[i].ID, ids[i])
		}
	}

	// The order is stable across another restart.
	again, err := Open(dir)
	if err != nil {
		t.Fatalf("second Open() error = %v", err)
	}
	if got := listedIDs(again); !slices.Equal(got, ids) {
		t.Fatalf("List() order changed after a restart: %v", got)
	}
}
