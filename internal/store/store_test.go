package store

import (
	"bytes"
	"encoding/json"
	"errors"
	"image"
	"image/color"
	"image/png"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/echoVic/Pinstill/internal/diskio"
)

// testPNG builds a deterministic, non-uniform PNG of the requested size.
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

// testStore opens a store inside a fresh temporary directory.
func testStore(t *testing.T) *Store {
	t.Helper()
	s, err := Open(filepath.Join(t.TempDir(), "captures"))
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	return s
}

func mustSave(t *testing.T, s *Store, data []byte, createdAt time.Time) Entry {
	t.Helper()
	entry, err := s.Save(data, createdAt)
	if err != nil {
		t.Fatalf("Save() error = %v", err)
	}
	return entry
}

// listedIDs extracts the IDs from List in order.
func listedIDs(s *Store) []string {
	entries := s.List()
	ids := make([]string, len(entries))
	for i, entry := range entries {
		ids[i] = entry.ID
	}
	return ids
}

func readIndexFile(t *testing.T, dir string) indexFile {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(dir, indexFileName))
	if err != nil {
		t.Fatalf("os.ReadFile(index.json) error = %v", err)
	}
	var index indexFile
	if err := json.Unmarshal(data, &index); err != nil {
		t.Fatalf("index.json is not valid JSON: %v", err)
	}
	return index
}

func TestOpenCreatesDirectoryAndIndex(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "nested", "captures")
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
	if entries := s.List(); len(entries) != 0 {
		t.Fatalf("List() = %v, want empty", entries)
	}
	index := readIndexFile(t, dir)
	if index.Version != indexVersion || len(index.Entries) != 0 {
		t.Fatalf("index = %+v, want version %d with no entries", index, indexVersion)
	}

	reopened, err := Open(dir)
	if err != nil {
		t.Fatalf("re-Open() error = %v", err)
	}
	if len(reopened.List()) != 0 {
		t.Fatalf("List() after reopen = %v, want empty", reopened.List())
	}

	if _, err := Open(""); err == nil {
		t.Fatal("Open(\"\") returned nil error")
	}
}

func TestOpenKeepsUnrelatedFilesButIgnoresThem(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "captures")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("os.MkdirAll() error = %v", err)
	}
	notes := filepath.Join(dir, "notes.txt")
	if err := os.WriteFile(notes, []byte("keep me"), 0o644); err != nil {
		t.Fatalf("os.WriteFile() error = %v", err)
	}
	// A PNG whose name does not follow the ID contract must not be adopted.
	if err := os.WriteFile(filepath.Join(dir, "not-an-id.png"), testPNG(t, 8, 8), 0o644); err != nil {
		t.Fatalf("os.WriteFile() error = %v", err)
	}

	s, err := Open(dir)
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	if entries := s.List(); len(entries) != 0 {
		t.Fatalf("List() = %v, want no recovered entries", entries)
	}
	if _, err := os.Stat(notes); err != nil {
		t.Fatalf("unrelated file was removed: %v", err)
	}
}

func TestSaveListGetReadPNG(t *testing.T) {
	s := testStore(t)
	data := testPNG(t, 800, 600)
	createdAt := time.Date(2026, 11, 9, 10, 30, 0, 0, time.UTC)

	entry := mustSave(t, s, data, createdAt)
	if !validID(entry.ID) {
		t.Fatalf("Save() ID = %q, want format 20060102-150405-[a-z0-9]{4}", entry.ID)
	}
	if entry.File != entry.ID+pngExt {
		t.Fatalf("File = %q, want %q", entry.File, entry.ID+pngExt)
	}
	if entry.Thumb != entry.ID+thumbExt {
		t.Fatalf("Thumb = %q, want %q", entry.Thumb, entry.ID+thumbExt)
	}
	if entry.Width != 800 || entry.Height != 600 {
		t.Fatalf("size = %dx%d, want 800x600", entry.Width, entry.Height)
	}
	if entry.Size != int64(len(data)) {
		t.Fatalf("Size = %d, want %d", entry.Size, len(data))
	}
	if !entry.CreatedAt.Equal(createdAt) {
		t.Fatalf("CreatedAt = %v, want %v", entry.CreatedAt, createdAt)
	}

	if _, err := os.Stat(filepath.Join(s.Dir(), entry.File)); err != nil {
		t.Fatalf("screenshot file missing: %v", err)
	}
	if _, err := os.Stat(filepath.Join(s.Dir(), entry.Thumb)); err != nil {
		t.Fatalf("thumbnail file missing: %v", err)
	}
	index := readIndexFile(t, s.Dir())
	if len(index.Entries) != 1 || index.Entries[0].ID != entry.ID {
		t.Fatalf("index entries = %+v, want the saved entry", index.Entries)
	}

	got, ok := s.Get(entry.ID)
	if !ok {
		t.Fatalf("Get(%q) = not found", entry.ID)
	}
	if got.ID != entry.ID || got.Width != 800 || got.Height != 600 || got.Size != entry.Size ||
		got.File != entry.File || got.Thumb != entry.Thumb || !got.CreatedAt.Equal(createdAt) {
		t.Fatalf("Get() = %+v, want %+v", got, entry)
	}
	if _, ok := s.Get("20990101-000000-aaaa"); ok {
		t.Fatal("Get(unknown) = found")
	}
	if _, ok := s.Get("../../etc/passwd"); ok {
		t.Fatal("Get(path traversal) = found")
	}

	pngData, err := s.ReadPNG(entry.ID)
	if err != nil {
		t.Fatalf("ReadPNG() error = %v", err)
	}
	if !bytes.Equal(pngData, data) {
		t.Fatal("ReadPNG() bytes differ from the saved PNG")
	}
	if _, err := s.ReadPNG("20990101-000000-aaaa"); err == nil {
		t.Fatal("ReadPNG(unknown) returned nil error")
	}

	list := s.List()
	if len(list) != 1 || list[0].ID != entry.ID {
		t.Fatalf("List() = %+v, want one entry %q", list, entry.ID)
	}
}

func TestListNewestFirst(t *testing.T) {
	s := testStore(t)
	base := time.Date(2026, 11, 9, 12, 0, 0, 0, time.UTC)
	middle := mustSave(t, s, testPNG(t, 32, 24), base.Add(2*time.Minute))
	oldest := mustSave(t, s, testPNG(t, 32, 24), base)
	newest := mustSave(t, s, testPNG(t, 32, 24), base.Add(4*time.Minute))

	got := listedIDs(s)
	want := []string{newest.ID, middle.ID, oldest.ID}
	if len(got) != len(want) {
		t.Fatalf("List() IDs = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("List() IDs = %v, want %v", got, want)
		}
	}
}

func TestSaveRejectsInvalidInput(t *testing.T) {
	s := testStore(t)
	if _, err := s.Save(nil, time.Now()); err == nil {
		t.Fatal("Save(nil) returned nil error")
	}
	if _, err := s.Save([]byte("definitely not a png"), time.Now()); err == nil {
		t.Fatal("Save(garbage) returned nil error")
	}
	if entries := s.List(); len(entries) != 0 {
		t.Fatalf("List() = %v, want empty after failed saves", entries)
	}
	assertOnlyIndexFile(t, s.Dir())
}

func TestSaveZeroCreatedAtUsesNow(t *testing.T) {
	s := testStore(t)
	before := time.Now().Add(-time.Second)
	entry := mustSave(t, s, testPNG(t, 16, 16), time.Time{})
	after := time.Now().Add(time.Second)
	if entry.CreatedAt.IsZero() {
		t.Fatal("Save(zero time) left CreatedAt zero")
	}
	if entry.CreatedAt.Before(before) || entry.CreatedAt.After(after) {
		t.Fatalf("CreatedAt = %v, want close to now", entry.CreatedAt)
	}
	if prefix := entry.CreatedAt.UTC().Format("20060102-150405"); len(entry.ID) < len(prefix) || entry.ID[:len(prefix)] != prefix {
		t.Fatalf("ID = %q does not start with %q", entry.ID, prefix)
	}
}

func TestIDsAreUniqueWithinOneSecond(t *testing.T) {
	s := testStore(t)
	createdAt := time.Date(2026, 11, 9, 12, 0, 0, 0, time.UTC)
	seen := make(map[string]struct{})
	for i := 0; i < 30; i++ {
		entry := mustSave(t, s, testPNG(t, 8, 8), createdAt)
		if !validID(entry.ID) {
			t.Fatalf("ID = %q does not match the contract", entry.ID)
		}
		if _, duplicate := seen[entry.ID]; duplicate {
			t.Fatalf("duplicate ID %q", entry.ID)
		}
		seen[entry.ID] = struct{}{}
	}
	if len(seen) != 30 {
		t.Fatalf("IDs = %d, want 30", len(seen))
	}
}

func TestReopenPreservesIndex(t *testing.T) {
	s := testStore(t)
	base := time.Date(2026, 11, 9, 12, 0, 0, 0, time.UTC)
	first := mustSave(t, s, testPNG(t, 64, 48), base)
	second := mustSave(t, s, testPNG(t, 96, 72), base.Add(time.Minute))

	reopened, err := Open(s.Dir())
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	got := reopened.List()
	if len(got) != 2 {
		t.Fatalf("List() = %+v, want 2 entries", got)
	}
	if got[0].ID != second.ID || got[1].ID != first.ID {
		t.Fatalf("List() order = %v, want %v then %v", listedIDs(reopened), second.ID, first.ID)
	}
	for i, want := range []Entry{second, first} {
		if !got[i].CreatedAt.Equal(want.CreatedAt) || got[i].Width != want.Width ||
			got[i].Height != want.Height || got[i].Size != want.Size ||
			got[i].File != want.File || got[i].Thumb != want.Thumb {
			t.Fatalf("reopened entry %d = %+v, want %+v", i, got[i], want)
		}
	}
	if _, err := reopened.ReadPNG(first.ID); err != nil {
		t.Fatalf("ReadPNG() after reopen error = %v", err)
	}

	// New stores keep writing to the same directory.
	third := mustSave(t, reopened, testPNG(t, 32, 32), base.Add(2*time.Minute))
	final, err := Open(s.Dir())
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	if ids := listedIDs(final); len(ids) != 3 || ids[0] != third.ID {
		t.Fatalf("List() after third save = %v, want %q first", ids, third.ID)
	}
}

func TestCorruptIndexIsRebuiltFromPNGFiles(t *testing.T) {
	s := testStore(t)
	base := time.Date(2026, 11, 9, 12, 0, 0, 0, time.UTC)
	first := mustSave(t, s, testPNG(t, 40, 30), base)
	second := mustSave(t, s, testPNG(t, 50, 25), base.Add(time.Minute))

	indexPath := filepath.Join(s.Dir(), indexFileName)
	if err := os.WriteFile(indexPath, []byte("{ this is not json"), 0o644); err != nil {
		t.Fatalf("os.WriteFile() error = %v", err)
	}
	reopened, err := Open(s.Dir())
	if err != nil {
		t.Fatalf("Open() with corrupt index error = %v", err)
	}
	ids := listedIDs(reopened)
	if len(ids) != 2 || ids[0] != second.ID || ids[1] != first.ID {
		t.Fatalf("recovered IDs = %v, want %v then %v", ids, second.ID, first.ID)
	}
	if _, err := reopened.ReadPNG(first.ID); err != nil {
		t.Fatalf("ReadPNG() after recovery error = %v", err)
	}
	index := readIndexFile(t, s.Dir())
	if len(index.Entries) != 2 {
		t.Fatalf("rebuilt index has %d entries, want 2", len(index.Entries))
	}
}

func TestOpenDropsInvalidIndexEntries(t *testing.T) {
	s := testStore(t)
	base := time.Date(2026, 11, 9, 12, 0, 0, 0, time.UTC)
	good := mustSave(t, s, testPNG(t, 40, 30), base)

	// A canary file next to the store that unsafe entries must never touch.
	canary := filepath.Join(filepath.Dir(s.Dir()), "canary.png")
	if err := os.WriteFile(canary, []byte("canary"), 0o644); err != nil {
		t.Fatalf("os.WriteFile(canary) error = %v", err)
	}
	invalid := []Entry{
		{ID: "../../canary", CreatedAt: base, Width: 1, Height: 1, Size: 1, File: "../../canary.png"},
		{ID: "20260101-000000-aaaa", CreatedAt: base, Width: 1, Height: 1, Size: 1, File: "../../canary.png"},
		{ID: good.ID, CreatedAt: base, Width: 1, Height: 1, Size: 1, File: "other.png", Thumb: ""},
		good,
		good, // duplicate
	}
	payload, err := json.Marshal(indexFile{Version: indexVersion, Entries: invalid})
	if err != nil {
		t.Fatalf("json.Marshal() error = %v", err)
	}
	if err := os.WriteFile(filepath.Join(s.Dir(), indexFileName), payload, 0o644); err != nil {
		t.Fatalf("os.WriteFile(index) error = %v", err)
	}

	reopened, err := Open(s.Dir())
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	if ids := listedIDs(reopened); len(ids) != 1 || ids[0] != good.ID {
		t.Fatalf("List() IDs = %v, want only %q", ids, good.ID)
	}
	if _, err := reopened.ReadPNG("../../canary"); err == nil {
		t.Fatal("ReadPNG(path traversal) returned nil error")
	}
	if err := reopened.Delete("../../canary"); err != nil {
		t.Fatalf("Delete(path traversal) error = %v", err)
	}
	if _, err := os.Stat(canary); err != nil {
		t.Fatalf("canary file was touched: %v", err)
	}
	if index := readIndexFile(t, s.Dir()); len(index.Entries) != 1 {
		t.Fatalf("index was not rewritten with only valid entries: %+v", index.Entries)
	}
}

func TestDelete(t *testing.T) {
	s := testStore(t)
	base := time.Date(2026, 11, 9, 12, 0, 0, 0, time.UTC)
	first := mustSave(t, s, testPNG(t, 32, 32), base)
	second := mustSave(t, s, testPNG(t, 32, 32), base.Add(time.Minute))

	if err := s.Delete(first.ID); err != nil {
		t.Fatalf("Delete() error = %v", err)
	}
	if ids := listedIDs(s); len(ids) != 1 || ids[0] != second.ID {
		t.Fatalf("List() after Delete = %v, want only %q", ids, second.ID)
	}
	for _, name := range []string{first.File, first.Thumb} {
		if _, err := os.Stat(filepath.Join(s.Dir(), name)); !os.IsNotExist(err) {
			t.Fatalf("%q still exists after Delete (err = %v)", name, err)
		}
	}
	if _, ok := s.Get(first.ID); ok {
		t.Fatalf("Get(%q) still found after Delete", first.ID)
	}
	if _, err := s.ReadPNG(first.ID); err == nil {
		t.Fatal("ReadPNG() after Delete returned nil error")
	}
	if err := s.Delete(first.ID); err != nil {
		t.Fatalf("second Delete() error = %v, want nil", err)
	}
	if err := s.Delete("20990101-000000-aaaa"); err != nil {
		t.Fatalf("Delete(unknown) error = %v, want nil", err)
	}
	if index := readIndexFile(t, s.Dir()); len(index.Entries) != 1 || index.Entries[0].ID != second.ID {
		t.Fatalf("index after Delete = %+v, want only %q", index.Entries, second.ID)
	}
	if _, err := Open(s.Dir()); err != nil {
		t.Fatalf("Open() after Delete error = %v", err)
	}
}

func TestClear(t *testing.T) {
	s := testStore(t)
	base := time.Date(2026, 11, 9, 12, 0, 0, 0, time.UTC)
	mustSave(t, s, testPNG(t, 32, 32), base)
	mustSave(t, s, testPNG(t, 32, 32), base.Add(time.Minute))

	if err := s.Clear(); err != nil {
		t.Fatalf("Clear() error = %v", err)
	}
	if entries := s.List(); len(entries) != 0 {
		t.Fatalf("List() after Clear = %v, want empty", entries)
	}
	assertOnlyIndexFile(t, s.Dir())
	index := readIndexFile(t, s.Dir())
	if len(index.Entries) != 0 {
		t.Fatalf("index after Clear = %+v, want no entries", index.Entries)
	}

	// The store stays usable after Clear.
	entry := mustSave(t, s, testPNG(t, 24, 24), base.Add(2*time.Minute))
	if ids := listedIDs(s); len(ids) != 1 || ids[0] != entry.ID {
		t.Fatalf("List() after Clear + Save = %v, want %q", ids, entry.ID)
	}
}

func TestSavePublishesCommittedIndexBeforeDurabilityError(t *testing.T) {
	s := testStore(t)
	data := testPNG(t, 32, 32)
	s.indexWriter = func(path string, data []byte, mode os.FileMode) error {
		if err := diskio.WriteFileAtomic(path, data, mode); err != nil {
			return err
		}
		if filepath.Base(path) == indexFileName {
			return diskio.MarkCommitted(errors.New("directory sync failed"))
		}
		return nil
	}

	entry, err := s.Save(data, time.Now())
	if err == nil {
		t.Fatal("Save() returned nil error after committed durability failure")
	}
	if entry.ID == "" {
		t.Fatalf("Save() returned empty entry with committed error: %v", err)
	}
	if listed := s.List(); len(listed) != 1 || listed[0].ID != entry.ID {
		t.Fatalf("List() after committed Save = %v, want only %q", listed, entry.ID)
	}

	reopened, err := Open(s.Dir())
	if err != nil {
		t.Fatalf("Open() after committed Save: %v", err)
	}
	if listed := reopened.List(); len(listed) != 1 || listed[0].ID != entry.ID {
		t.Fatalf("reopened List() = %v, want only %q", listed, entry.ID)
	}
}

func TestReadPNGMissingFile(t *testing.T) {
	s := testStore(t)
	entry := mustSave(t, s, testPNG(t, 16, 16), time.Now())
	if err := os.Remove(filepath.Join(s.Dir(), entry.File)); err != nil {
		t.Fatalf("os.Remove() error = %v", err)
	}
	if _, err := s.ReadPNG(entry.ID); err == nil {
		t.Fatal("ReadPNG() with missing file returned nil error")
	}
}

func TestConcurrentSaves(t *testing.T) {
	s := testStore(t)
	const workers = 16
	var wg sync.WaitGroup
	ids := make(chan string, workers)
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			data := testPNG(t, 16+i, 16)
			entry, err := s.Save(data, time.Date(2026, 11, 9, 12, 0, i, 0, time.UTC))
			if err != nil {
				t.Errorf("Save() error = %v", err)
				return
			}
			ids <- entry.ID
		}(i)
	}
	wg.Wait()
	close(ids)

	if entries := s.List(); len(entries) != workers {
		t.Fatalf("List() has %d entries, want %d", len(entries), workers)
	}
	seen := make(map[string]struct{}, workers)
	for id := range ids {
		if _, duplicate := seen[id]; duplicate {
			t.Fatalf("duplicate ID %q", id)
		}
		seen[id] = struct{}{}
	}
	reopened, err := Open(s.Dir())
	if err != nil {
		t.Fatalf("Open() after concurrent saves error = %v", err)
	}
	if len(reopened.List()) != workers {
		t.Fatalf("reopened List() has %d entries, want %d", len(reopened.List()), workers)
	}
}

// assertOnlyIndexFile verifies no leftover screenshot or temporary files exist.
func assertOnlyIndexFile(t *testing.T, dir string) {
	t.Helper()
	dirEntries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("os.ReadDir() error = %v", err)
	}
	for _, entry := range dirEntries {
		if entry.Name() != indexFileName && entry.Name() != ".pinstill.lock" {
			t.Fatalf("unexpected file %q in store directory", entry.Name())
		}
	}
}

func TestClearRetriesFailedFileCleanup(t *testing.T) {
	s := testStore(t)
	entry := mustSave(t, s, testPNG(t, 32, 32), time.Now())
	blocked := filepath.Join(s.Dir(), entry.File)
	if err := os.Remove(blocked); err != nil {
		t.Fatalf("remove original PNG before blocker: %v", err)
	}
	if err := os.Mkdir(blocked, 0o755); err != nil {
		t.Fatalf("make blocking directory: %v", err)
	}
	if err := os.WriteFile(filepath.Join(blocked, "live"), []byte("keep until retry"), 0o600); err != nil {
		t.Fatalf("write blocker child: %v", err)
	}

	if err := s.Clear(); err == nil {
		t.Fatal("first Clear() returned nil despite blocked file")
	}
	if err := os.Remove(filepath.Join(blocked, "live")); err != nil {
		t.Fatalf("remove blocker child: %v", err)
	}
	if err := os.Remove(blocked); err != nil {
		t.Fatalf("remove blocker directory: %v", err)
	}

	reopened, err := Open(s.Dir())
	if err != nil {
		t.Fatalf("reopen before cleanup retry: %v", err)
	}
	if err := reopened.Clear(); err != nil {
		t.Fatalf("retry Clear() error = %v", err)
	}
	for _, name := range []string{entry.File, entry.Thumb} {
		if _, err := os.Stat(filepath.Join(s.Dir(), name)); !os.IsNotExist(err) {
			t.Fatalf("%q remains after cleanup retry (err = %v)", name, err)
		}
	}
}

func TestDeleteRetriesFailedFileCleanup(t *testing.T) {
	s := testStore(t)
	entry := mustSave(t, s, testPNG(t, 32, 32), time.Now())
	blocked := filepath.Join(s.Dir(), entry.File)
	if err := os.Remove(blocked); err != nil {
		t.Fatalf("remove original PNG before blocker: %v", err)
	}
	if err := os.Mkdir(blocked, 0o755); err != nil {
		t.Fatalf("make blocking directory: %v", err)
	}
	if err := os.WriteFile(filepath.Join(blocked, "live"), []byte("keep until retry"), 0o600); err != nil {
		t.Fatalf("write blocker child: %v", err)
	}

	if err := s.Delete(entry.ID); err == nil {
		t.Fatal("first Delete() returned nil despite blocked file")
	}
	if err := os.Remove(filepath.Join(blocked, "live")); err != nil {
		t.Fatalf("remove blocker child: %v", err)
	}
	if err := os.Remove(blocked); err != nil {
		t.Fatalf("remove blocker directory: %v", err)
	}

	reopened, err := Open(s.Dir())
	if err != nil {
		t.Fatalf("reopen before cleanup retry: %v", err)
	}
	if err := reopened.Delete(entry.ID); err != nil {
		t.Fatalf("retry Delete() error = %v", err)
	}
	for _, name := range []string{entry.File, entry.Thumb} {
		if _, err := os.Stat(filepath.Join(s.Dir(), name)); !os.IsNotExist(err) {
			t.Fatalf("%q remains after Delete retry (err = %v)", name, err)
		}
	}
}

func TestOpenRejectsOversizedIndexWithoutRecovery(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, indexFileName)
	data := []byte(`{"version":1,"entries":[],"padding":"` + strings.Repeat("x", 8<<20) + `"}`)
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := Open(dir); err == nil {
		t.Fatal("Open() accepted an oversized index")
	} else if errors.Is(err, errCorruptIndex) {
		t.Fatalf("oversized index was treated as recoverable corruption: %v", err)
	}
	if info, err := os.Stat(path); err != nil || info.Size() != int64(len(data)) {
		t.Fatalf("oversized index was altered: %v", err)
	}
}

func TestDeleteSanitizesRefreshedIndexBeforeRemovingFiles(t *testing.T) {
	s := testStore(t)
	entry := mustSave(t, s, testPNG(t, 32, 32), time.Now())
	outside := filepath.Join(filepath.Dir(s.Dir()), "outside-history.png")
	if err := os.WriteFile(outside, []byte("keep"), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Remove(outside) })
	entry.File = "../outside-history.png"
	data, err := json.Marshal(indexFile{Version: indexVersion, Entries: []Entry{entry}})
	if err != nil {
		t.Fatal(err)
	}
	writeFile(t, s.Dir(), indexFileName, data)

	if err := s.Delete(entry.ID); err != nil {
		t.Fatalf("Delete() error = %v", err)
	}
	if _, err := os.Stat(outside); err != nil {
		t.Fatalf("tampered index caused deletion outside store: %v", err)
	}
}
