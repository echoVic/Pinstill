package app

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"testing/synctest"
	"time"

	"github.com/echoVic/Pinstill/internal/pin"
	"github.com/egoist/mygo"
)

func pinPersistenceApp(t *testing.T) (*App, pin.State) {
	t.Helper()
	s, err := pin.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	file, err := s.SaveImage("pin-1", gnPNGData(t, 32, 32))
	if err != nil {
		t.Fatal(err)
	}
	st, err := s.Upsert(pin.State{ID: "pin-1", File: file, Width: 32, Height: 32, Opacity: 100})
	if err != nil {
		t.Fatal(err)
	}
	return &App{
		pinStore:  s,
		pinStates: map[string]pin.State{st.ID: st},
		pinTimers: make(map[string]*time.Timer),
	}, st
}

func TestOpenSavedPinDoesNotShowWindowOnPersistenceFailure(t *testing.T) {
	a, _ := pinPersistenceApp(t)
	file, err := a.pinStore.SaveImage("pin-new", gnPNGData(t, 16, 16))
	if err != nil {
		t.Fatal(err)
	}
	indexPath := filepath.Join(a.pinStore.Dir(), "pins.json")
	if err := os.Remove(indexPath); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(indexPath, 0o755); err != nil {
		t.Fatal(err)
	}
	shown := false
	id, err := a.openSavedPin("pin-new", file, 16, 16, mygo.Display{}, func(pin.State) error {
		shown = true
		return nil
	})
	if err == nil || id != "" {
		t.Errorf("pin creation = (%q, %v), want an error", id, err)
	}
	if shown {
		t.Error("pin creation showed a window without persisting its state")
	}
	if _, ok := a.pinState("pin-new"); ok {
		t.Error("failed pin creation left an in-memory pin")
	}
	if _, err := os.Stat(filepath.Join(a.pinStore.Dir(), file)); !os.IsNotExist(err) {
		t.Fatalf("failed pin creation left its new image orphaned: %v", err)
	}
}

func TestUpdatePinStateReportsPersistenceFailure(t *testing.T) {
	a, st := pinPersistenceApp(t)
	indexPath := filepath.Join(a.pinStore.Dir(), "pins.json")
	if err := os.Remove(indexPath); err != nil {
		t.Fatal(err)
	}
	// An index path occupied by a directory makes the atomic rename fail.
	if err := os.Mkdir(indexPath, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := a.updatePinState(st.ID, PinWindowState{Width: 200}); err == nil {
		t.Error("updatePinState reported success when its index write failed")
	}
	if got, _ := a.pinState(st.ID); got.Width != st.Width {
		t.Fatalf("failed persistence changed in-memory state: width = %d, want %d", got.Width, st.Width)
	}
	if got := a.pinStore.List(); len(got) != 1 || got[0].Width != st.Width {
		t.Fatalf("failed persistence changed store state: %+v", got)
	}
}

func TestRemovePinStateStopsPendingSnapshot(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		a, st := pinPersistenceApp(t)
		fired := false
		a.pinTimers[st.ID] = time.AfterFunc(time.Second, func() { fired = true })
		a.removePinState(st.ID)
		time.Sleep(2 * time.Second)
		synctest.Wait()
		if fired {
			t.Fatal("removed pin's pending snapshot timer still fired")
		}
	})
}

func TestLatePinSnapshotCannotResurrectRemovedState(t *testing.T) {
	a, st := pinPersistenceApp(t)
	// snapshotPin may already have copied the state before removal wins.
	a.removePinState(st.ID)
	st.X = 123
	a.storePinState(st)
	if _, ok := a.pinState(st.ID); ok {
		t.Error("late snapshot resurrected the removed in-memory pin")
	}
	if got := a.pinStore.List(); len(got) != 0 {
		t.Fatalf("late snapshot resurrected the removed persisted pin: %+v", got)
	}
}

func failPinIndex(t *testing.T, a *App) {
	t.Helper()
	path := filepath.Join(a.pinStore.Dir(), "pins.json")
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(path, 0o755); err != nil {
		t.Fatal(err)
	}
}

func TestRemovePinStatePreservesLiveStateOnFailure(t *testing.T) {
	a, st := pinPersistenceApp(t)
	failPinIndex(t, a)
	a.removePinState(st.ID)
	if got, ok := a.pinState(st.ID); !ok || got != st {
		t.Fatalf("failed deletion forgot live pin: %+v, %v", got, ok)
	}
	if _, err := os.Stat(filepath.Join(a.pinStore.Dir(), st.File)); err != nil {
		t.Fatalf("failed deletion lost image: %v", err)
	}
}

func TestPinGeometryPreservesAspectAtDisplayBounds(t *testing.T) {
	display := testDisplay(testRect(0, 0, 1000, 700), testRect(0, 20, 1000, 680))
	for _, tt := range []struct{ w, h, wantW, wantH int }{
		{2000, 2000, 680, 680}, {1, 30000, 1, 680}, {30000, 1, 1000, 1},
	} {
		got := clampPinToDisplays(pin.State{Width: tt.w, Height: tt.h}, []mygo.Display{display})
		if got.Width != tt.wantW || got.Height != tt.wantH {
			t.Errorf("%dx%d -> %dx%d, want %dx%d", tt.w, tt.h, got.Width, got.Height, tt.wantW, tt.wantH)
		}
	}
}

func TestOpenSavedPinPreservesThinImageAspect(t *testing.T) {
	a, _ := pinPersistenceApp(t)
	file, err := a.pinStore.SaveImage("thin", gnPNGData(t, 1, 30000))
	if err != nil {
		t.Fatal(err)
	}
	_, err = a.openSavedPin("thin", file, 1, 30000, mygo.Display{ScaleFactor: 1}, func(st pin.State) error {
		if st.Width != 1 || st.Height != 720 {
			t.Errorf("new pin = %dx%d, want 1x720", st.Width, st.Height)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	st, _ := a.pinState("thin")
	if st.Width != 1 || st.Height != 720 {
		t.Errorf("persisted pin = %dx%d, want 1x720", st.Width, st.Height)
	}
}

func TestClosePinPreventsCloseOnPersistenceFailure(t *testing.T) {
	a, st := pinPersistenceApp(t)
	failPinIndex(t, a)
	closed := false
	if err := a.closePinWith(st.ID, func() { closed = true }); err == nil {
		t.Error("close succeeded despite failed persistence")
	}
	if closed {
		t.Error("native close called on persistence failure")
	}
	e := &mygo.CloseEvent{}
	reported := false
	a.handlePinClose(st.ID, e, func(error) { reported = true })
	if !e.DefaultPrevented() || !reported {
		t.Fatalf("close handler: prevented=%v reported=%v", e.DefaultPrevented(), reported)
	}
}

func TestPinMutationPersistsBeforeNativeChange(t *testing.T) {
	for _, kind := range []string{"resize", "opacity"} {
		t.Run(kind, func(t *testing.T) {
			a, st := pinPersistenceApp(t)
			failPinIndex(t, a)
			applied := false
			err := a.changePin(st.ID, func(st *pin.State) {
				if kind == "resize" {
					st.Width = 200
				} else {
					st.Opacity = 50
				}
			}, func(pin.State) { applied = true })
			if err == nil || applied {
				t.Fatalf("failed %s: err=%v nativeChanged=%v", kind, err, applied)
			}
			if got, _ := a.pinState(st.ID); got != st {
				t.Fatalf("failed mutation changed live state: %+v", got)
			}
		})
	}
}

func TestSnapshotPersistenceFailureIsReported(t *testing.T) {
	a, st := pinPersistenceApp(t)
	failPinIndex(t, a)
	reported := false
	a.savePinSnapshot(st, func(err error) { reported = err != nil })
	if !reported {
		t.Fatal("snapshot persistence failure was not reported to user")
	}
}

func TestArchivedPinRestoreRollsBackIfWindowFails(t *testing.T) {
	a, st := pinPersistenceApp(t)
	st.Archived = true
	if _, err := a.pinStore.Upsert(st); err != nil {
		t.Fatal(err)
	}
	delete(a.pinStates, st.ID)
	failure := errors.New("window failed")
	err := a.restoreArchivedPinWith(st.ID, func(pin.State) error { return failure })
	if !errors.Is(err, failure) {
		t.Fatalf("restore err=%v", err)
	}
	archived := a.archivedPinStates()
	if len(archived) != 1 || archived[0].ID != st.ID {
		t.Fatalf("failed restore lost archive: %+v", archived)
	}
	if _, ok := a.pinState(st.ID); ok {
		t.Fatal("failed restore published a live pin")
	}
	if _, err := a.pinStore.ReadImage(st); err != nil {
		t.Fatal(err)
	}
}

func TestArchivedPinRestoreHonorsCapAndUnarchives(t *testing.T) {
	a, st := pinPersistenceApp(t)
	st.Archived = true
	if _, err := a.pinStore.Upsert(st); err != nil {
		t.Fatal(err)
	}
	delete(a.pinStates, st.ID)
	for i := 0; i < maxRestoredPins; i++ {
		id := fmt.Sprintf("live-%d", i)
		a.pinStates[id] = pin.State{ID: id}
	}
	opened := false
	show := func(st pin.State) error { opened = true; return nil }
	if err := a.restoreArchivedPinWith(st.ID, show); err == nil || opened {
		t.Fatalf("restore over live cap err=%v opened=%v", err, opened)
	}
	delete(a.pinStates, "live-0")
	if err := a.restoreArchivedPinWith(st.ID, show); err != nil {
		t.Fatal(err)
	}
	if !opened || len(a.archivedPinStates()) != 0 {
		t.Fatal("successful restore did not unarchive")
	}
	if got, ok := a.pinState(st.ID); !ok || got.Archived {
		t.Fatalf("restored state missing: %+v", got)
	}
}

func TestArchivedPinRestoreClampsAndPersistsEffectiveGeometry(t *testing.T) {
	a, st := pinPersistenceApp(t)
	st.X, st.Y, st.Width, st.Height = 5000, 5000, 400, 300
	st.Archived = true
	if _, err := a.pinStore.Upsert(st); err != nil {
		t.Fatal(err)
	}
	delete(a.pinStates, st.ID)
	display := testDisplay(testRect(0, 0, 800, 600), testRect(0, 20, 800, 580))
	var shown pin.State
	if err := a.restoreArchivedPinWithDisplays(st.ID, []mygo.Display{display}, func(got pin.State) error {
		shown = got
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if shown.X != 400 || shown.Y != 300 || shown.Width != 400 || shown.Height != 300 {
		t.Fatalf("shown state = %+v, want clamped geometry", shown)
	}
	stored, ok := a.pinState(st.ID)
	if !ok || stored != shown {
		t.Fatalf("live state = %+v (ok=%v), want persisted shown state %+v", stored, ok, shown)
	}
	if archived := a.archivedPinStates(); len(archived) != 0 {
		t.Fatalf("restored state remains archived: %+v", archived)
	}
}

func TestOpenSavedPinClampsToSmallWorkArea(t *testing.T) {
	a, _ := pinPersistenceApp(t)
	file, err := a.pinStore.SaveImage("small-area", gnPNGData(t, 640, 720))
	if err != nil {
		t.Fatal(err)
	}
	display := testDisplay(testRect(100, 200, 120, 90), testRect(100, 200, 120, 90))
	var shown pin.State
	if _, err := a.openSavedPin("small-area", file, 640, 720, display, func(got pin.State) error {
		shown = got
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if shown.X != 100 || shown.Y != 200 || shown.Width != 80 || shown.Height != 90 {
		t.Fatalf("shown state = %+v, want aspect-preserving work-area fit", shown)
	}
}

func TestSnapshotAllPinsPropagatesFailures(t *testing.T) {
	a, _ := pinPersistenceApp(t)
	failure := errors.New("snapshot failed")
	if err := a.snapshotAllPinsWith(func(string) error { return failure }); !errors.Is(err, failure) {
		t.Fatalf("quit snapshot error=%v", err)
	}
}

func TestChangePinSerializesConcurrentFieldUpdates(t *testing.T) {
	a, st := pinPersistenceApp(t)
	firstEntered := make(chan struct{})
	releaseFirst := make(chan struct{})
	firstDone := make(chan error, 1)
	secondEntered := make(chan struct{})
	secondDone := make(chan error, 1)

	go func() {
		firstDone <- a.changePin(st.ID, func(got *pin.State) {
			close(firstEntered)
			<-releaseFirst
			got.Width = 200
		}, func(pin.State) {})
	}()
	<-firstEntered
	go func() {
		secondDone <- a.changePin(st.ID, func(got *pin.State) {
			close(secondEntered)
			got.Opacity = 50
		}, func(pin.State) {})
	}()

	secondCompleted := false
	select {
	case <-secondEntered:
		if err := <-secondDone; err != nil {
			t.Fatalf("second changePin() error = %v", err)
		}
		secondCompleted = true
	case <-time.After(time.Second):
		// A correct implementation keeps the second read-modify-write behind
		// the first one. Release the first operation so it can proceed.
	}
	close(releaseFirst)
	if err := <-firstDone; err != nil {
		t.Fatalf("first changePin() error = %v", err)
	}
	if !secondCompleted {
		select {
		case err := <-secondDone:
			if err != nil {
				t.Fatalf("second changePin() error = %v", err)
			}
		case <-time.After(time.Second):
			t.Fatal("second changePin() did not complete")
		}
	}

	got, ok := a.pinState(st.ID)
	if !ok {
		t.Fatal("pin state disappeared")
	}
	if got.Width != 200 || got.Opacity != 50 {
		t.Fatalf("concurrent changes lost a field: got width=%d opacity=%g", got.Width, got.Opacity)
	}
}

func TestQueuedPinChangeAppliesLatestState(t *testing.T) {
	a, st := pinPersistenceApp(t)
	queued := make([]func(), 0, 2)
	a.scrollUI = func(fn func()) { queued = append(queued, fn) }
	var applied []pin.State

	if err := a.changePin(st.ID, func(got *pin.State) {
		got.Width = 200
	}, func(got pin.State) {
		applied = append(applied, got)
	}); err != nil {
		t.Fatalf("width changePin() error = %v", err)
	}
	if err := a.changePin(st.ID, func(got *pin.State) {
		got.Opacity = 50
	}, func(got pin.State) {
		applied = append(applied, got)
	}); err != nil {
		t.Fatalf("opacity changePin() error = %v", err)
	}
	if len(queued) != 2 {
		t.Fatalf("queued callbacks = %d, want 2", len(queued))
	}

	queued[0]()
	if len(applied) != 1 || applied[0].Width != 200 || applied[0].Opacity != 50 {
		t.Fatalf("older callback applied stale state: %+v", applied)
	}

	queued[1]()
	if len(applied) != 2 || applied[1].Width != 200 || applied[1].Opacity != 50 {
		t.Fatalf("newer callback applied unexpected state: %+v", applied)
	}

	a, st = pinPersistenceApp(t)
	queued = queued[:0]
	a.scrollUI = func(fn func()) { queued = append(queued, fn) }
	applied = nil
	if err := a.changePin(st.ID, func(got *pin.State) {
		got.Width = 220
	}, func(got pin.State) {
		applied = append(applied, got)
	}); err != nil {
		t.Fatalf("removal changePin() error = %v", err)
	}
	a.mu.Lock()
	delete(a.pinStates, st.ID)
	a.mu.Unlock()
	queued[0]()
	if len(applied) != 0 {
		t.Fatalf("callback applied a removed pin: %+v", applied)
	}
}
