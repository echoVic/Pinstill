package pin

import (
	"bytes"
	"errors"
	"fmt"
	"github.com/echoVic/Pinstill/internal/diskio"
	"github.com/echoVic/Pinstill/internal/imagebudget"
	"image/png"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sync"
	"testing"
	"time"
)

func TestSaveImageRejectsTruncatedReplacement(t *testing.T) {
	s := openTestStore(t)
	original := testPNG(t, 8, 8)
	if _, err := s.SaveImage("pin-1", original); err != nil {
		t.Fatal(err)
	}
	// A valid IHDR is enough for DecodeConfig, but not enough to display a PNG.
	truncated := original[:33]
	if _, err := png.DecodeConfig(bytes.NewReader(truncated)); err != nil {
		t.Fatalf("fixture has no valid PNG header: %v", err)
	}
	if _, err := s.SaveImage("pin-1", truncated); !errors.Is(err, ErrInvalidImage) {
		t.Errorf("SaveImage(truncated replacement) error = %v, want ErrInvalidImage", err)
	}
	got, err := s.ReadImage(State{ID: "pin-1"})
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, original) {
		t.Fatal("truncated replacement destroyed the original image")
	}
}

func TestRemovePreservesSharedImage(t *testing.T) {
	for _, sharedFile := range []string{"shared.png", "SHARED.png"} {
		t.Run(sharedFile, func(t *testing.T) {
			s := openTestStore(t)
			original := testPNG(t, 8, 8)
			if _, err := s.SaveImage("shared", original); err != nil {
				t.Fatal(err)
			}
			if _, err := os.Stat(filepath.Join(s.Dir(), sharedFile)); os.IsNotExist(err) {
				t.Skip("case-sensitive filesystem: uppercase name does not alias the image")
			} else if err != nil {
				t.Fatal(err)
			}
			mustUpsert(t, s, State{ID: "pin-1", File: "shared.png"})
			survivor := mustUpsert(t, s, State{ID: "pin-2", File: sharedFile})
			if err := s.Remove("pin-1"); err != nil {
				t.Fatal(err)
			}
			got, err := s.ReadImage(survivor)
			if err != nil || !bytes.Equal(got, original) {
				t.Fatalf("removing pin-1 destroyed pin-2's shared image: %v", err)
			}
			reopened, err := Open(s.Dir())
			if err != nil {
				t.Fatal(err)
			}
			if states := reopened.List(); len(states) != 1 || states[0].ID != "pin-2" {
				t.Fatalf("surviving pin lost on reopen: %+v", states)
			}
			if err := reopened.Remove("pin-2"); err != nil {
				t.Fatal(err)
			}
			if _, err := os.Stat(filepath.Join(s.Dir(), sharedFile)); !os.IsNotExist(err) {
				t.Fatalf("last reference removal did not delete the image: %v", err)
			}
		})
	}
}

func TestOpenPreservesNewerIndexWithUnknownStateShape(t *testing.T) {
	s := openTestStore(t)
	content := []byte(`{"version":99,"pins":{"new-format":true}}`)
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

func TestIndependentHandlesPreserveMutations(t *testing.T) {
	first := openTestStore(t)
	second, err := Open(first.Dir())
	if err != nil {
		t.Fatal(err)
	}
	mustSaveImage(t, first, "pin-1", 8, 8)
	mustSaveImage(t, second, "pin-2", 9, 9)
	mustUpsert(t, first, State{ID: "pin-1"})
	mustUpsert(t, second, State{ID: "pin-2"})
	reopened, err := Open(first.Dir())
	if err != nil {
		t.Fatal(err)
	}
	if got := reopened.List(); len(got) != 2 {
		t.Errorf("second handle lost first pin: %+v", got)
	}
	if err := first.Remove("pin-1"); err != nil {
		t.Fatal(err)
	}
	reopened, err = Open(first.Dir())
	if err != nil {
		t.Fatal(err)
	}
	if got := reopened.List(); len(got) != 1 || got[0].ID != "pin-2" {
		t.Fatalf("stale removal lost unrelated pin: %+v", got)
	}
}

func TestOpenRejectsCorruptPinImageWithoutDeletingOriginal(t *testing.T) {
	s := openTestStore(t)
	file := mustSaveImage(t, s, "pin-1", 8, 8)
	mustUpsert(t, s, State{ID: "pin-1"})
	bad := testPNG(t, 8, 8)[:33]
	if err := os.WriteFile(filepath.Join(s.Dir(), file), bad, 0o644); err != nil {
		t.Fatal(err)
	}
	reopened, err := Open(s.Dir())
	if err != nil {
		t.Fatal(err)
	}
	if got := reopened.List(); len(got) != 0 {
		t.Errorf("restored corrupt image: %+v", got)
	}
	if _, err := s.ReadImage(State{ID: "pin-1"}); err == nil {
		t.Error("ReadImage accepted corrupt original")
	}
	got, err := os.ReadFile(filepath.Join(s.Dir(), file))
	if err != nil || !bytes.Equal(got, bad) {
		t.Fatalf("damaged original changed: %v", err)
	}
}

func TestOpenWaitsForLiveWriterBeforeCleaningTemps(t *testing.T) {
	s := openTestStore(t)
	unlock, err := diskio.LockDir(s.Dir())
	if err != nil {
		t.Fatal(err)
	}
	// An atomic writer holds this lock from creation through rename.
	temp := filepath.Join(s.Dir(), ".pins.json.tmp-live")
	if err := os.WriteFile(temp, []byte("live transaction"), 0o600); err != nil {
		unlock()
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() { _, err := Open(s.Dir()); done <- err }()
	select {
	case err := <-done:
		unlock()
		t.Fatalf("Open bypassed live writer's lock: %v", err)
	case <-time.After(50 * time.Millisecond):
	}
	if _, err := os.Stat(temp); err != nil {
		unlock()
		t.Fatalf("live temp removed: %v", err)
	}
	unlock()
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(temp); !os.IsNotExist(err) {
		t.Fatalf("abandoned temp not cleaned under lock: %v", err)
	}
}

func TestIndependentProcessesPreservePins(t *testing.T) {
	if dir := os.Getenv("PINSTILL_PIN_LOCK_TEST_DIR"); dir != "" {
		s, err := Open(dir)
		if err != nil {
			t.Fatal(err)
		}
		prefix := os.Getenv("PINSTILL_PIN_LOCK_TEST_ID")
		for i := 0; i < 4; i++ {
			id := fmt.Sprintf("%s-%d", prefix, i)
			mustSaveImage(t, s, id, 8, 8)
			mustUpsert(t, s, State{ID: id})
		}
		return
	}
	s := openTestStore(t)
	var wg sync.WaitGroup
	for i := 0; i < 3; i++ {
		wg.Go(func() {
			cmd := exec.Command(os.Args[0], "-test.run=^TestIndependentProcessesPreservePins$")
			cmd.Env = append(os.Environ(), "PINSTILL_PIN_LOCK_TEST_DIR="+s.Dir(), fmt.Sprintf("PINSTILL_PIN_LOCK_TEST_ID=process-%d", i))
			if out, err := cmd.CombinedOutput(); err != nil {
				t.Errorf("child: %v\n%s", err, out)
			}
		})
	}
	wg.Wait()
	reopened, err := Open(s.Dir())
	if err != nil {
		t.Fatal(err)
	}
	if got := len(reopened.List()); got != 12 {
		t.Fatalf("concurrent processes lost pins: got %d, want 12", got)
	}
}

func TestReadImageRejectsOversizedEncodedFile(t *testing.T) {
	s := openTestStore(t)
	file := mustSaveImage(t, s, "pin-1", 8, 8)
	path := filepath.Join(s.Dir(), file)
	if err := os.Truncate(path, imagebudget.MaxEncodedBytes+1); err != nil {
		t.Fatal(err)
	}
	if _, err := s.ReadImage(State{ID: "pin-1"}); err == nil {
		t.Fatal("oversized file accepted")
	}
	if info, err := os.Stat(path); err != nil || info.Size() != imagebudget.MaxEncodedBytes+1 {
		t.Fatalf("oversized original altered: %v", err)
	}
}

func TestThinPinDimensionsSurvivePersistence(t *testing.T) {
	s := openTestStore(t)
	mustSaveImage(t, s, "thin", 1, 720)
	got := mustUpsert(t, s, State{ID: "thin", Width: 1, Height: 720})
	if got.Width != 1 || got.Height != 720 {
		t.Fatalf("thin pin distorted to %dx%d", got.Width, got.Height)
	}
	reopened, err := Open(s.Dir())
	if err != nil {
		t.Fatal(err)
	}
	if got := reopened.List()[0]; got.Width != 1 || got.Height != 720 {
		t.Fatalf("restored thin pin distorted: %+v", got)
	}
}

func TestRestoreCapArchivesWithoutResurfacingOrDeleting(t *testing.T) {
	s := openTestStore(t)
	for i, id := range []string{"old", "middle", "new"} {
		mustSaveImage(t, s, id, 8, 8)
		mustUpsert(t, s, State{ID: id, CreatedAt: time.Unix(int64(i+1), 0)})
	}
	active, err := s.PrepareRestore(2)
	if err != nil {
		t.Fatal(err)
	}
	if len(active) != 2 || active[0].ID != "middle" || active[1].ID != "new" {
		t.Fatalf("initial restore: %+v", active)
	}
	if err := s.Remove("new"); err != nil {
		t.Fatal(err)
	}
	reopened, err := Open(s.Dir())
	if err != nil {
		t.Fatal(err)
	}
	active, err = reopened.PrepareRestore(2)
	if err != nil {
		t.Fatal(err)
	}
	if len(active) != 1 || active[0].ID != "middle" {
		t.Fatalf("archived pin resurfaced: %+v", active)
	}
	if len(reopened.List()) != 2 {
		t.Fatal("archive deleted persisted state")
	}
	if _, err := reopened.ReadImage(State{ID: "old"}); err != nil {
		t.Fatalf("archive lost original: %v", err)
	}
}

func TestRestoreCapReportsPersistenceFailure(t *testing.T) {
	s := openTestStore(t)
	for _, id := range []string{"old", "new"} {
		mustSaveImage(t, s, id, 8, 8)
		mustUpsert(t, s, State{ID: id})
	}
	path := filepath.Join(s.Dir(), indexFileName)
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	// Read-only directory leaves the index readable but prevents archive writes.
	if runtime.GOOS == "windows" || os.Geteuid() == 0 {
		t.Skip("requires Unix non-root permissions")
	}
	if err := os.Chmod(s.Dir(), 0o555); err != nil {
		t.Fatal(err)
	}
	defer os.Chmod(s.Dir(), 0o755)
	if _, err := s.PrepareRestore(1); err == nil {
		t.Error("archive failure reported success")
	}
	after, err := os.ReadFile(path)
	if err != nil || !bytes.Equal(after, data) {
		t.Fatalf("failed archive changed index: %v", err)
	}
}
