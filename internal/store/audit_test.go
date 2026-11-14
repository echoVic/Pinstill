package store

import (
	"bytes"
	"encoding/json"
	"fmt"
	"image/png"
	"math"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestOpenPreservesNewerIndexWithUnknownEntryShape(t *testing.T) {
	s := testStore(t)
	content := []byte(`{"version":99,"entries":{"new-format":true}}`)
	path := filepath.Join(s.Dir(), indexFileName)
	if err := os.WriteFile(path, content, 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := Open(s.Dir()); err == nil {
		t.Error("Open accepted an unsupported future index")
	}
	after, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(after, content) {
		t.Fatal("Open overwrote a future index it could not decode")
	}
}

func TestPruneSizeSumDoesNotOverflow(t *testing.T) {
	s := testStore(t)
	base := time.Now()
	oldest := mustSave(t, s, testPNG(t, 8, 8), base)
	newest := mustSave(t, s, testPNG(t, 8, 8), base.Add(time.Minute))
	// These positive int64 sizes are accepted when loading an edited index.
	oldest.Size, newest.Size = math.MaxInt64, 1
	data, err := json.Marshal(indexFile{Version: indexVersion, Entries: []Entry{newest, oldest}})
	if err != nil {
		t.Fatal(err)
	}
	writeFile(t, s.Dir(), indexFileName, data)
	reopened, err := Open(s.Dir())
	if err != nil {
		t.Fatal(err)
	}
	removed, err := reopened.Prune(0, math.MaxInt64)
	if err != nil || removed != 1 {
		t.Fatalf("Prune with overflowing total = (%d, %v), want (1, nil)", removed, err)
	}
	if got := reopened.List(); len(got) != 1 || got[0].ID != newest.ID {
		t.Fatalf("Prune kept wrong entries: %+v", got)
	}
}

func TestOpenRecoversStructurallyEmptyIndex(t *testing.T) {
	for _, content := range []string{"null", "{}", `{"version":1}`, `{"version":1,"entries":null}`} {
		t.Run(content, func(t *testing.T) {
			s := testStore(t)
			entry := mustSave(t, s, testPNG(t, 8, 8), time.Now())
			writeFile(t, s.Dir(), indexFileName, []byte(content))
			reopened, err := Open(s.Dir())
			if err != nil {
				t.Fatal(err)
			}
			if got := reopened.List(); len(got) != 1 || got[0].ID != entry.ID {
				t.Fatalf("structurally empty index hid recoverable screenshot: %+v", got)
			}
		})
	}
}

func TestRecoverySkipsTruncatedPNG(t *testing.T) {
	for _, keepThumbnail := range []bool{false, true} {
		t.Run(fmt.Sprintf("existing-thumbnail=%v", keepThumbnail), func(t *testing.T) {
			s := testStore(t)
			data := testPNG(t, 8, 8)
			entry := mustSave(t, s, data, time.Now())
			truncated := data[:33]
			if _, err := png.DecodeConfig(bytes.NewReader(truncated)); err != nil {
				t.Fatalf("fixture has no valid PNG header: %v", err)
			}
			writeFile(t, s.Dir(), entry.File, truncated)
			if !keepThumbnail {
				if err := os.Remove(filepath.Join(s.Dir(), entry.Thumb)); err != nil {
					t.Fatal(err)
				}
			}
			writeFile(t, s.Dir(), indexFileName, []byte("{broken"))
			reopened, err := Open(s.Dir())
			if err != nil {
				t.Fatal(err)
			}
			if got := reopened.List(); len(got) != 0 {
				t.Fatalf("recovery indexed a truncated PNG: %+v", got)
			}
			if got, err := os.ReadFile(filepath.Join(s.Dir(), entry.File)); err != nil || !bytes.Equal(got, truncated) {
				t.Fatalf("recovery must leave damaged source files in place: %v", err)
			}
		})
	}
}

func TestIndependentHandlesPreserveMutations(t *testing.T) {
	first := testStore(t)
	second, err := Open(first.Dir())
	if err != nil {
		t.Fatal(err)
	}
	a := mustSave(t, first, testPNG(t, 8, 8), time.Now())
	b := mustSave(t, second, testPNG(t, 9, 9), time.Now())
	reopened, err := Open(first.Dir())
	if err != nil {
		t.Fatal(err)
	}
	if got := reopened.List(); len(got) != 2 {
		t.Errorf("second handle lost first save: %+v", got)
	}
	if err := first.Delete(a.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := second.Prune(0, 0); err != nil {
		t.Fatal(err)
	}
	reopened, err = Open(first.Dir())
	if err != nil {
		t.Fatal(err)
	}
	if got := reopened.List(); len(got) != 1 || got[0].ID != b.ID {
		t.Fatalf("stale delete lost unrelated save: %+v", got)
	}
}

func TestOpenRepairsCorruptExistingThumbnail(t *testing.T) {
	for _, recoverIndex := range []bool{false, true} {
		t.Run(fmt.Sprint(recoverIndex), func(t *testing.T) {
			s := testStore(t)
			entry := mustSave(t, s, testPNG(t, 8, 8), time.Now())
			writeFile(t, s.Dir(), entry.Thumb, []byte("corrupt but nonempty"))
			if recoverIndex {
				writeFile(t, s.Dir(), indexFileName, []byte("broken"))
			}
			if _, err := Open(s.Dir()); err != nil {
				t.Fatal(err)
			}
			assertValidThumbnail(t, s.Dir(), entry.Thumb)
		})
	}
}
