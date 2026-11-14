package store

import (
	"bytes"
	"image/jpeg"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

// recoveryBaseTime keeps recovered-entry assertions deterministic; the store
// takes CreatedAt from the file modification time during a rebuild, so tests
// never compare against this value directly.
var recoveryBaseTime = time.Date(2026, 11, 9, 9, 30, 0, 0, time.UTC)

// requireWritableDirectorySemantics skips tests that rely on a read-only
// directory denying writes: root ignores directory permissions and Windows
// does not model them the same way.
func requireWritableDirectorySemantics(t *testing.T) {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("directory permissions do not deny writes the same way on Windows")
	}
	if os.Geteuid() == 0 {
		t.Skip("root can write into a read-only directory")
	}
}

// makeDirectoryReadOnly makes dir unwritable and restores the permissions
// before t.TempDir's own cleanup runs (cleanups are LIFO).
func makeDirectoryReadOnly(t *testing.T, dir string) {
	t.Helper()
	if err := os.Chmod(dir, 0o555); err != nil {
		t.Fatalf("os.Chmod(%q, 0555) error = %v", dir, err)
	}
	t.Cleanup(func() { _ = os.Chmod(dir, 0o755) })
}

// assertValidThumbnail verifies that a rebuilt thumbnail exists and is a real
// JPEG whose longest side respects the frozen 320px thumbnail contract.
func assertValidThumbnail(t *testing.T, dir, thumb string) {
	t.Helper()
	if thumb == "" {
		t.Fatal("recovered entry has no thumbnail name")
	}
	path := filepath.Join(dir, thumb)
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("os.ReadFile(%q) error = %v", thumb, err)
	}
	if len(data) < 2 || data[0] != 0xFF || data[1] != 0xD8 {
		t.Fatalf("thumbnail %q does not start with a JPEG SOI marker", thumb)
	}
	config, err := jpeg.DecodeConfig(bytes.NewReader(data))
	if err != nil {
		t.Fatalf("jpeg.DecodeConfig(%q) error = %v", thumb, err)
	}
	if config.Width <= 0 || config.Height <= 0 {
		t.Fatalf("thumbnail %q has empty dimensions %dx%d", thumb, config.Width, config.Height)
	}
	if longest := max(config.Width, config.Height); longest > thumbnailMaxEdge {
		t.Fatalf("thumbnail %q is %dx%d, longest side exceeds %d", thumb, config.Width, config.Height, thumbnailMaxEdge)
	}
}

// writeFile writes raw bytes into dir, failing the test on error.
func writeFile(t *testing.T, dir, name string, data []byte) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, name), data, 0o644); err != nil {
		t.Fatalf("os.WriteFile(%q) error = %v", name, err)
	}
}

// TestOpenRecoversFromDamagedIndex exercises every corrupted-index shape the
// store claims to tolerate: the history must be rebuilt by scanning the PNG
// files, thumbnails must be repaired, and unrelated/broken files must be left
// alone.
func TestOpenRecoversFromDamagedIndex(t *testing.T) {
	type damage struct {
		name string
		run  func(t *testing.T, dir string)
	}
	damages := []damage{
		{name: "missing index", run: func(t *testing.T, dir string) {
			t.Helper()
			if err := os.Remove(filepath.Join(dir, indexFileName)); err != nil {
				t.Fatalf("os.Remove(index.json) error = %v", err)
			}
		}},
		{name: "empty index", run: func(t *testing.T, dir string) {
			t.Helper()
			writeFile(t, dir, indexFileName, nil)
		}},
		{name: "truncated json", run: func(t *testing.T, dir string) {
			t.Helper()
			writeFile(t, dir, indexFileName, []byte(`{"version":1,"entries":[{"id":"2026`))
		}},
		{name: "garbage", run: func(t *testing.T, dir string) {
			t.Helper()
			writeFile(t, dir, indexFileName, []byte("{ this is not json"))
		}},
		{name: "wrong top-level shape", run: func(t *testing.T, dir string) {
			t.Helper()
			writeFile(t, dir, indexFileName, []byte("[]"))
		}},
		{name: "entries wrong type", run: func(t *testing.T, dir string) {
			t.Helper()
			writeFile(t, dir, indexFileName, []byte(`{"version":1,"entries":"nope"}`))
		}},
	}

	for _, damage := range damages {
		t.Run(damage.name, func(t *testing.T) {
			dir := filepath.Join(t.TempDir(), "captures")
			s, err := Open(dir)
			if err != nil {
				t.Fatalf("Open() error = %v", err)
			}
			first := mustSave(t, s, testPNG(t, 640, 480), recoveryBaseTime)
			second := mustSave(t, s, testPNG(t, 96, 64), recoveryBaseTime.Add(time.Minute))

			// Damage the index and lose one thumbnail along the way; a
			// zero-byte thumbnail must be rebuilt from the PNG.
			damage.run(t, dir)
			if err := os.Remove(filepath.Join(dir, first.Thumb)); err != nil {
				t.Fatalf("os.Remove(first thumbnail) error = %v", err)
			}
			writeFile(t, dir, second.Thumb, nil)

			// A contract-ID PNG that is not an image must be skipped, an
			// orphaned valid PNG must be adopted, and unrelated files must be
			// left untouched.
			brokenID := "20260101-010203-brok"
			writeFile(t, dir, brokenID+pngExt, []byte("not a png at all"))
			orphanID := "20260101-020304-orph"
			writeFile(t, dir, orphanID+pngExt, testPNG(t, 48, 24))
			notes := "notes.txt"
			writeFile(t, dir, notes, []byte("keep me"))

			reopened, err := Open(dir)
			if err != nil {
				t.Fatalf("Open() with damaged index error = %v", err)
			}
			entries := reopened.List()
			byID := make(map[string]Entry, len(entries))
			for _, entry := range entries {
				byID[entry.ID] = entry
			}
			if len(entries) != 3 {
				t.Fatalf("recovered %d entries (%v), want 3", len(entries), listedIDs(reopened))
			}
			for _, id := range []string{first.ID, second.ID, orphanID} {
				if _, ok := byID[id]; !ok {
					t.Fatalf("recovery lost %q; recovered = %v", id, listedIDs(reopened))
				}
			}
			if _, ok := byID[brokenID]; ok {
				t.Fatalf("recovery adopted the invalid PNG %q", brokenID)
			}
			if got := byID[first.ID]; got.Width != 640 || got.Height != 480 || got.Size <= 0 {
				t.Fatalf("recovered first = %+v, want 640x480 with a size", got)
			}
			if got := byID[second.ID]; got.Width != 96 || got.Height != 64 || got.Size <= 0 {
				t.Fatalf("recovered second = %+v, want 96x64 with a size", got)
			}
			for _, entry := range entries {
				if entry.CreatedAt.IsZero() {
					t.Fatalf("recovered entry %q has a zero CreatedAt", entry.ID)
				}
				assertValidThumbnail(t, dir, entry.Thumb)
				if _, err := reopened.ReadPNG(entry.ID); err != nil {
					t.Fatalf("ReadPNG(%q) after recovery error = %v", entry.ID, err)
				}
			}
			if _, err := os.Stat(filepath.Join(dir, notes)); err != nil {
				t.Fatalf("unrelated file was removed during recovery: %v", err)
			}
			index := readIndexFile(t, dir)
			if index.Version != indexVersion || len(index.Entries) != 3 {
				t.Fatalf("rebuilt index = %+v, want version %d with 3 entries", index, indexVersion)
			}
			// The rebuilt store must keep working.
			mustSave(t, reopened, testPNG(t, 16, 16), time.Now())
			if got := len(reopened.List()); got != 4 {
				t.Fatalf("List() after recovery + save has %d entries, want 4", got)
			}
		})
	}
}

// TestOpenRebuildsEmptyIndexWhenNoPNGsExist verifies a damaged index in an
// otherwise empty directory is replaced by a valid empty one.
func TestOpenRebuildsEmptyIndexWhenNoPNGsExist(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "captures")
	if _, err := Open(dir); err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	writeFile(t, dir, indexFileName, []byte("{broken"))
	reopened, err := Open(dir)
	if err != nil {
		t.Fatalf("Open() with corrupt index error = %v", err)
	}
	if entries := reopened.List(); len(entries) != 0 {
		t.Fatalf("List() = %+v, want empty", entries)
	}
	index := readIndexFile(t, dir)
	if index.Version != indexVersion || len(index.Entries) != 0 {
		t.Fatalf("rebuilt index = %+v, want empty version %d", index, indexVersion)
	}
}

// TestOpenRejectsNewerIndexVersion documents the frozen downgrade behavior: an
// index written by a future version cannot be parsed safely and makes Open
// fail so the caller can degrade to a disabled history instead of corrupting
// newer data.
func TestOpenRejectsNewerIndexVersion(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "captures")
	s, err := Open(dir)
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	mustSave(t, s, testPNG(t, 16, 16), recoveryBaseTime)
	writeFile(t, dir, indexFileName, []byte(`{"version":99,"entries":[]}`))
	if _, err := Open(dir); err == nil || !strings.Contains(err.Error(), "newer") {
		t.Fatalf("Open() with a newer index version error = %v, want a 'newer' error", err)
	}
}

// TestOpenFailsWhenPathIsNotADirectory verifies the degradation contract: a
// history directory that cannot be created is reported as an error (the app
// disables history) instead of silently misbehaving.
func TestOpenFailsWhenPathIsNotADirectory(t *testing.T) {
	path := filepath.Join(t.TempDir(), "captures")
	writeFile(t, filepath.Dir(path), filepath.Base(path), []byte("not a directory"))
	if _, err := Open(path); err == nil {
		t.Fatal("Open() on a regular file returned nil error")
	}
}

// TestOpenFailsWhenDirectoryIsReadOnly verifies that an unwritable history
// directory surfaces as an error from Open so the caller can degrade.
func TestOpenFailsWhenDirectoryIsReadOnly(t *testing.T) {
	requireWritableDirectorySemantics(t)
	dir := filepath.Join(t.TempDir(), "captures")
	if err := os.Mkdir(dir, 0o755); err != nil {
		t.Fatalf("os.Mkdir(%q) error = %v", dir, err)
	}
	makeDirectoryReadOnly(t, dir)
	if _, err := Open(dir); err == nil {
		t.Fatal("Open() on a read-only directory returned nil error")
	}
}

// TestMutationsFailCleanlyOnReadOnlyDirectory verifies that when the history
// directory becomes unwritable at runtime, Save/Prune/Delete/Clear return
// errors and leave the in-memory history and the files on disk untouched; the
// store recovers once permissions are restored. This is the degradation path
// behind "history writes failed but the app keeps running".
func TestMutationsFailCleanlyOnReadOnlyDirectory(t *testing.T) {
	requireWritableDirectorySemantics(t)
	s := testStore(t)
	old := mustSave(t, s, testPNG(t, 64, 48), time.Now().Add(-30*24*time.Hour))
	before := s.List()

	makeDirectoryReadOnly(t, s.Dir())

	if _, err := s.Save(testPNG(t, 16, 16), time.Now()); err == nil {
		t.Fatal("Save() in a read-only directory returned nil error")
	}
	if removed, err := s.Prune(1, 0); err == nil || removed != 0 {
		t.Fatalf("Prune() in a read-only directory = (%d, %v), want (0, error)", removed, err)
	}
	if err := s.Delete(old.ID); err == nil {
		t.Fatal("Delete() in a read-only directory returned nil error")
	}
	if err := s.Clear(); err == nil {
		t.Fatal("Clear() in a read-only directory returned nil error")
	}

	if got := s.List(); len(got) != 1 || got[0].ID != old.ID {
		t.Fatalf("List() after failed mutations = %+v, want only %q", got, old.ID)
	}
	if !sameEntries(before, s.List()) {
		t.Fatalf("in-memory entries changed by failed mutations: %+v", s.List())
	}
	// Reading still works while the directory is read-only.
	if _, err := s.ReadPNG(old.ID); err != nil {
		t.Fatalf("ReadPNG() in a read-only directory error = %v", err)
	}
	if _, err := os.Stat(filepath.Join(s.Dir(), old.File)); err != nil {
		t.Fatalf("failed Prune/Delete/Clear removed the screenshot file: %v", err)
	}
	if index := readIndexFile(t, s.Dir()); len(index.Entries) != 1 || index.Entries[0].ID != old.ID {
		t.Fatalf("index changed by failed mutations: %+v", index.Entries)
	}

	// Restoring write access makes the same store usable again.
	if err := os.Chmod(s.Dir(), 0o755); err != nil {
		t.Fatalf("os.Chmod(%q, 0755) error = %v", s.Dir(), err)
	}
	if _, err := s.Save(testPNG(t, 16, 16), time.Now()); err != nil {
		t.Fatalf("Save() after restoring permissions error = %v", err)
	}
	if got := len(s.List()); got != 2 {
		t.Fatalf("List() after restoring permissions has %d entries, want 2", got)
	}
}

// sameEntries compares which entries (in which order) a store holds. It
// compares time instants instead of time.Time structs, which also carry a
// monotonic reading and a location pointer.
func sameEntries(a, b []Entry) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i].ID != b[i].ID || a[i].Width != b[i].Width || a[i].Height != b[i].Height ||
			a[i].Size != b[i].Size || a[i].File != b[i].File || a[i].Thumb != b[i].Thumb ||
			!a[i].CreatedAt.Equal(b[i].CreatedAt) {
			return false
		}
	}
	return true
}

// TestDeletedStoreDirectoryDegradesAndIsRecreated covers a history directory
// that disappears while the app is running: writes fail cleanly (no panic) and
// the next Open recreates the directory with an empty history.
func TestDeletedStoreDirectoryDegradesAndIsRecreated(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "captures")
	s, err := Open(dir)
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	old := mustSave(t, s, testPNG(t, 32, 32), recoveryBaseTime)
	if err := os.RemoveAll(dir); err != nil {
		t.Fatalf("os.RemoveAll(%q) error = %v", dir, err)
	}

	if _, err := s.Save(testPNG(t, 16, 16), time.Now()); err == nil {
		t.Fatal("Save() after the directory was deleted returned nil error")
	}
	if _, err := s.ReadPNG(old.ID); err == nil {
		t.Fatal("ReadPNG() after the directory was deleted returned nil error")
	}

	reopened, err := Open(dir)
	if err != nil {
		t.Fatalf("Open() on the deleted directory error = %v", err)
	}
	if entries := reopened.List(); len(entries) != 0 {
		t.Fatalf("List() after reopening the deleted directory = %+v, want empty", entries)
	}
	if info, err := os.Stat(dir); err != nil || !info.IsDir() {
		t.Fatalf("store directory was not recreated: %v", err)
	}
	if index := readIndexFile(t, dir); index.Version != indexVersion || len(index.Entries) != 0 {
		t.Fatalf("recreated index = %+v, want empty version %d", index, indexVersion)
	}
	if _, err := reopened.Save(testPNG(t, 16, 16), time.Now()); err != nil {
		t.Fatalf("Save() in the recreated store error = %v", err)
	}
}
